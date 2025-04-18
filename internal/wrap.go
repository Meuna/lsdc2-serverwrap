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

	"github.com/caarlos0/env"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type Wrapped struct {
	logger       *zap.Logger
	cl           []string
	cmd          *exec.Cmd
	sigWith      os.Signal
	processStart time.Time
	iface        string

	Home string `env:"LSDC2_HOME"`
	Uid  int    `env:"LSDC2_UID"`
	Gid  int    `env:"LSDC2_GID"`

	QueueUrl     string   `env:"LSDC2_QUEUE_URL"`
	PersistFiles []string `env:"LSDC2_PERSIST_FILES" envSeparator:";"`
	Bucket       string   `env:"LSDC2_BUCKET"`
	Server       string   `env:"LSDC2_SERVER"`
	Zip          bool     `env:"LSDC2_ZIP"`
	ZipFrom      string   `env:"LSDC2_ZIPFROM"`

	InEc2Instance            bool
	CloudWatchLogGroup       string        `env:"LSDC2_LOG_GROUP"`
	CloudWatchFlushInterval  time.Duration `env:"LSDC2_LOG_FLUSH_INTERVAL" envDefault:"5s"`
	TerminationCheckInterval time.Duration `env:"LSDC2_TERMINATION_CHECK_INTERVAL" envDefault:"10s"`
	SignalGraceDelay         time.Duration `env:"LSDC2_SIGNAL_GRACE_DELAY" envDefault:"20s"`
	SniffFilter              string        `env:"LSDC2_SNIFF_FILTER"`
	SniffTimeout             time.Duration `env:"LSDC2_SNIFF_TIMEOUT" envDefault:"1s"`
	SniffInterval            time.Duration `env:"LSDC2_SNIFF_INTERVAL" envDefault:"10s"`
	EmptyTimeout             time.Duration `env:"LSDC2_EMPTY_TIMEOUT" envDefault:"5m"`

	ScanStderr     bool     `env:"LSDC2_SCAN_STDERR" envDefault:"false"`
	ScanStdout     bool     `env:"LSDC2_SCAN_STDOUT" envDefault:"false"`
	WakeupSentinel string   `env:"LSDC2_WAKEUP_SENTINEL"`
	LogScans       bool     `env:"LSDC2_LOG_SCANS" envDefault:"false"`
	LogFilter      []string `env:"LSDC2_LOG_FILTER" envSeparator:";"`

	LowMemoryWarningThresholdMiB int64         `env:"LSDC2_LOW_MEMORY_WARNING_MB" envDefault:"0"`
	LowMemorySignalThresholdMiB  int64         `env:"LSDC2_LOW_MEMORY_SIGNAL_MB" envDefault:"0"`
	LowMemoryCheckInterval       time.Duration `env:"LSDC2_LOW_MEMORY_CHECK_INTERVAL" envDefault:"5s"`

	PanicOnSocketError   bool `env:"PANIC_ON_SOCKET_ERROR" envDefault:"true"`
	DisableShutdownCalls bool `env:"DISABLE_SHUTDOWN_CALLS" envDefault:"false"`
}

func NewWrapped(logger *zap.Logger, cl []string) Wrapped {
	w := Wrapped{}
	var err error
	if err = env.Parse(&w); err != nil {
		panic(err)
	}

	// This is not redundant with the envDefault defined in Config
	// struct because empty env variables are not equivalent to
	// empty variables. The former makes the values 0
	if w.SniffTimeout == 0 {
		w.SniffTimeout = 1 * time.Second
	}
	if w.SniffInterval == 0 {
		w.SniffInterval = 10 * time.Second
	}
	if w.EmptyTimeout == 0 {
		w.EmptyTimeout = 5 * time.Minute
	}

	w.Zip = w.Zip || len(w.PersistFiles) > 1

	w.logger = logger
	w.cl = cl
	w.sigWith = syscall.SIGTERM
	w.InEc2Instance = AreWeRunningEc2()

	return w
}

func (w *Wrapped) NewEc2CloudWatchTeeLogger(logger *zap.Logger) (*zap.Logger, error) {
	w.InEc2Instance = true
	instanceId, err := GetInstanceId()
	if err != nil {
		return nil, fmt.Errorf("GetInstanceId / %w", err)
	}
	cloudWatchCore, err := NewCloudWatchCore(
		zap.InfoLevel,
		w.CloudWatchLogGroup,
		fmt.Sprintf("ec2/%s_instance/%s", w.Server, instanceId),
		100,
		w.CloudWatchFlushInterval,
	)
	if err != nil {
		return nil, fmt.Errorf("NewCloudWatchCore / %w", err)
	}
	teeCore := zapcore.NewTee(
		cloudWatchCore,
		logger.Core(),
	)

	w.logger = zap.New(teeCore)

	return w.logger, nil
}

func (w *Wrapped) DetectIfaceAndAddHostFilter() {
	if iface, ip, err := GetFirstIfaceWithIp4(); err == nil {
		w.logger.Debug("found iface", zap.String("iface", iface), zap.String("ip", ip.String()))
		filterWithDest := fmt.Sprintf("dst host %v", ip)
		if w.SniffFilter != "" {
			filterWithDest = fmt.Sprintf("(%v) and (%v)", filterWithDest, w.SniffFilter)
		}
		w.iface = iface
		w.SniffFilter = filterWithDest
	} else {
		w.logger.Debug("iface not found, using 'any'", zap.Error(err))
		w.iface = "any"
	}
	w.logger.Debug("final BPF filter", zap.String("filter", w.SniffFilter))
}

func (w *Wrapped) StartProcess() {
	if len(w.PersistFiles) > 0 {
		w.logger.Info("downloading from S3")
		err := w.retrieveData()
		if err != nil {
			w.logger.Error("error in StartProcess", zap.String("culprit", "retrieveData"), zap.Error(err))
			w.NotifyBackend("error", "Savegame was not restored")
		} else {
			w.logger.Info("S3 download done !")
			w.NotifyBackend("info", "Savegame restored from S3")
		}
	}

	w.logger.Debug("cmd initialisation", zap.Strings("cl", w.cl))
	w.cmd = exec.Command(w.cl[0], w.cl[1:]...)
	scannedStreams := []io.ReadCloser{}
	if w.Home != "" {
		w.logger.Debug("set cmd working directory", zap.String("cwd", w.Home))
		w.cmd.Dir = w.Home
	}
	if (w.Uid != 0) || (w.Gid != 0) {
		w.logger.Debug("set cmd uid/gid", zap.Int("uid", w.Uid), zap.Int("gid", w.Gid))
		w.cmd.SysProcAttr = &syscall.SysProcAttr{}
		w.cmd.SysProcAttr.Credential = &syscall.Credential{Uid: uint32(w.Uid), Gid: uint32(w.Gid)}
	}
	if w.ScanStderr {
		w.logger.Debug("get cmd stderr stream")
		stream, err := w.cmd.StderrPipe()
		if err != nil {
			w.logger.Panic("error in StartProcess", zap.String("culprit", "StderrPipe"), zap.Error(err))
		}
		scannedStreams = append(scannedStreams, stream)
	}
	if w.ScanStdout {
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
		w.logger.Info("std scan enabled", zap.String("wakeupSentinel", w.WakeupSentinel), zap.Bool("logScans", w.LogScans), zap.Any("logFilter", w.LogFilter))
		w.enableStdScans(scannedStreams)
	}
	w.logger.Info("process started")
	w.processStart = time.Now()
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
				if w.LogScans {
					if len(w.LogFilter) > 0 {
						for _, word := range w.LogFilter {
							if strings.Contains(line, word) {
								logChan <- line
								break
							}
						}
					} else {
						logChan <- line
					}
				}
				if w.WakeupSentinel != "" && strings.Contains(line, w.WakeupSentinel) {
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
			timeSinceStart := time.Now().Sub(w.processStart)
			w.logger.Info("sentinel found", zap.String("sentinel", line))
			w.NotifyBackend("server-ready", fmt.Sprintf("The server is ready ! (started in %.2fs)", timeSinceStart.Seconds()))
		}
	}()
}

func (w *Wrapped) PollProcessPackets() bool {
	packetFound, err := PollFilteredIface(w.iface, w.SniffFilter, w.SniffTimeout)
	if err != nil {
		w.logger.Error("error polling network",
			zap.String("iface", w.iface),
			zap.String("filter", w.SniffFilter),
			zap.Error(err),
		)
		if w.PanicOnSocketError {
			w.ShutdownWhenInEc2()
			panic(err)
		}
	}
	return packetFound
}

func (w *Wrapped) StopProcess() {
	// Grace delay after warning
	time.Sleep(w.SignalGraceDelay)

	// Stop the process
	w.cmd.Process.Signal(w.sigWith)
	w.cmd.Wait()

	// Small wait to sync file system
	time.Sleep(1 * time.Second)

	if len(w.PersistFiles) > 0 {
		w.logger.Info("S3 upload")
		err := w.archiveData()
		if err != nil {
			w.logger.Error("error in StopProcess", zap.String("culprit", "archiveData"), zap.Error(err))
			w.NotifyBackend("error", "Error when exporting savegame to S3")
		} else {
			w.NotifyBackend("info", "Savegame exported to S3")
		}
	}

	w.ShutdownWhenInEc2()

	w.logger.Info("goodbye !")
}

func (w *Wrapped) ShutdownWhenInEc2() {
	// Clear early return if this is true
	if w.DisableShutdownCalls {
		return
	}
	if w.InEc2Instance {
		w.logger.Info("issue shutdown")
		cmd := exec.Command("shutdown", "now")

		err := cmd.Run()
		if err != nil {
			w.logger.Error("error in StopProcess", zap.String("culprit", "Run"), zap.Error(err))
			w.NotifyBackend("error", "Error worth checking in the EC2 instance")
		}
	}
}

func (w *Wrapped) retrieveData() error {
	if w.Zip {
		return unzipFromS3(w.logger, w.Bucket, w.Server, w.ZipFrom, w.Uid, w.Gid)
	} else {
		return downloadFromS3(w.Bucket, w.Server, w.PersistFiles[0], w.Uid, w.Gid)
	}
}

func (w *Wrapped) archiveData() error {
	if w.Zip {
		return zipToS3(w.logger, w.Bucket, w.Server, w.ZipFrom, w.PersistFiles)
	} else {
		return uploadToS3(w.Bucket, w.Server, w.PersistFiles[0])
	}
}

func (w *Wrapped) NotifyBackend(action string, msg string) {
	cmd := struct {
		Api  string
		Args any
	}{
		Api: "tasknotify",
		Args: struct {
			ServerName string
			Action     string
			Message    string
		}{
			ServerName: w.Server,
			Action:     action,
			Message:    msg,
		},
	}
	bodyBytes, err := json.Marshal(cmd)
	if err != nil {
		w.logger.Error("error in NotifyBackend", zap.String("culprit", "Marshal"), zap.Error(err))
		return
	}
	err = queueMessage(w.QueueUrl, string(bodyBytes[:]))
	if err != nil {
		w.logger.Error("error in NotifyBackend", zap.String("culprit", "queueMessage"), zap.Error(err))
	}
}
