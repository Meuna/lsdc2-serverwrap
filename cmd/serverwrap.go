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
	defer logger.Sync()

	logger.Info("Running serverwrap", zap.String("version", Version), zap.String("Commit", Commit), zap.String("BuildDate", BuildDate))

	// Initialise wrapped from command line and env
	wrapped := internal.NewWrapped(logger, os.Args[1:])
	logger.Debug("wrapped initialised", zap.Any("wrapped", wrapped))

	// Setup CloudWatch logger if running in EC2
	if wrapped.InEc2Instance {
		newLogger, err := wrapped.NewEc2CloudWatchTeeLogger(logger)
		if err != nil {
			logger.Error("error in SetupEc2Monitoring", zap.Error(err))
			wrapped.NotifyBackend("error", "Failure setting EC2 monitoring")
		} else {
			logger = newLogger
			defer logger.Sync()
		}
	}

	// Prepare BPF to filter on incomming IP4 packes
	wrapped.DetectIfaceAndAddHostFilter()

	// Start the process
	wrapped.StartProcess()

	// Start monitoring channels
	pollingC := make(chan bool)
	terminationCheckTicker := time.NewTicker(wrapped.TerminationCheckInterval)
	lowMemoryCheckTicker := time.NewTicker(wrapped.TerminationCheckInterval)
	sniffTicker := time.NewTicker(wrapped.SniffInterval)
	emptyTicker := time.NewTicker(wrapped.EmptyTimeout)

	sigC := make(chan os.Signal, 1)
	signal.Notify(sigC, syscall.SIGTERM, syscall.SIGINT)

	defer func() {
		terminationCheckTicker.Stop()
		lowMemoryCheckTicker.Stop()
		sniffTicker.Stop()
		emptyTicker.Stop()
		wrapped.StopProcess()
	}()

	if !wrapped.InEc2Instance {
		terminationCheckTicker.Stop()
	}

	if wrapped.LowMemoryWarningThresholdMiB == 0 && wrapped.LowMemorySignalThresholdMiB == 0 {
		lowMemoryCheckTicker.Stop()
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
			wrapped.NotifyBackend("info", "Server empty. Terminating instance.")
			return
		case <-terminationCheckTicker.C:
			logger.Debug("checking SPOT termination")
			terminationNotified, err := internal.SpotTerminationIsNotified()
			if err != nil {
				logger.Error("error getting termination notification", zap.Error(err))
				wrapped.NotifyBackend("error", "Error worth checking in the EC2 instance")
			}
			if terminationNotified {
				logger.Info("spot termination detected")
				wrapped.NotifyBackend("warning", "SPOT termination detected. Terminating instance.")
				return
			}
		case <-lowMemoryCheckTicker.C:
			logger.Debug("checking low memory")
			freeMemoryMib, err := internal.GetFreeMemoryMiB()
			if err != nil {
				logger.Error("error getting free memory", zap.Error(err))
				wrapped.NotifyBackend("error", "Error checking free memory")
			}
			if freeMemoryMib < wrapped.LowMemorySignalThresholdMiB {
				logger.Warn("low memory signal", zap.Int64("freeMemory", freeMemoryMib))
				wrapped.NotifyBackend("warning", fmt.Sprintf("Memory limit breached (%d MiB). Terminating instance.", freeMemoryMib))
				return
			} else if freeMemoryMib < wrapped.LowMemoryWarningThresholdMiB {
				logger.Warn("low memory warning", zap.Int64("freeMemory", freeMemoryMib))
				wrapped.NotifyBackend("warning", fmt.Sprintf("Low memory warning (%d MiB)", freeMemoryMib))
			}
		case <-sigC:
			logger.Info("received signal")
			wrapped.NotifyBackend("warning", "Signal received. Terminating instance.")
			return
		}
	}
}
