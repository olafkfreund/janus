package telemetry

import (
	"context"
	"net/http"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// TestTraceContextPropagation guards the exact failure mode that motivated
// this wiring: if InitTelemetry forgets to register a text-map propagator,
// otel's Extract silently no-ops and inbound traces are dropped on the floor.
func TestTraceContextPropagation(t *testing.T) {
	if _, err := InitTelemetry("test"); err != nil {
		t.Fatalf("InitTelemetry: %v", err)
	}

	const wantTrace = "4bf92f3577b34da6a3ce929d0e0e4736"
	h := http.Header{}
	h.Set("traceparent", "00-"+wantTrace+"-00f067aa0ba902b7-01")

	ctx := otel.GetTextMapPropagator().Extract(context.Background(), propagation.HeaderCarrier(h))

	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		t.Fatal("no span context extracted — propagator not registered?")
	}
	if got := sc.TraceID().String(); got != wantTrace {
		t.Fatalf("trace id = %q, want %q", got, wantTrace)
	}
}
