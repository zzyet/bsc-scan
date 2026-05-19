package config

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type DatabaseConfig struct {
	DSN            string `yaml:"dsn"`
	MaxConnections int    `yaml:"max_connections"`
}

type ScannerConfig struct {
	StartBlock     int64         `yaml:"start_block"`
	WorkerCount    int           `yaml:"worker_count"`
	BatchSize      int           `yaml:"batch_size"`
	FetchBatchSize int           `yaml:"fetch_batch_size"`
	PollInterval   time.Duration `yaml:"poll_interval"`
}

type Config struct {
	Database     DatabaseConfig `yaml:"database"`
	Scanner      ScannerConfig  `yaml:"scanner"`
	SyncInterval time.Duration  `yaml:"sync_interval"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
		cfg := &Config{
		Scanner: ScannerConfig{
			WorkerCount:    5,
			BatchSize:      50,
			FetchBatchSize: 500,
			PollInterval:   3 * time.Second,
		},
		SyncInterval: 5 * time.Minute,
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
