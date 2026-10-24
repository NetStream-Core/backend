package config

// pkiConfig gates certificate issuance the same way kafkaConfig gates the
// incident correlator: empty CACertFile means "not configured", and the
// backend runs exactly as before this feature existed.
type pkiConfig struct {
	CACertFile       string `env:"CA_CERT_FILE"`
	CAKeyFile        string `env:"CA_KEY_FILE"`
	CertValidityDays int    `env:"CERT_VALIDITY_DAYS" env-default:"90"`
}
