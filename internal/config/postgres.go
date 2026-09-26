package config

import "fmt"

type postgresConfig struct {
	Host     string `env:"POSTGRES_HOST"     env-default:"localhost"`
	Port     string `env:"POSTGRES_PORT"     env-default:"5432"`
	Username string `env:"POSTGRES_USER"     env-default:"netstream"`
	Password string `env:"POSTGRES_PASSWORD" env-default:"netstream-dev"`
	Name     string `env:"POSTGRES_DB"       env-default:"netstream_control_plane"`
	SSLMode  string `env:"POSTGRES_SSLMODE"  env-default:"disable"`
}

func (c *postgresConfig) BuildDSN() string {
	return fmt.Sprintf("postgresql://%s:%s@%s:%s/%s?sslmode=%s",
		c.Username,
		c.Password,
		c.Host,
		c.Port,
		c.Name,
		c.SSLMode)
}
