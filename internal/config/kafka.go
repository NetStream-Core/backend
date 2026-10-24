package config

// kafkaConfig's BootstrapServers gates the incident correlator the same
// way TLSClientCAFile gates mTLS: empty means "not configured", and the
// backend runs exactly as it did before this feature existed, with no
// Kafka connection attempted at all.
type kafkaConfig struct {
	BootstrapServers string `env:"KAFKA_BOOTSTRAP_SERVERS"`
	DetectionsTopic  string `env:"KAFKA_DETECTIONS_TOPIC" env-default:"netstream.detections.v1"`
	AlertsTopic      string `env:"KAFKA_ALERTS_TOPIC" env-default:"netstream.alerts.v1"`
	ConsumerGroup    string `env:"KAFKA_CONSUMER_GROUP" env-default:"netstream-backend-incidents"`
}
