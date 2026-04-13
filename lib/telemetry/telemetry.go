// Package telemetry initialises OpenTelemetry trace and metric providers.
// Call Setup once at process start; defer the returned shutdown function so
// providers are flushed before the process exits.
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"gomario/lib/config"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Setup initialises the global OTel trace and metric providers and returns a
// shutdown function that must be called before process exit to flush buffers.
// If telemetry is disabled in config, Setup is a no-op and returns a nil-safe
// shutdown.
func Setup(ctx context.Context, cfg *config.Config) (shutdown func(context.Context) error, err error) {
	if !cfg.Telemetry.Enabled {
		return func(context.Context) error { return nil }, nil
	}

	var shutdownFuncs []func(context.Context) error

	// Combine all shutdown errors into one.
	shutdown = func(ctx context.Context) error {
		var errs []error
		for _, fn := range shutdownFuncs {
			errs = append(errs, fn(ctx))
		}
		return errors.Join(errs...)
	}

	// Propagator: W3C TraceContext + Baggage headers.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// ── Trace provider ──────────────────────────────────────────────────────

	var traceExporters []sdktrace.SpanExporter

	// OTLP HTTP trace exporter (production + dev when a collector is running).
	otlpTraceExp, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpoint(cfg.Telemetry.Endpoint),
		otlptracehttp.WithInsecure(),
	)
	if err != nil {
		return shutdown, fmt.Errorf("telemetry: otlp trace exporter: %w", err)
	}
	traceExporters = append(traceExporters, otlpTraceExp)

	// In development also write spans to stdout so traces are visible without
	// a running collector.
	if !cfg.IsProduction {
		stdExp, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
		if err != nil {
			return shutdown, fmt.Errorf("telemetry: stdout trace exporter: %w", err)
		}
		traceExporters = append(traceExporters, stdExp)
	}

	var traceOpts []sdktrace.TracerProviderOption
	traceOpts = append(traceOpts, sdktrace.WithResource(newResource(cfg.Telemetry.ServiceName)))
	for _, exp := range traceExporters {
		traceOpts = append(traceOpts, sdktrace.WithBatcher(exp))
	}

	tp := sdktrace.NewTracerProvider(traceOpts...)
	shutdownFuncs = append(shutdownFuncs, tp.Shutdown)
	otel.SetTracerProvider(tp)

	// ── Metric provider ─────────────────────────────────────────────────────

	metricExp, err := otlpmetrichttp.New(ctx,
		otlpmetrichttp.WithEndpoint(cfg.Telemetry.Endpoint),
		otlpmetrichttp.WithInsecure(),
	)
	if err != nil {
		return shutdown, fmt.Errorf("telemetry: otlp metric exporter: %w", err)
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(newResource(cfg.Telemetry.ServiceName)),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp)),
	)
	shutdownFuncs = append(shutdownFuncs, mp.Shutdown)
	otel.SetMeterProvider(mp)

	return shutdown, nil
}
