package internal

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"go.uber.org/zap"
)

type Wrapped struct {
	logger *zap.Logger

	dir     string
	sigWith os.Signal
	uid     int
	gid     int

	persist  bool
	files    []string
	bucket   string
	queueUrl string
	instance string
	zipFrom  string
	zip      bool

	cl             []string
	cmd            *exec.Cmd
	scanStderr     bool
	scanStdout     bool
	wakeupSentinel string
	logScans       bool
	logFilter      []string
}

func NewWrapped(logger *zap.Logger, cl []string, cfg Config) *Wrapped {
	return &Wrapped{
		logger: logger,
		dir:    cfg.Cwd,
		uid:    cfg.Uid,
		gid:    cfg.Gid,

		persist:  len(cfg.PersistFiles) > 0,
		files:    cfg.PersistFiles,
		bucket:   cfg.Bucket,
		queueUrl: cfg.QueueUrl,
		instance: cfg.Instance,
		zipFrom:  cfg.ZipFrom,
		zip:      cfg.Zip,

		cl:             cl,
		sigWith:        syscall.SIGTERM,
		scanStderr:     cfg.ScanStderr,
		scanStdout:     cfg.ScanStdout,
		wakeupSentinel: cfg.WakeupSentinel,
		logScans:       cfg.LogScans,
		logFilter:      cfg.LogFilter,
	}
}

func (w *Wrapped) StartProcess() {
	if w.persist {
		w.logger.Info("downloading from S3")
		err := w.retrieveData()
		if err != nil {
			w.logger.Error("error in StartProcess", zap.String("culprit", "retrieveData"), zap.Error(err))
		} else {
			w.logger.Info("S3 download done !")
		}
	}

	w.logger.Debug("cmd initialisation", zap.Strings("cl", w.cl))
	w.cmd = exec.Command(w.cl[0], w.cl[1:]...)
	scannedStreams := []io.ReadCloser{}
	if w.dir != "" {
		w.logger.Debug("set cmd working directory", zap.String("cwd", w.dir))
		w.cmd.Dir = w.dir
	}
	if (w.uid != 0) || (w.gid != 0) {
		w.logger.Debug("set cmd uid/gid", zap.Int("uid", w.uid), zap.Int("gid", w.gid))
		w.cmd.SysProcAttr = &syscall.SysProcAttr{}
		w.cmd.SysProcAttr.Credential = &syscall.Credential{Uid: uint32(w.uid), Gid: uint32(w.gid)}
	}
	if w.scanStderr {
		w.logger.Debug("get cmd stderr stream")
		stream, err := w.cmd.StderrPipe()
		if err != nil {
			w.logger.Panic("error in StartProcess", zap.String("culprit", "StderrPipe"), zap.Error(err))
		}
		scannedStreams = append(scannedStreams, stream)
	}
	if w.scanStdout {
		w.logger.Debug("get cmd stdout stream")
		stream, err := w.cmd.StdoutPipe()
		if err != nil {
			w.logger.Panic("error in StartProcess", zap.String("culprit", "StdoutPipe"), zap.Error(err))
		}
		scannedStreams = append(scannedStreams, stream)
	}
	w.logger.Debug("start cmd")
	if err := w.cmd.Start(); err != nil {
		w.logger.Panic("error in StartProcess", zap.String("culprit", "Start"), zap.Error(err))
	}
	if len(scannedStreams) > 0 {
		w.logger.Info("std scan enabled", zap.String("wakeupSentinel", w.wakeupSentinel), zap.Bool("logScans", w.logScans), zap.Any("logFilter", w.logFilter))
		w.enableStdScans(scannedStreams)
	}
	w.logger.Info("process started")
}

func (w *Wrapped) enableStdScans(streams []io.ReadCloser) {
	logChan := make(chan string, 60)
	wakeupChan := make(chan string, 60)
	for _, stream := range streams {
		scanner := bufio.NewScanner(stream)
		go func() {
			for scanner.Scan() {
				line := scanner.Text()
				line = strings.TrimSpace(line)
				if w.logScans {
					if len(w.logFilter) > 0 {
						for _, word := range w.logFilter {
							if strings.Contains(line, word) {
								logChan <- line
								break
							}
						}
					} else {
						logChan <- line
					}
				}
				if w.wakeupSentinel != "" && strings.Contains(line, w.wakeupSentinel) {
					wakeupChan <- line
				}
			}
		}()
	}
	go func() {
		for line := range logChan {
			w.logger.Info(line)
		}
	}()
	go func() {
		for line := range wakeupChan {
			w.logger.Info("sentinel found", zap.String("sentinel", line))
			if err := w.notifyWakeup("The server is ready !"); err != nil {
				w.logger.Error("error in enableStdScans", zap.String("culprit", "notifyWakeup"), zap.Error(err))
			}
		}
	}()
}

func (w *Wrapped) StopProcess() {
	w.cmd.Process.Signal(w.sigWith)
	w.cmd.Wait()

	// Small wait to sync file system
	time.Sleep(1 * time.Second)

	if w.persist {
		w.logger.Info("S3 upload")
		err := w.archiveData()
		if err != nil {
			w.logger.Error("error in StopProcess", zap.String("culprit", "archiveData"), zap.Error(err))
		}
	}

	w.logger.Info("goodbye !")
}

func (w *Wrapped) retrieveData() error {
	if w.zip {
		return unzipFromS3(w.logger, w.bucket, w.instance, w.zipFrom, w.uid, w.gid)
	} else {
		return downloadFromS3(w.bucket, w.instance, w.files[0], w.uid, w.gid)
	}
}

func (w *Wrapped) archiveData() error {
	if w.zip {
		return zipToS3(w.logger, w.bucket, w.instance, w.zipFrom, w.files)
	} else {
		return uploadToS3(w.bucket, w.instance, w.files[0])
	}
}

func (w *Wrapped) notifyWakeup(msg string) error {
	cmd := struct {
		Api  string
		Args any
	}{
		Api: "tasknotify",
		Args: struct {
			InstanceName string
			Message      string
		}{
			InstanceName: w.instance,
			Message:      msg,
		},
	}
	bodyBytes, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("json.Marshal / %w", err)
	}
	return queueMessage(w.queueUrl, string(bodyBytes[:]))
}
