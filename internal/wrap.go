package internal

import (
	"bufio"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"go.uber.org/zap"
)

type Wrapped struct {
	Dir     string
	SigWith os.Signal
	uid     int
	gid     int

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

func (w *Wrapped) RunAs(uid int, gid int) *Wrapped {
	w.uid = uid
	w.gid = gid
	return w
}

func (w *Wrapped) PersistData(bucket string, key string, files []string, zip bool, zipFrom string) *Wrapped {
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
		w.logger.Info("S3 download")
		err := w.retrieveData()
		if err != nil {
			w.logger.Error("retrieveData failed",
				zap.Error(err),
			)
		}
	}

	w.cmd = exec.Command(w.cl[0], w.cl[1:]...)
	logStreams := []io.ReadCloser{}
	if w.Dir != "" {
		w.cmd.Dir = w.Dir
	}
	if (w.uid != 0) || (w.gid != 0) {
		w.cmd.SysProcAttr = &syscall.SysProcAttr{}
		w.cmd.SysProcAttr.Credential = &syscall.Credential{Uid: uint32(w.uid), Gid: uint32(w.gid)}
	}
	if w.logStderr {
		stream, err := w.cmd.StderrPipe()
		if err != nil {
			w.logger.Panic("StderrPipe failed",
				zap.Error(err),
			)
		}
		logStreams = append(logStreams, stream)
	}
	if w.logStdout {
		stream, err := w.cmd.StdoutPipe()
		if err != nil {
			w.logger.Panic("StdoutPipe failed",
				zap.Error(err),
			)
		}
		logStreams = append(logStreams, stream)
	}
	if err := w.cmd.Start(); err != nil {
		w.logger.Panic("cmd.Start failed",
			zap.Error(err),
		)
	}
	if len(logStreams) > 0 {
		w.logger.Info("std livelog enabled")
		w.enableLivelog(logStreams)
	}
	w.logger.Info("process started")
}

func (w *Wrapped) enableLivelog(streams []io.ReadCloser) {
	logChan := make(chan string, 5)
	var wg sync.WaitGroup
	for _, stream := range streams {
		scanner := bufio.NewScanner(stream)
		wg.Add(1)
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
			wg.Done()
		}()
	}
	go func() {
		wg.Wait()
		close(logChan)
	}()
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
		log.Println("S3 upload")
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
		return unzipFromS3(w.bucket, w.key, w.zipFrom, w.uid, w.gid)
	} else {
		return downloadFromS3(w.bucket, w.key, w.files[0], w.uid, w.gid)
	}
}

func (w *Wrapped) archiveData() error {
	if w.zip {
		return zipToS3(w.bucket, w.key, w.zipFrom, w.files)
	} else {
		return uploadToS3(w.bucket, w.key, w.files[0])
	}
}
