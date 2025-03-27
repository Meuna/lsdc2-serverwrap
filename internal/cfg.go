package internal

import (
	"time"

	"github.com/caarlos0/env"
)

type Config struct {
	Iface                 string        `env:"LSDC2_SNIFF_IFACE"`
	SniffFilter           string        `env:"LSDC2_SNIFF_FILTER"`
	Home                  string        `env:"LSDC2_HOME"`
	Uid                   int           `env:"LSDC2_UID"`
	Gid                   int           `env:"LSDC2_GID"`
	PersistFiles          []string      `env:"LSDC2_PERSIST_FILES" envSeparator:";"`
	Bucket                string        `env:"LSDC2_BUCKET"`
	Server                string        `env:"LSDC2_SERVER"`
	QueueUrl              string        `env:"LSDC2_QUEUE_URL"`
	Zip                   bool          `env:"LSDC2_ZIP"`
	ZipFrom               string        `env:"LSDC2_ZIPFROM"`
	TerminationCheckDelay time.Duration `env:"LSDC2_TERMINATION_CHECK_DELAY" envDefault:"10s"`
	InEc2Instance         bool          `env:"LSDC2_IN_EC2_INSTANCE" envDefault:"false"`
	SniffTimeout          time.Duration `env:"LSDC2_SNIFF_TIMEOUT" envDefault:"1s"`
	SniffDelay            time.Duration `env:"LSDC2_SNIFF_DELAY" envDefault:"10s"`
	EmptyTimeout          time.Duration `env:"LSDC2_EMPTY_TIMEOUT" envDefault:"5m"`
	ScanStderr            bool          `env:"LSDC2_SCAN_STDERR" envDefault:"false"`
	ScanStdout            bool          `env:"LSDC2_SCAN_STDOUT" envDefault:"false"`
	WakeupSentinel        string        `env:"LSDC2_WAKEUP_SENTINEL"`
	LogScans              bool          `env:"LSDC2_LOG_SCANS" envDefault:"false"`
	LogFilter             []string      `env:"LSDC2_LOG_FILTER" envSeparator:";"`
	PanicOnSocketError    bool          `env:"PANIC_ON_SOCKET_ERROR" envDefault:"true"`
}

func ParseEnv() (Config, error) {
	cfg := Config{}
	if err := env.Parse(&cfg); err != nil {
		return cfg, err
	}
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
	return cfg, nil
}
