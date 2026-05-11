package main

import (
	"encoding/json"
	"fmt"
	"os"
)

type Config struct {
	Mail                  MailConfig      `json:"mail"`
	Recipients            []string        `json:"recipients"`
	CheckIntervalMin      int             `json:"check_interval_minutes"`
	StuckThresholdMin     int             `json:"stuck_threshold_minutes"`
	AlertRepeatIntervalMin int            `json:"alert_repeat_interval_minutes"`
	LogFile               string          `json:"log_file"`
	Services              []ServiceConfig `json:"services"`
}

type MailConfig struct {
	Host    string `json:"host"`
	Port    int    `json:"port"`
	From    string `json:"from"`
	Subject string `json:"subject"`
}

type ServiceConfig struct {
	Name         string `json:"name"`
	LogDirectory string `json:"log_directory"`
	ProcessMatch string `json:"process_match"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("open config %s: %w", path, err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}

	// Defaults
	if cfg.CheckIntervalMin <= 0 {
		cfg.CheckIntervalMin = 15
	}
	if cfg.StuckThresholdMin <= 0 {
		cfg.StuckThresholdMin = 15
	}
	if cfg.AlertRepeatIntervalMin <= 0 {
		cfg.AlertRepeatIntervalMin = cfg.CheckIntervalMin
	}
	if cfg.Mail.Subject == "" {
		cfg.Mail.Subject = "[ watchdog ] java service alert"
	}
	if cfg.Mail.Port == 0 {
		cfg.Mail.Port = 25
	}

	// Validation
	if len(cfg.Services) == 0 {
		return nil, fmt.Errorf("config has no services")
	}
	if len(cfg.Recipients) == 0 {
		return nil, fmt.Errorf("config has no recipients")
	}
	if cfg.Mail.Host == "" {
		return nil, fmt.Errorf("config mail.host is required")
	}
	if cfg.Mail.From == "" {
		return nil, fmt.Errorf("config mail.from is required")
	}
	for i, s := range cfg.Services {
		if s.Name == "" {
			return nil, fmt.Errorf("service[%d]: name is required", i)
		}
		if s.LogDirectory == "" {
			return nil, fmt.Errorf("service[%d] %s: log_directory is required", i, s.Name)
		}
		if s.ProcessMatch == "" {
			return nil, fmt.Errorf("service[%d] %s: process_match is required", i, s.Name)
		}
	}
	return &cfg, nil
}