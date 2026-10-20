package config

type serverConfig struct {
	Port        string `env:"SERVER_PORT" env-default:"9999"`
	TLSCertFile string `env:"TLS_CERT_FILE"`
	TLSKeyFile  string `env:"TLS_KEY_FILE"`
	// Enables mTLS: a client certificate signed by this CA is required for
	// every API request, and its Subject identifies the caller (CommonName)
	// and role (OrganizationalUnit "operator" vs anything else = sensor).
	// Requires TLSCertFile/TLSKeyFile to also be set.
	TLSClientCAFile string `env:"TLS_CLIENT_CA_FILE"`
}
