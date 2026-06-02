package config

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

const (
	DefaultHost          = "0.0.0.0"
	DefaultPort          = 50052
	DefaultMaxResults    = 50
	DefaultMinRetry      = 1000
	DefaultMaxRetry      = 60000
	DefaultRateLimit     = 25.0
	DefaultConfigPath    = "config/dev.yaml"
	DefaultLogLevel      = "info"
	DefaultDBHost        = "localhost"
	DefaultDBPort        = 5432
	DefaultDBUser        = "postgres"
	DefaultDBPassword    = "password"
	DefaultDBName        = "jira_connector"
	DefaultMetricsAddr   = ":9090"
	DefaultMetricsEnable = true
	DefaultPprofAddr     = ":6060"
	DefaultPprofEnable   = false
)

var ErrJiraBaseURLRequired = errors.New("jira.baseUrl is required")

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

// MetricsConfig configures the Prometheus /metrics scrape endpoint.
type MetricsConfig struct {
	Enabled bool   `yaml:"enabled"`
	Addr    string `yaml:"addr"`
}

type PprofConfig struct {
	Enabled bool   `yaml:"enabled"`
	Addr    string `yaml:"addr"`
}

type AppConfig struct {
	Jira    JiraConfig    `yaml:"jira"`
	DB      DBConfig      `yaml:"db"`
	Metrics MetricsConfig `yaml:"metrics"`
	Pprof   PprofConfig   `yaml:"pprof"`
	App     struct {
		LogLevel string `yaml:"logLevel"`
	} `yaml:"app"`
}

func Load(path string) (*AppConfig, error) {
	if path == "" {
		path = envOr("CONNECTOR_CONFIG", DefaultConfigPath)
	}

	var cfg AppConfig

	// A missing config file is not fatal: the whole config can be supplied via
	// the environment (.env). This lets the service run env-only on deploy
	// targets that do not ship a YAML (e.g. Timeweb App Platform). Only a real
	// read/parse error is surfaced.
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		err = yaml.Unmarshal(data, &cfg)
		if err != nil {
			return nil, fmt.Errorf("unmarshal config: %w", err)
		}
	case !os.IsNotExist(err):
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}

	applyDefaults(&cfg)
	overrideFromEnv(&cfg)

	err = validate(&cfg)
	if err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	return &cfg, nil
}

func applyDefaults(cfg *AppConfig) {
	applyJiraDefaults(&cfg.Jira)
	applyDBDefaults(&cfg.DB)
	applyMetricsDefaults(&cfg.Metrics)
	applyPprofDefaults(&cfg.Pprof)

	if cfg.App.LogLevel == "" {
		cfg.App.LogLevel = DefaultLogLevel
	}
}

func applyMetricsDefaults(cfg *MetricsConfig) {
	// When no metrics block was present in the YAML the addr is empty. Apply
	// full defaults including enabling the endpoint. An explicit YAML block
	// (even with just addr set) preserves the user's enabled choice.
	if cfg.Addr == "" {
		cfg.Addr = DefaultMetricsAddr
		cfg.Enabled = DefaultMetricsEnable
	}
}

func applyPprofDefaults(cfg *PprofConfig) {
	if cfg.Addr == "" {
		cfg.Addr = DefaultPprofAddr
	}
}

func applyJiraDefaults(cfg *JiraConfig) {
	if cfg.MaxResults <= 0 {
		cfg.MaxResults = DefaultMaxResults
	}

	if cfg.MinRetryDelay <= 0 {
		cfg.MinRetryDelay = DefaultMinRetry
	}

	if cfg.MaxRetryDelay <= 0 {
		cfg.MaxRetryDelay = DefaultMaxRetry
	}

	if cfg.RateLimitPerSec <= 0 {
		cfg.RateLimitPerSec = DefaultRateLimit
	}
}

func applyDBDefaults(cfg *DBConfig) {
	if cfg.Host == "" {
		cfg.Host = DefaultDBHost
	}

	if cfg.Port == 0 {
		cfg.Port = DefaultDBPort
	}

	if cfg.User == "" {
		cfg.User = DefaultDBUser
	}

	if cfg.Password == "" {
		cfg.Password = DefaultDBPassword
	}

	if cfg.DBName == "" {
		cfg.DBName = DefaultDBName
	}
}

func overrideFromEnv(cfg *AppConfig) {
	if v := os.Getenv("JIRA_BASE_URL"); v != "" {
		cfg.Jira.BaseURL = v
	}

	if v := os.Getenv("JIRA_TOKEN"); v != "" {
		cfg.Jira.Token = v
	}

	if v := os.Getenv("DB_HOST"); v != "" {
		cfg.DB.Host = v
	}

	if v := os.Getenv("DB_PORT"); v != "" {
		cfg.DB.Port = parseInt(v, DefaultDBPort)
	}

	if v := os.Getenv("DB_USER"); v != "" {
		cfg.DB.User = v
	}

	if v := os.Getenv("DB_PASSWORD"); v != "" {
		cfg.DB.Password = v
	}

	if v := os.Getenv("DB_NAME"); v != "" {
		cfg.DB.DBName = v
	}

	if v := os.Getenv("LOG_LEVEL"); v != "" {
		cfg.App.LogLevel = v
	}

	overrideObsFromEnv(cfg)
}

func overrideObsFromEnv(cfg *AppConfig) {
	if v := os.Getenv("METRICS_ADDR"); v != "" {
		cfg.Metrics.Addr = v
	}

	if v := os.Getenv("PPROF_ADDR"); v != "" {
		cfg.Pprof.Addr = v
	}
}

func validate(cfg *AppConfig) error {
	var errs []error

	if cfg.Jira.BaseURL == "" {
		errs = append(errs, ErrJiraBaseURLRequired)
	}

	return errors.Join(errs...)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}

	return fallback
}

func parseInt(s string, fallback int) int {
	var result int

	_, err := fmt.Sscanf(s, "%d", &result)
	if err != nil {
		return fallback
	}

	return result
}
