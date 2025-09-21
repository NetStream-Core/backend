package config

import "fmt"

type databaseConfig struct {
	Host     string `env:"DB_HOST"     env-default:"localhost"`
	Port     string `env:"DB_PORT"     env-default:"5432"`
	Username string `env:"DB_USERNAME" env-default:"postgres"`
	Password string `env:"DB_PASSWORD" env-default:"1"`
	Name     string `env:"DB_NAME"     env-default:"postgres"`
}

func (c *databaseConfig) BuildDSN() string {
	return fmt.Sprintf("postgresql://%s:%s@127.0.0.1:%s/%s",
		c.Username,
		c.Password,
		c.Port,
		c.Name)
}
