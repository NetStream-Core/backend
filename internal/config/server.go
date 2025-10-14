package config

type serverConfig struct {
	Port string `env:"SERVER_PORT" env-default:"50051"`
}
