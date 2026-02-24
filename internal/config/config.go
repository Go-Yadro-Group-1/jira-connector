package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type JiraConfig struct {
	BaseURL         string  `yaml:"baseUrl"`
	Token           string  `yaml:"token"`
	MaxResults      int     `yaml:"maxResults"`
	MinRetryDelay   int     `yaml:"minRetryDelay"`
	MaxRetryDelay   int     `yaml:"maxRetryDelay"`
	RateLimitPerSec float64 `yaml:"rateLimitPerSec"`
}

type DBConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	DBName   string `yaml:"dbname"`
}

type AppConfig struct {
	Jira JiraConfig `yaml:"jira"`
	DB   DBConfig   `yaml:"db"`
	App  struct {
		LogLevel string `yaml:"logLevel"`
	} `yaml:"app"`
}

func LoadDevConfig() (*AppConfig, error) {
	data, err := os.ReadFile("config/dev.yaml")
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg AppConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config YAML: %w", err)
	}

	return &cfg, nil
}
