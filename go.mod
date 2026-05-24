module github.com/PratikDhanave/bodh

go 1.26

require github.com/google/uuid v1.6.0

// OTel SDK + OTLP/HTTP exporter — used ONLY by files behind the `otel`
// build tag (pkg/observability/otlp.go and pkg/observability/otlp_test.go).
// The default Bodh build does not link any of these; run
//   go build -tags otel ./cmd/care
// or `go mod tidy` after enabling the tag to populate go.sum.
require (
	go.opentelemetry.io/otel v1.34.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.34.0
	go.opentelemetry.io/otel/sdk v1.34.0
	go.opentelemetry.io/otel/trace v1.34.0
	go.opentelemetry.io/proto/otlp v1.5.0
	google.golang.org/protobuf v1.36.1
)
