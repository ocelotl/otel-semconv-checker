// SPDX-License-Identifier: Apache-2.0

package servers

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/madvikinggod/otel-semconv-checker/pkg/semconv"
	pbCollectorMetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	v1 "go.opentelemetry.io/proto/otlp/common/v1"
	pbMetrics "go.opentelemetry.io/proto/otlp/metrics/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type MetricsServer struct {
	pbCollectorMetrics.UnimplementedMetricsServiceServer

	resource        matchDef
	matches         []matchDef
	reportUnmatched bool

	disableError bool
}

func NewMetricsService(cfg Config, svs map[string]semconv.SemanticVersion) *MetricsServer {
	_, found := svs[cfg.Resource.SemanticVersion]
	if cfg.Resource.SemanticVersion != "" && !found {
		cfg.Resource.SemanticVersion = semconv.DefaultVersion
	}

	resSemVer := cfg.Resource.SemanticVersion
	if !found {
		resSemVer = semconv.DefaultVersion
	}
	resource := newMatchDef(cfg.Resource, svs[resSemVer].Groups)

	matches := []matchDef{}
	for _, match := range cfg.Metrics {
		groups, ok := svs[match.SemanticVersion]
		if !ok {
			match.SemanticVersion = semconv.DefaultVersion
		}
		matches = append(matches, newMatchDef(match, groups.Groups))
	}

	return &MetricsServer{
		resource:        resource,
		matches:         matches,
		reportUnmatched: cfg.ReportUnmatched,
		disableError:    cfg.DisableError,
	}
}

func (s *MetricsServer) Export(ctx context.Context, req *pbCollectorMetrics.ExportMetricsServiceRequest) (*pbCollectorMetrics.ExportMetricsServiceResponse, error) {
	if req == nil {
		return nil, nil
	}
	log := slog.With("type", "metrics")
	count := 0
	names := []string{}
	for _, r := range req.ResourceMetrics {
		if s.resource.semVer != nil && *s.resource.semVer != "" && r.SchemaUrl != *s.resource.semVer {
			log.Info("incorrect resource version",
				slog.String("section", "resource"),
				slog.String("version", r.SchemaUrl),
				slog.String("expected", *s.resource.semVer),
			)
		}
		if r.Resource != nil {
			log := log.With(
				slog.String("section", "resource"),
				slog.String("version", r.SchemaUrl),
			)

			s.resource.compareAttributes(log, r.Resource.Attributes)
		}

		for _, scope := range r.ScopeMetrics {
			log := log.With(slog.String("section", "metric"))
			if name := scope.GetScope().GetName(); name != "" {
				log = log.With(slog.String("scope.name", name))
			}
			for _, metric := range scope.Metrics {
				found := false
				log := log.With(slog.String("name", metric.Name))
				if url := scope.GetSchemaUrl(); url != "" {
					log = log.With(slog.String("schema", url))
				}
				log.Error("Got metric")

				for _, match := range s.matches {
					missing, matched := checkMetric(log, match, metric, scope.GetScope(), r.GetResource())
					found = found || matched
					count += missing
					if missing > 0 {
						names = append(names, fmt.Sprintf("%s/%s", scope.Scope.GetName(), metric.GetName()))
					}
				}
				if !found && s.reportUnmatched {
					log.Info("unmatched metric")
				}
			}
		}
	}

	if count > 0 && !s.disableError {
		return &pbCollectorMetrics.ExportMetricsServiceResponse{
			PartialSuccess: &pbCollectorMetrics.ExportMetricsPartialSuccess{
				RejectedDataPoints: int64(count),
				ErrorMessage:       "missing attributes",
			},
		}, status.Error(codes.FailedPrecondition, fmt.Sprintf("missing attributes: %v", names))
	}

	return &pbCollectorMetrics.ExportMetricsServiceResponse{}, nil
}

func checkMetric(log *slog.Logger, match matchDef, metric *pbMetrics.Metric, scope, resource attributeGetter) (int, bool) {
	name := metric.GetName()
	if !match.isNameMatch(name) {
		return 0, false
	}

	switch d := metric.Data.(type) {
	case *pbMetrics.Metric_Gauge:
		return checkDataPoints(log, match, d.Gauge, scope, resource)
	case *pbMetrics.Metric_Sum:
		return checkDataPoints(log, match, d.Sum, scope, resource)
	case *pbMetrics.Metric_Histogram:
		return checkDataPoints(log, match, d.Histogram, scope, resource)
	case *pbMetrics.Metric_Summary:
		return checkDataPoints(log, match, d.Summary, scope, resource)
	case *pbMetrics.Metric_ExponentialHistogram:
		return checkDataPoints(log, match, d.ExponentialHistogram, scope, resource)
	default:
		log.Warn("Unsupported metric type: %t", metric.Data)
	}
	return 0, false
}

func checkDataPoints[T attributeGetter, D dataPointGetter[T]](log *slog.Logger, match matchDef, metric D, scope, resource attributeGetter) (int, bool) {
	found := false
	count := 0
	for _, p := range metric.GetDataPoints() {
		if !match.isAttrMatch(p.GetAttributes()) {
			continue
		}
		missing := match.compareAttributes(log, p.GetAttributes(), scope.GetAttributes(), resource.GetAttributes())
		found = true
		count += missing
	}
	return count, found
}

type attributeGetter interface {
	GetAttributes() []*v1.KeyValue
}

var _ attributeGetter = &pbMetrics.NumberDataPoint{}
var _ attributeGetter = &pbMetrics.HistogramDataPoint{}
var _ attributeGetter = &pbMetrics.SummaryDataPoint{}
var _ attributeGetter = &pbMetrics.ExponentialHistogramDataPoint{}

type dataPointGetter[T attributeGetter] interface {
	GetDataPoints() []T
}

var _ dataPointGetter[*pbMetrics.NumberDataPoint] = &pbMetrics.Gauge{}
var _ dataPointGetter[*pbMetrics.NumberDataPoint] = &pbMetrics.Sum{}
var _ dataPointGetter[*pbMetrics.HistogramDataPoint] = &pbMetrics.Histogram{}
var _ dataPointGetter[*pbMetrics.SummaryDataPoint] = &pbMetrics.Summary{}
var _ dataPointGetter[*pbMetrics.ExponentialHistogramDataPoint] = &pbMetrics.ExponentialHistogram{}
