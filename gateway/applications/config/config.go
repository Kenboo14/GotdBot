package config

import (
	"log"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Telegram struct {
		BotToken string `yaml:"bot_token"`
	} `yaml:"telegram"`

	Mongo struct {
		URI    string `yaml:"mongo_uri"`
		DBName string `yaml:"mongo_db"`
	} `yaml:"mongo"`

	Redis struct {
		Address  string `yaml:"redis_address"`
		Password string `yaml:"redis_password"`
		TLS      bool   `yaml:"redis_tls"`
	} `yaml:"redis"`
}

func Load(path string) (*Config, error) {
	cfg := &Config{}

	file, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err == nil {
		if err := yaml.Unmarshal(file, cfg); err != nil {
			return nil, err
		}
	}
	applyEnv(cfg)
	if strings.TrimSpace(cfg.Telegram.BotToken) == "" {
		return nil, os.ErrInvalid
	}
	if strings.TrimSpace(cfg.Redis.Address) == "" {
		return nil, os.ErrInvalid
	}

	log.Println("config loaded:", path)

	return cfg, nil
}

func applyEnv(cfg *Config) {
	if value := os.Getenv("BOT_TOKEN"); value != "" {
		cfg.Telegram.BotToken = value
	}
	if value := os.Getenv("REDIS_ADDR"); value != "" {
		cfg.Redis.Address = value
	}
	if value := os.Getenv("REDIS_PASS"); value != "" {
		cfg.Redis.Password = value
	}
	if value := os.Getenv("REDIS_TLS"); value != "" {
		cfg.Redis.TLS = strings.EqualFold(value, "true") || value == "1"
	}
}
