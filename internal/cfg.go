package internal

import (
	"time"

	"github.com/caarlos0/env"
)

type Config struct {
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
	LogStderr    bool          `env:"LSDC2_LOG_STDERR" envDefault:"false"`
	LogStdout    bool          `env:"LSDC2_LOG_STDOUT" envDefault:"false"`
	LogFilter    []string      `env:"LSDC2_LOG_FILTER" envSeparator:";"`
}

func ParseEnv() (cfg Config, err error) {
	if err = env.Parse(&cfg); err == nil {
		// This is not redundant with the envDefault defined in Config
		// struct because empty env variables are not equivalent to
		// empty variables. The former makes the values 0
		if cfg.SniffTimeout == 0 {
			cfg.SniffTimeout = 1 * time.Second
		}
		if cfg.SniffDelay == 0 {
			cfg.SniffDelay = 10 * time.Second
		}
		if cfg.EmptyTimeout == 0 {
			cfg.EmptyTimeout = 5 * time.Minute
		}
	}
	return
}
