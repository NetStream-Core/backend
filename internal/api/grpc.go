package api

import (
	"context"
	"net"
	"network-monitor-backend/internal/logger"
	"network-monitor-backend/internal/storage"
	"network-monitor-backend/proto"

	"github.com/klauspost/compress/zstd"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	pbproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

type GRPCServer struct {
	proto.UnimplementedMetricsServiceServer
	storage storage.Storage
}

func NewGRPCServer(s storage.Storage) *GRPCServer {
	return &GRPCServer{storage: s}
}

func (s *GRPCServer) SendMetrics(ctx context.Context, req *proto.CompressedMetricsBatch) (*emptypb.Empty, error) {
	decoder, err := zstd.NewReader(nil)
	if err != nil {
		logger.Logger.Error("Failed to create zstd decoder", zap.Error(err))
		return nil, err
	}
	defer decoder.Close()

	decompressed, err := decoder.DecodeAll(req.CompressedData, nil)
	if err != nil {
		logger.Logger.Error("Failed to decompress zstd data", zap.Error(err))
		return nil, err
	}

	var batch proto.MetricsBatch
	if err := pbproto.Unmarshal(decompressed, &batch); err != nil {
		logger.Logger.Error("Failed to deserialize metrics batch", zap.Error(err))
		return nil, err
	}

	logger.Logger.Info("Received batch", zap.Int("metrics_count", len(batch.Metrics)))
	for _, metric := range batch.Metrics {
		if err := s.storage.Write(metric); err != nil {
			logger.Logger.Error("Failed to write metric to storage", zap.Error(err))
			return nil, err
		}
		logger.Logger.Debug("Metric written to database",
			zap.Uint32("protocol", metric.Protocol),
			zap.String("src_ip", metric.SrcIp),
			zap.Uint64("count", metric.Count),
			zap.String("dst_ip", metric.DstIp),
			zap.Uint32("src_port", metric.SrcPort),
			zap.Uint32("dst_port", metric.DstPort),
			zap.Uint64("timestamp", metric.Timestamp),
			zap.Uint32("payload_size", metric.PayloadSize))
	}
	return &emptypb.Empty{}, nil
}

func RunGRPCServer(s storage.Storage, addr string) error {
	logger.Logger.Debug("Attempting to start gRPC server on " + addr)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		logger.Logger.Error("Failed to listen", zap.Error(err))
		return err
	}
	grpcServer := grpc.NewServer()
	proto.RegisterMetricsServiceServer(grpcServer, NewGRPCServer(s))
	logger.Logger.Info("gRPC server started", zap.String("address", addr))
	return grpcServer.Serve(lis)
}
