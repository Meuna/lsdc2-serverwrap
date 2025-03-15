package internal

import (
	"bufio"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"go.uber.org/zap"
)

type Wrapped struct {
	Dir     string
	SigWith os.Signal
	Uid     int
	Gid     int

	persist bool
	files   []string
	bucket  string
	key     string
	zipFrom string
	zip     bool

	logger    *zap.Logger
	cl        []string
	cmd       *exec.Cmd
	logStderr bool
	logStdout bool
	logFilter []string
}

func NewWrapped(logger *zap.Logger, cl []string, cfg Config) *Wrapped {
	return &Wrapped{
		logger:    logger,
		cl:        cl,
		SigWith:   syscall.SIGTERM,
		logStderr: cfg.LogStderr,
		logStdout: cfg.LogStdout,
		logFilter: cfg.LogFilter,
	}
}

func (w *Wrapped) SetupPersistance(bucket string, key string, files []string, zip bool, zipFrom string) *Wrapped {
	if len(files) > 1 {
		zip = true
	}
	w.persist = true
	w.bucket = bucket
	w.key = key
	w.files = files
	w.zip = zip
	w.zipFrom = zipFrom

	return w
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

	w.logger.Debug("cmd initialisation", zap.Any("cl", w.cl))
	w.cmd = exec.Command(w.cl[0], w.cl[1:]...)
	logStreams := []io.ReadCloser{}
	if w.Dir != "" {
		w.logger.Debug("set cmd working directory", zap.String("cwd", w.Dir))
		w.cmd.Dir = w.Dir
	}
	if (w.Uid != 0) || (w.Gid != 0) {
		w.logger.Debug("set cmd uid/gid", zap.Int("uid", w.Uid), zap.Int("gid", w.Gid))
		w.cmd.SysProcAttr = &syscall.SysProcAttr{}
		w.cmd.SysProcAttr.Credential = &syscall.Credential{Uid: uint32(w.Uid), Gid: uint32(w.Gid)}
	}
	if w.logStderr {
		w.logger.Debug("get cmd stderr stream")
		stream, err := w.cmd.StderrPipe()
		if err != nil {
			w.logger.Panic("error in StartProcess", zap.String("culprit", "StderrPipe"), zap.Error(err))
		}
		logStreams = append(logStreams, stream)
	}
	if w.logStdout {
		w.logger.Debug("get cmd stdout stream")
		stream, err := w.cmd.StdoutPipe()
		if err != nil {
			w.logger.Panic("error in StartProcess", zap.String("culprit", "StdoutPipe"), zap.Error(err))
		}
		logStreams = append(logStreams, stream)
	}
	w.logger.Debug("start cmd")
	if err := w.cmd.Start(); err != nil {
		w.logger.Panic("error in StartProcess", zap.String("culprit", "Start"), zap.Error(err))
	}
	if len(logStreams) > 0 {
		w.logger.Info("std livelog enabled", zap.Any("logFilter", w.logFilter))
		w.enableLivelog(logStreams)
	}
	w.logger.Info("process started")
}

func (w *Wrapped) enableLivelog(streams []io.ReadCloser) {
	logChan := make(chan string, 5)
	for _, stream := range streams {
		scanner := bufio.NewScanner(stream)
		go func() {
			for scanner.Scan() {
				line := scanner.Text()
				line = strings.TrimSpace(line)
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
		}()
	}
	go func() {
		for line := range logChan {
			w.logger.Info(line)
		}
	}()

}

func (w *Wrapped) StopProcess() {
	w.cmd.Process.Signal(w.SigWith)
	w.cmd.Wait()

	// Small wait to sync file system
	time.Sleep(1 * time.Second)

	if w.persist {
		w.logger.Info("S3 upload")
		err := w.archiveData()
		if err != nil {
			w.logger.Error("archiveData failed",
				zap.Error(err),
			)
		}
	}
}

func (w *Wrapped) retrieveData() error {
	if w.zip {
		return unzipFromS3(w.logger, w.bucket, w.key, w.zipFrom, w.Uid, w.Gid)
	} else {
		return downloadFromS3(w.bucket, w.key, w.files[0], w.Uid, w.Gid)
	}
}

func (w *Wrapped) archiveData() error {
	if w.zip {
		return zipToS3(w.logger, w.bucket, w.key, w.zipFrom, w.files)
	} else {
		return uploadToS3(w.bucket, w.key, w.files[0])
	}
}
