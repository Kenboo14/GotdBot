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

	SQLite struct {
		Path string `yaml:"sqlite_path"`
	} `yaml:"sqlite"`
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
	loadDotEnv()

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
	if value := os.Getenv("MONGO_URI"); value != "" {
		cfg.Mongo.URI = value
	} else if value := os.Getenv("MONGO_BACKUP_URI"); value != "" {
		cfg.Mongo.URI = value
	}
	if value := os.Getenv("MONGO_DB"); value != "" {
		cfg.Mongo.DBName = value
	} else if value := os.Getenv("MONGO_BACKUP_DB"); value != "" {
		cfg.Mongo.DBName = value
	}
	if cfg.Mongo.DBName == "" {
		cfg.Mongo.DBName = "gotdbot"
	}
	if value := os.Getenv("SQLITE_PATH"); value != "" {
		cfg.SQLite.Path = value
	}
	if cfg.SQLite.Path == "" {
		cfg.SQLite.Path = "userbot.db"
	}
}

func loadDotEnv() {
	paths := []string{".env", "../.env"}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err == nil {
			lines := strings.Split(string(data), "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				idx := strings.Index(line, "=")
				if idx > 0 {
					k := strings.TrimSpace(line[:idx])
					v := strings.TrimSpace(line[idx+1:])
					v = strings.Trim(v, `"'`)
					if os.Getenv(k) == "" {
						os.Setenv(k, v)
					}
				}
			}
			break
		}
	}
}
