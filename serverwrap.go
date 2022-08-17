package main

import (
	"fmt"
	"lsdc2/serverwrap/internal"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/caarlos0/env"
	"go.uber.org/zap"
)

func main() {
	logger, _ := zap.NewProduction()
	defer logger.Sync() // flushes buffer, if any

	cfg := internal.Config{}
	err := env.Parse(&cfg)
	if err != nil {
		logger.Panic("env parse failed",
			zap.Error(err),
		)
	}

	// Prepare BPF to filter on incomming IP4 packes
	filter := cfg.SniffFilter
	if ip, err := internal.GetIP4(cfg.Iface); err == nil {
		filter = fmt.Sprintf("dst host %v", ip)
		if cfg.SniffFilter != "" {
			filter = fmt.Sprintf("(%v) and (%v)", filter, cfg.SniffFilter)
		}
	}
	logger.Info("BPF filter updated",
		zap.String("filter", filter),
	)

	// Prepare and wrapped start process
	wrapped := internal.NewWrapped(logger, os.Args[1:], cfg)
	wrapped.Dir = cfg.Cwd
	wrapped.RunAs(cfg.Uid, cfg.Gid)
	if len(cfg.PersistFiles) > 0 {
		wrapped.PersistData(cfg.Bucket, cfg.Key, cfg.PersistFiles, cfg.Zip, cfg.ZipFrom)
	}
	wrapped.StartProcess()

	// Start channel monitoring
	pollingC := make(chan bool)
	sniffTicker := time.NewTicker(cfg.SniffDelay)
	emptyTicker := time.NewTicker(cfg.EmptyTimeout)

	sigC := make(chan os.Signal, 1)
	signal.Notify(sigC, syscall.SIGTERM, syscall.SIGINT)

	defer func() {
		close(pollingC)
		sniffTicker.Stop()
		emptyTicker.Stop()
		wrapped.StopProcess()
	}()

	for {
		select {
		case packetFound := <-pollingC:
			if packetFound {
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
			logger.Info("Received signal")
			return
		}
	}
}
