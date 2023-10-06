package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"

	"github.com/madvikinggod/otel-semconv-checker/pkg/semconv"
	"github.com/madvikinggod/otel-semconv-checker/pkg/servers"
	"github.com/spf13/viper"
	pbLog "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	pbMetric "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	pbTrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
)

func main() {

	g, err := semconv.ParseGroups()
	if err != nil {
		slog.Error("failed to parse groups", "error", err)
		return
	}

	viper.SetConfigName("config")
	viper.AddConfigPath(".")
	if err := viper.ReadInConfig(); err != nil {
		fmt.Println(err)
		viper.SetConfigType("yaml")
		viper.ReadConfig(strings.NewReader(servers.DefaultConfig))
	}
	cfg := servers.Config{}
	if err := viper.Unmarshal(&cfg); err != nil {
		slog.Error("failed to unmarshal config", "error", err)
		return
	}

	lis, err := net.Listen("tcp", cfg.ServerAddress)
	if err != nil {
		slog.Error("failed to listen", "address", cfg.ServerAddress, "error", err)
		return
	}

	grpcServer := grpc.NewServer()
	pbTrace.RegisterTraceServiceServer(grpcServer, servers.NewTraceService(cfg, g))
	pbMetric.RegisterMetricsServiceServer(grpcServer, &metricServer{g: g})
	pbLog.RegisterLogsServiceServer(grpcServer, &logServer{g: g})

	slog.Info("starting server", "address", cfg.ServerAddress)
	if err := grpcServer.Serve(lis); err != nil {
		slog.Error("failed to serve", "error", err)
		return
	}
}

type metricServer struct {
	pbMetric.UnimplementedMetricsServiceServer
	g map[string]semconv.Group
}

func (s *metricServer) Export(ctx context.Context, req *pbMetric.ExportMetricsServiceRequest) (*pbMetric.ExportMetricsServiceResponse, error) {
	return nil, nil
}

type logServer struct {
	pbLog.UnimplementedLogsServiceServer
	g map[string]semconv.Group
}

func (s *logServer) Export(ctx context.Context, req *pbLog.ExportLogsServiceRequest) (*pbLog.ExportLogsServiceResponse, error) {
	return nil, nil
}
