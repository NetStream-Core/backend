package config

type serverConfig struct {
	Port        string `env:"SERVER_PORT" env-default:"9999"`
	TLSCertFile string `env:"TLS_CERT_FILE"`
	TLSKeyFile  string `env:"TLS_KEY_FILE"`
}
