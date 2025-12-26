package config

import (
	"log"

	"os"

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
	} `yaml:"redis"`
}

func Load(path string) (*Config, error) {
	cfg := &Config{}

	file, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	if err := yaml.Unmarshal(file, cfg); err != nil {
		return nil, err
	}

	log.Println("config loaded:", path)

	return cfg, nil
}
