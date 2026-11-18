// Package observability configures logs, metrics and traces.
package observability

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/config"
)

// Telemetry owns the OpenTelemetry providers of the process.
type Telemetry struct {
	logHandler slog.Handler
	shutdown   []func(context.Context) error
}

// LogHandler returns the handler that exports logs, or nil when disabled.
func (t *Telemetry) LogHandler() slog.Handler { return t.logHandler }

// Shutdown flushes pending telemetry.
func (t *Telemetry) Shutdown(ctx context.Context) error {
	var errs []error
	for _, fn := range t.shutdown {
		errs = append(errs, fn(ctx))
	}
	return errors.Join(errs...)
}

// Setup installs the global providers. Exporters connect lazily, so an
// unavailable collector does not stop the API from starting.
func Setup(ctx context.Context, cfg config.Config, version string) (*Telemetry, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))

	telemetry := &Telemetry{}
	if !cfg.Telemetry.Enabled {
		return telemetry, nil
	}

	res, err := resource.New(ctx,
		resource.WithTelemetrySDK(),
		resource.WithAttributes(
			attribute.String("service.name", cfg.ServiceName),
			attribute.String("service.version", version),
			attribute.String("deployment.environment.name", string(cfg.Environment)),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("build telemetry resource: %w", err)
	}

	traceOptions := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(cfg.Telemetry.OTLPEndpoint)}
	metricOptions := []otlpmetricgrpc.Option{otlpmetricgrpc.WithEndpoint(cfg.Telemetry.OTLPEndpoint)}
	logOptions := []otlploggrpc.Option{otlploggrpc.WithEndpoint(cfg.Telemetry.OTLPEndpoint)}
	if cfg.Telemetry.OTLPInsecure {
		traceOptions = append(traceOptions, otlptracegrpc.WithInsecure())
		metricOptions = append(metricOptions, otlpmetricgrpc.WithInsecure())
		logOptions = append(logOptions, otlploggrpc.WithInsecure())
	}

	traceExporter, err := otlptracegrpc.New(ctx, traceOptions...)
	if err != nil {
		return nil, fmt.Errorf("create trace exporter: %w", err)
	}
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(traceExporter),
	)
	otel.SetTracerProvider(tracerProvider)
	telemetry.shutdown = append(telemetry.shutdown, tracerProvider.Shutdown)

	metricExporter, err := otlpmetricgrpc.New(ctx, metricOptions...)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("create metric exporter: %w", err), telemetry.Shutdown(ctx))
	}
	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter,
			sdkmetric.WithInterval(cfg.Telemetry.MetricInterval))),
	)
	otel.SetMeterProvider(meterProvider)
	telemetry.shutdown = append(telemetry.shutdown, meterProvider.Shutdown)

	if err := runtime.Start(runtime.WithMeterProvider(meterProvider)); err != nil {
		return nil, errors.Join(fmt.Errorf("start runtime metrics: %w", err), telemetry.Shutdown(ctx))
	}

	logExporter, err := otlploggrpc.New(ctx, logOptions...)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("create log exporter: %w", err), telemetry.Shutdown(ctx))
	}
	loggerProvider := sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExporter)),
	)
	telemetry.shutdown = append(telemetry.shutdown, loggerProvider.Shutdown)
	telemetry.logHandler = otelslog.NewHandler(cfg.ServiceName, otelslog.WithLoggerProvider(loggerProvider))

	return telemetry, nil
}
