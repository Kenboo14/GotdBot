package config

import (
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Telegram struct {
		BotToken    string `yaml:"bot_token"`
		LogBotToken string `yaml:"logbot_token"`
	} `yaml:"telegram"`

	Sqlite struct {
		Path string `yaml:"path"`
	} `yaml:"sqlite"`

	MongoBackup struct {
		URI    string `yaml:"uri"`
		DBName string `yaml:"db_name"`
	} `yaml:"mongo_backup"`

	// Backward compatibility field
	Mongo struct {
		URI    string `yaml:"mongo_uri"`
		DBName string `yaml:"mongo_db"`
	} `yaml:"mongo"`

	Redis struct {
		Address  string `yaml:"redis_address"`
		Password string `yaml:"redis_password"`
		TLS      bool   `yaml:"redis_tls"`
	} `yaml:"redis"`

	Userbot struct {
		APIID                int    `yaml:"api_id"`
		APIHash              string `yaml:"api_hash"`
		SessionEncryptionKey string `yaml:"session_encryption_key"`
	} `yaml:"userbot"`

	Cache struct {
		GroupCache     string `yaml:"group_cache"`
		NotesCache     int    `yaml:"notes_cache"`
		TeksCache      int    `yaml:"teks_cache"`
		WatermarkCache int    `yaml:"watermark_cache"`
		SessionCache   int    `yaml:"session_cache"`
	} `yaml:"cache"`
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
	if strings.TrimSpace(cfg.Redis.Address) == "" {
		return nil, os.ErrInvalid
	}
	if cfg.Userbot.APIID < 1 || strings.TrimSpace(cfg.Userbot.APIHash) == "" {
		return nil, fmt.Errorf("userbot API_ID dan API_HASH wajib diisi")
	}
	key, err := base64.StdEncoding.DecodeString(cfg.Userbot.SessionEncryptionKey)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("SESSION_ENCRYPTION_KEY harus base64 dari tepat 32 byte")
	}

	log.Println("config loaded:", path)

	return cfg, nil
}

func applyEnv(cfg *Config) {
	if value := os.Getenv("SQLITE_PATH"); value != "" {
		cfg.Sqlite.Path = value
	}
	if cfg.Sqlite.Path == "" {
		cfg.Sqlite.Path = "userbot.db"
	}

	if value := os.Getenv("MONGO_BACKUP_URI"); value != "" {
		cfg.MongoBackup.URI = value
	} else if value := os.Getenv("MONGO_URI"); value != "" {
		cfg.MongoBackup.URI = value
	}

	if value := os.Getenv("MONGO_BACKUP_DB"); value != "" {
		cfg.MongoBackup.DBName = value
	} else if value := os.Getenv("MONGO_DB"); value != "" {
		cfg.MongoBackup.DBName = value
	}
	if cfg.MongoBackup.DBName == "" {
		cfg.MongoBackup.DBName = "gotdbot"
	}

	// Keep Mongo struct populated for backward compatibility
	cfg.Mongo.URI = cfg.MongoBackup.URI
	cfg.Mongo.DBName = cfg.MongoBackup.DBName

	if value := os.Getenv("BOT_TOKEN"); value != "" {
		cfg.Telegram.BotToken = value
	}
	if value := os.Getenv("LOGBOT_TOKEN"); value != "" {
		cfg.Telegram.LogBotToken = value
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
	if value := os.Getenv("API_ID"); value != "" {
		_, _ = fmt.Sscanf(value, "%d", &cfg.Userbot.APIID)
	}
	if value := os.Getenv("API_HASH"); value != "" {
		cfg.Userbot.APIHash = value
	}
	if value := os.Getenv("SESSION_ENCRYPTION_KEY"); value != "" {
		cfg.Userbot.SessionEncryptionKey = value
	}

	if value := os.Getenv("GROUP_CACHE"); value != "" {
		cfg.Cache.GroupCache = value
	}
	if cfg.Cache.GroupCache == "" {
		cfg.Cache.GroupCache = "AiotdCaches"
	}

	if value := os.Getenv("NOTES_CACHE"); value != "" {
		_, _ = fmt.Sscanf(value, "%d", &cfg.Cache.NotesCache)
	}
	if cfg.Cache.NotesCache <= 0 {
		cfg.Cache.NotesCache = 7157
	}

	if value := os.Getenv("TEKS_CACHE"); value != "" {
		_, _ = fmt.Sscanf(value, "%d", &cfg.Cache.TeksCache)
	}
	if cfg.Cache.TeksCache <= 0 {
		cfg.Cache.TeksCache = 3325
	}

	if value := os.Getenv("WATERMARK_CACHE"); value != "" {
		_, _ = fmt.Sscanf(value, "%d", &cfg.Cache.WatermarkCache)
	}
	if cfg.Cache.WatermarkCache <= 0 {
		cfg.Cache.WatermarkCache = 6847
	}

	if value := os.Getenv("SESSION_CACHE"); value != "" {
		_, _ = fmt.Sscanf(value, "%d", &cfg.Cache.SessionCache)
	}
	if cfg.Cache.SessionCache <= 0 {
		cfg.Cache.SessionCache = 7073
	}
}
