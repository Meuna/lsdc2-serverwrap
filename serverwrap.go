package main

import (
	"fmt"
	"log"
	"lsdc2/serverwrap/internal"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/caarlos0/env"
)

type config struct {
	Iface        string        `env:"LSDC2_SNIFF_IFACE"`
	SniffFilter  string        `env:"LSDC2_SNIFF_FILTER"`
	Cwd          string        `env:"LSDC2_CWD"`
	Uid          int           `env:"LSDC2_UID"`
	Gid          int           `env:"LSDC2_GID"`
	PersistFiles []string      `env:"LSDC2_PERSIST_FILES" envSeparator:";"`
	Bucket       string        `env:"LSDC2_BUCKET"`
	Key          string        `env:"LSDC2_KEY"`
	Zip          bool          `env:"LSDC2_ZIP"`
	ZipFrom      string        `env:"LSDC2_ZIPFROM"`
	SniffTimeout time.Duration `env:"LSDC2_SNIFF_TIMEOUT" envDefault:"1s"`
	SniffDelay   time.Duration `env:"LSDC2_SNIFF_DELAY" envDefault:"10s"`
	EmptyTimeout time.Duration `env:"LSDC2_EMPTY_TIMEOUT" envDefault:"5m"`
}

func main() {
	cfg := config{}
	err := env.Parse(&cfg)
	if err != nil {
		log.Panic(err)
	}

	// Prepare BPF to filter on incomming IP4 packes
	filter := cfg.SniffFilter
	if ip, err := internal.GetIP4(cfg.Iface); err == nil {
		filter = fmt.Sprintf("dst host %v", ip)
		if cfg.SniffFilter != "" {
			filter = fmt.Sprintf("(%v) and (%v)", filter, cfg.SniffFilter)
		}
	}

	// Prepare and wrapped start process
	wrapped := internal.NewWrapped(os.Args[1:])
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
				pollingC <- internal.PollFilteredIface(cfg.Iface, filter, cfg.SniffTimeout)
			}()
		case <-emptyTicker.C:
			log.Println("Server empty for too long")
			return
		case <-sigC:
			log.Println("Recieved signal")
			return
		}
	}
}
