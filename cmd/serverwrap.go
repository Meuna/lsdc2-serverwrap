package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/meuna/lsdc2-serverwrap/internal"

	"go.uber.org/zap"
)

func main() {
	var logger *zap.Logger
	if os.Getenv("DEBUG") != "" {
		logger, _ = zap.NewDevelopment()
	} else {
		logger, _ = zap.NewProduction()
	}
	defer logger.Sync() // flushes buffer, if any

	cfg, err := internal.ParseEnv()
	if err != nil {
		logger.Panic("ParseEnv failed", zap.Error(err))
	}
	logger.Debug("configuration parsed", zap.Any("cfg", cfg))

	// Prepare BPF to filter on incomming IP4 packes
	filter := cfg.SniffFilter
	if ip, err := internal.GetIP4(cfg.Iface); err == nil {
		filter = fmt.Sprintf("dst host %v", ip)
		if cfg.SniffFilter != "" {
			filter = fmt.Sprintf("(%v) and (%v)", filter, cfg.SniffFilter)
		}
	}
	logger.Debug("computed BPF filter", zap.String("filter", filter))

	// Prepare and wrapped start process
	wrapped := internal.NewWrapped(logger, os.Args[1:], cfg)
	wrapped.Dir = cfg.Cwd
	wrapped.Uid = cfg.Uid
	wrapped.Gid = cfg.Gid
	if len(cfg.PersistFiles) > 0 {
		logger.Debug("S3 persistance configured")
		wrapped.SetupPersistance(cfg.Bucket, cfg.Key, cfg.PersistFiles, cfg.Zip, cfg.ZipFrom)
	}
	wrapped.StartProcess()

	// Start channel monitoring
	pollingC := make(chan bool)
	sniffTicker := time.NewTicker(cfg.SniffDelay)
	emptyTicker := time.NewTicker(cfg.EmptyTimeout)

	sigC := make(chan os.Signal, 1)
	signal.Notify(sigC, syscall.SIGTERM, syscall.SIGINT)

	defer func() {
		sniffTicker.Stop()
		emptyTicker.Stop()
		wrapped.StopProcess()
	}()

	logger.Info("start monitoring network and signals")
	for {
		select {
		case packetFound := <-pollingC:
			if packetFound {
				logger.Debug("network activity detected")
				emptyTicker.Reset(cfg.EmptyTimeout)
			}
		case <-sniffTicker.C:
			go func() {
				pollingC <- internal.PollFilteredIface(logger, cfg.Iface, filter, cfg.SniffTimeout)
			}()
		case <-emptyTicker.C:
			logger.Info("server empty for too long")
			return
		case <-sigC:
			logger.Info("received signal")
			return
		}
	}
}
