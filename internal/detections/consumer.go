// Package detections consumes netstream.detections.v1 — the same topic
// ml's online detector publishes to and ClickHouse's Kafka-engine table
// reads directly — on its own consumer group, so backend joining this
// stream never competes with or blocks either of those existing readers.
// Every attack detection becomes (or extends) an incidents.Incident; a
// freshly opened incident is published to netstream.alerts.v1 for anyone
// downstream who only cares about new incidents, not every detection.
package detections

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"network-monitor-backend/internal/incidents"
	"network-monitor-backend/internal/logger"

	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"
)

// message mirrors ml's netstream_ml.detector.DetectionResult.to_dict() —
// see ml/docs/online-detector.md, "Формат DetectionResult".
type message struct {
	HostID    string  `json:"host_id"`
	SrcIP     string  `json:"src_ip"`
	Score     float64 `json:"score"`
	IsAttack  bool    `json:"is_attack"`
	LatencyMs float64 `json:"latency_ms"`
}

// Alert is published to netstream.alerts.v1 when RecordDetection opens a
// brand new incident (not on every detection that merely extends one
// already open) — the alerts topic exists so a consumer can act once per
// incident instead of once per detection.
type Alert struct {
	IncidentID int64     `json:"incident_id"`
	HostID     string    `json:"host_id"`
	SrcIP      string    `json:"src_ip"`
	Severity   string    `json:"severity"`
	OpenedAt   time.Time `json:"opened_at"`
}

type Config struct {
	BootstrapServers string
	DetectionsTopic  string
	AlertsTopic      string
	ConsumerGroup    string
}

// Run blocks, consuming Config.DetectionsTopic until ctx is cancelled.
// Call it in its own goroutine; a consumption or publish error for one
// message is logged and skipped, not fatal — a single malformed or
// unreachable-broker message should not take the whole backend process
// down over a feed it only observes, not serves.
func Run(ctx context.Context, cfg Config, svc *incidents.Service) {
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers: []string{cfg.BootstrapServers},
		Topic:   cfg.DetectionsTopic,
		GroupID: cfg.ConsumerGroup,
	})
	defer func() {
		if err := reader.Close(); err != nil {
			logger.Logger.Warn("Failed to close the detections reader", zap.Error(err))
		}
	}()

	writer := &kafka.Writer{
		Addr:  kafka.TCP(cfg.BootstrapServers),
		Topic: cfg.AlertsTopic,
	}
	defer func() {
		if err := writer.Close(); err != nil {
			logger.Logger.Warn("Failed to close the alerts writer", zap.Error(err))
		}
	}()

	logger.Logger.Info("Incident correlator started",
		zap.String("detections_topic", cfg.DetectionsTopic),
		zap.String("alerts_topic", cfg.AlertsTopic),
		zap.String("consumer_group", cfg.ConsumerGroup),
	)

	for {
		kafkaMsg, err := reader.ReadMessage(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			logger.Logger.Warn("Failed to read a detection from Kafka", zap.Error(err))
			continue
		}

		var msg message
		if err := json.Unmarshal(kafkaMsg.Value, &msg); err != nil {
			logger.Logger.Warn("Skipping an undecodable detection message", zap.Error(err))
			continue
		}
		if !msg.IsAttack {
			continue
		}

		incident, opened, err := svc.RecordDetection(ctx, incidents.Detection{
			HostID: msg.HostID,
			SrcIP:  msg.SrcIP,
			Score:  msg.Score,
			At:     time.Now().UTC(),
		})
		if err != nil {
			logger.Logger.Error("Failed to record a detection as an incident", zap.Error(err))
			continue
		}
		if !opened {
			continue
		}

		alert := Alert{
			IncidentID: incident.ID,
			HostID:     incident.HostID,
			SrcIP:      incident.SrcIP,
			Severity:   incident.Severity,
			OpenedAt:   incident.FirstSeen,
		}
		payload, err := json.Marshal(alert)
		if err != nil {
			logger.Logger.Error("Failed to marshal an alert", zap.Error(err))
			continue
		}
		if err := writer.WriteMessages(ctx, kafka.Message{Value: payload}); err != nil {
			logger.Logger.Error("Failed to publish an alert", zap.Error(err), zap.Int64("incident_id", incident.ID))
		}
	}
}
