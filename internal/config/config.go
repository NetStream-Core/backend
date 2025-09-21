package config

import (
	"github.com/ilyakaznacheev/cleanenv"
)

type Config struct {
	Database databaseConfig
	Server   serverConfig
}

func NewConfig() (*Config, error) {
	var cfg Config

	if err := cleanenv.ReadConfig(".env", &cfg); err != nil {
		if err := cleanenv.ReadEnv(&cfg); err != nil {
			return nil, err
		}
	}

	return &cfg, nil
}
