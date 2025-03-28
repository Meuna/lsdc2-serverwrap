package main

import (
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/meuna/lsdc2-serverwrap/internal"

	"go.uber.org/zap"
)

var (
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
)

func main() {
	var logger *zap.Logger
	if os.Getenv("DEBUG") != "" {
		logger, _ = zap.NewDevelopment()
	} else {
		logger, _ = zap.NewProduction()
	}
	defer logger.Sync() // flushes buffer, if any

	logger.Info("Running serverwrap", zap.String("version", Version), zap.String("Commit", Commit), zap.String("BuildDate", BuildDate))

	// Initialise wrapped from command line and env
	wrapped := internal.NewWrapped(logger, os.Args[1:])
	logger.Debug("wrapped initialised", zap.Any("wrapped", wrapped))

	// Prepare BPF to filter on incomming IP4 packes
	wrapped.UpdateFilterWithDestination()

	// Start the process
	wrapped.StartProcess()

	// Start channel monitoring
	pollingC := make(chan bool)
	terminationCheckTicker := time.NewTicker(wrapped.TerminationCheckDelay)
	sniffTicker := time.NewTicker(wrapped.SniffDelay)
	emptyTicker := time.NewTicker(wrapped.EmptyTimeout)

	sigC := make(chan os.Signal, 1)
	signal.Notify(sigC, syscall.SIGTERM, syscall.SIGINT)

	defer func() {
		terminationCheckTicker.Stop()
		sniffTicker.Stop()
		emptyTicker.Stop()
		wrapped.StopProcess()
	}()

	if !wrapped.InEc2Instance {
		terminationCheckTicker.Stop()
	}

	logger.Info("start monitoring network and signals")
	for {
		select {
		case packetFound := <-pollingC:
			if packetFound {
				logger.Debug("network activity detected")
				emptyTicker.Reset(wrapped.EmptyTimeout)
			}
		case <-sniffTicker.C:
			go func() {
				pollingC <- wrapped.PollProcessPackets()
			}()
		case <-emptyTicker.C:
			logger.Info("server empty for too long")
			return
		case <-terminationCheckTicker.C:
			terminationNotified, err := internal.SpotTerminationIsNotified()
			if err != nil {
				logger.Error("error getting termination notification", zap.Error(err))
				wrapped.NotifyBackend("🚫 Error worth checking in the EC2 instance")
			}
			if terminationNotified {
				logger.Info("spot termination detected")
				return
			}
		case <-sigC:
			logger.Info("received signal")
			return
		}
	}
}
