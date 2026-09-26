package config

type objectStoreConfig struct {
	Endpoint  string `env:"OBJECTSTORE_ENDPOINT"   env-default:"localhost:8333"`
	AccessKey string `env:"OBJECTSTORE_ACCESS_KEY" env-default:"netstream"`
	SecretKey string `env:"OBJECTSTORE_SECRET_KEY" env-default:"netstream-dev"`
	Bucket    string `env:"OBJECTSTORE_BUCKET"     env-default:"netstream-models"`
	UseSSL    bool   `env:"OBJECTSTORE_USE_SSL"    env-default:"false"`
}
