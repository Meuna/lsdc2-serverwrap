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
	logger.Debug("wrapped initialised")

	wrapped.StartProcess()

	// Start channel monitoring
	pollingC := make(chan bool)
	terminationCheckTicker := time.NewTicker(cfg.TerminationCheckDelay)
	sniffTicker := time.NewTicker(cfg.SniffDelay)
	emptyTicker := time.NewTicker(cfg.EmptyTimeout)

	sigC := make(chan os.Signal, 1)
	signal.Notify(sigC, syscall.SIGTERM, syscall.SIGINT)

	defer func() {
		terminationCheckTicker.Stop()
		sniffTicker.Stop()
		emptyTicker.Stop()
		wrapped.StopProcess()
	}()

	if !cfg.InEc2Instance {
		terminationCheckTicker.Stop()
	}

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
				packetFound, err := internal.PollFilteredIface(cfg.Iface, filter, cfg.SniffTimeout)
				if err != nil {
					logger.Error("error polling network",
						zap.String("iface", cfg.Iface),
						zap.String("filter", filter),
						zap.Error(err),
					)
					if cfg.PanicOnSocketError {
						panic(err)
					}
				}
				pollingC <- packetFound
			}()
		case <-emptyTicker.C:
			logger.Info("server empty for too long")
			return
		case <-terminationCheckTicker.C:
			if cfg.InEc2Instance {
				terminationNotified, err := internal.SpotTerminationIsNotified()
				if err != nil {
					logger.Error("error getting termination notification", zap.Error(err))
					wrapped.NotifyBackend("🚫 Error worth checking in the EC2 instance")
				}
				if terminationNotified {
					return
				}
			}
		case <-sigC:
			logger.Info("received signal")
			return
		}
	}
}
