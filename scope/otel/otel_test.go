// SPDX-License-Identifier: MIT

package otel

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	drv "github.com/CorkCyber/athenadriver/v2/go"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/trace"
)

func TestAdapterSatisfiesScope(t *testing.T) {
	var _ drv.Scope = New(otel.Meter("test"))
}

// TestAdapterLiveFire wires the adapter to a ManualReader-backed
// MeterProvider and asserts the emitted counter + histogram show up with
// the values we recorded: end-to-end proof the Int64Counter and
// Float64Histogram plumbing actually reaches otel's collector layer.
func TestAdapterLiveFire(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	defer provider.Shutdown(context.Background())

	s := New(provider.Meter("athenadriver.test"))
	s.Counter("driver.calls").Inc(3)
	s.Counter("driver.calls").Inc(2) // second lookup exercises the cache
	s.Timer("driver.latency").Record(42 * time.Millisecond)
	s.Timer("driver.latency").Record(8 * time.Millisecond)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(rm.ScopeMetrics) == 0 {
		t.Fatal("no scope metrics collected")
	}

	var counterSum int64
	var histCount uint64
	var histSum float64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			switch data := m.Data.(type) {
			case metricdata.Sum[int64]:
				if m.Name == "driver.calls" {
					for _, dp := range data.DataPoints {
						counterSum += dp.Value
					}
				}
			case metricdata.Histogram[float64]:
				if m.Name == "driver.latency" {
					for _, dp := range data.DataPoints {
						histCount += dp.Count
						histSum += dp.Sum
					}
				}
			}
		}
	}
	if counterSum != 5 {
		t.Errorf("counter sum = %d, want 5", counterSum)
	}
	if histCount != 2 {
		t.Errorf("histogram count = %d, want 2", histCount)
	}
	// Record() writes milliseconds. 42 + 8 = 50. Allow float slack.
	if histSum < 49.9 || histSum > 50.1 {
		t.Errorf("histogram sum = %v ms, want ~50", histSum)
	}
}

// TestAdapterCacheHit pins the two properties of the cache: a repeat
// lookup returns the identical wrapper (so the instrument was built once)
// and costs zero allocations (no fresh wrapper escaping to the heap).
func TestAdapterCacheHit(t *testing.T) {
	provider := metric.NewMeterProvider(metric.WithReader(metric.NewManualReader()))
	defer provider.Shutdown(context.Background())
	s := New(provider.Meter("cache.test"))

	if a, b := s.Counter("k"), s.Counter("k"); a != b {
		t.Errorf("Counter(%q) returned different wrappers: %#v vs %#v", "k", a, b)
	}
	if a, b := s.Timer("k"), s.Timer("k"); a != b {
		t.Errorf("Timer(%q) returned different wrappers: %#v vs %#v", "k", a, b)
	}

	if n := testing.AllocsPerRun(100, func() { _ = s.Counter("k") }); n != 0 {
		t.Errorf("Counter cache hit allocated %v times, want 0", n)
	}
	if n := testing.AllocsPerRun(100, func() { _ = s.Timer("k") }); n != 0 {
		t.Errorf("Timer cache hit allocated %v times, want 0", n)
	}
}

// TestAdapterConcurrentLookup exercises the RWMutex-guarded
// counter/timer cache under `-race`. Many goroutines racing on the same
// and on different names must never deadlock or corrupt the map, and
// every goroutine must see the same wrapper for a given name.
func TestAdapterConcurrentLookup(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	defer provider.Shutdown(context.Background())
	s := New(provider.Meter("concurrent.test"))

	const goroutines = 32
	const perGoroutine = 128
	var seen sync.Map // name -> drv.Counter, first wrapper any goroutine got
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := range goroutines {
		go func(id int) {
			defer wg.Done()
			for i := range perGoroutine {
				name := fmt.Sprintf("k%d", (id+i)%4) // 4 shared names
				c := s.Counter(name)
				if prev, loaded := seen.LoadOrStore(name, c); loaded && prev != c {
					t.Errorf("Counter(%q) returned two different wrappers", name)
				}
				c.Inc(1)
				s.Timer(name).Record(time.Microsecond)
			}
		}(g)
	}
	wg.Wait()

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	// Just prove metrics were recorded end-to-end; exact per-key totals are
	// deterministic (goroutines * perGoroutine) but the assertion above
	// under -race is the point.
	var total int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if s, ok := m.Data.(metricdata.Sum[int64]); ok {
				for _, dp := range s.DataPoints {
					total += dp.Value
				}
			}
		}
	}
	if want := int64(goroutines * perGoroutine); total != want {
		t.Errorf("counter total = %d, want %d", total, want)
	}
}

// TestAdapterNilMeter pins the nil-input contract: New(nil) degrades to
// the driver's NoopScope instead of panicking on first use.
func TestAdapterNilMeter(t *testing.T) {
	s := New(nil)
	if s != drv.NoopScope {
		t.Fatalf("New(nil) = %T, want drv.NoopScope", s)
	}
	s.Counter("k").Inc(1) // must not panic
	s.Timer("t").Record(time.Millisecond)
}

// TestNewTracer_NilTracer pins the nil-input contract: NewTracer(nil)
// degrades to the driver's NoopTracer instead of panicking on first use.
func TestNewTracer_NilTracer(t *testing.T) {
	tr := NewTracer(nil)
	if tr != drv.NoopTracer {
		t.Fatalf("NewTracer(nil) = %T, want drv.NoopTracer", tr)
	}
	_, span := tr.StartSpan(context.Background(), "athena.query") // must not panic
	span.SetAttr("k", "v")
	span.End()
}

// TestTracer_SpanIsClientKindWithAttributes is the end-to-end proof that
// matters for ingestion: the span this adapter produces is CLIENT-kind
// (required by OpenTelemetry's database semantic conventions for a
// backend like Sentry/Datadog to render it as a DB call) and carries every
// attribute type the driver sets.
func TestTracer_SpanIsClientKindWithAttributes(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	defer provider.Shutdown(context.Background())

	tr := NewTracer(provider.Tracer("athenadriver.test"))
	_, span := tr.StartSpan(context.Background(), "athena.query")
	span.SetAttr("db.system.name", "aws.athena")
	span.SetAttr("athena.data_scanned_bytes", int64(1024))
	span.SetAttr("bool.attr", true)
	span.SetAttr("float.attr", 1.5)
	span.End()

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	got := spans[0]
	if got.Name != "athena.query" {
		t.Errorf("span name = %q, want %q", got.Name, "athena.query")
	}
	if got.SpanKind != trace.SpanKindClient {
		t.Errorf("span kind = %v, want %v (required for DB-span ingestion)", got.SpanKind, trace.SpanKindClient)
	}
	attrs := map[string]any{}
	for _, kv := range got.Attributes {
		attrs[string(kv.Key)] = kv.Value.AsInterface()
	}
	if attrs["db.system.name"] != "aws.athena" {
		t.Errorf("db.system.name = %v, want aws.athena", attrs["db.system.name"])
	}
	if attrs["athena.data_scanned_bytes"] != int64(1024) {
		t.Errorf("athena.data_scanned_bytes = %v, want 1024", attrs["athena.data_scanned_bytes"])
	}
	if attrs["bool.attr"] != true {
		t.Errorf("bool.attr = %v, want true", attrs["bool.attr"])
	}
	if attrs["float.attr"] != 1.5 {
		t.Errorf("float.attr = %v, want 1.5", attrs["float.attr"])
	}
}

// TestTracer_RecordErrorSetsSpanStatus proves a recorded error attaches
// an exception event and flips the span status to Error: a span left at
// its default Unset status reads as "succeeded" to most backends
// regardless of any error event on it.
func TestTracer_RecordErrorSetsSpanStatus(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	defer provider.Shutdown(context.Background())

	tr := NewTracer(provider.Tracer("athenadriver.test"))
	_, span := tr.StartSpan(context.Background(), "athena.query")
	span.RecordError(errors.New("workgroup is disabled"))
	span.End()

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	got := spans[0]
	if got.Status.Code != codes.Error {
		t.Errorf("span status = %v, want codes.Error", got.Status.Code)
	}
	if len(got.Events) != 1 || got.Events[0].Name != "exception" {
		t.Errorf("events = %+v, want one exception event", got.Events)
	}
}

// TestTracer_RecordErrorNilIsNoop pins the nil guard: a nil error must not
// attach an exception event or flip the span status to Error.
func TestTracer_RecordErrorNilIsNoop(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	defer provider.Shutdown(context.Background())

	tr := NewTracer(provider.Tracer("athenadriver.test"))
	_, span := tr.StartSpan(context.Background(), "athena.query")
	span.RecordError(nil)
	span.End()

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	got := spans[0]
	if got.Status.Code == codes.Error {
		t.Errorf("span status = %v, want not codes.Error", got.Status.Code)
	}
	if len(got.Events) != 0 {
		t.Errorf("events = %+v, want none", got.Events)
	}
}
