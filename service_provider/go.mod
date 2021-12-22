module service_provider

go 1.16

require (
	github.com/ChimeraCoder/gojson v1.1.0 // indirect
	github.com/golang/protobuf v1.5.2
	github.com/synerex/synerex_api v0.5.1
	github.com/synerex/synerex_beta_example/proto_order v0.0.0-20211205141607-6c5b768ee197
	github.com/synerex/synerex_sxutil v0.7.0
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.28.0 // indirect
	go.opentelemetry.io/otel/exporters/jaeger v1.3.0 // indirect
	google.golang.org/protobuf v1.27.1
)

replace github.com/synerex/synerex_beta_example/proto_order => ../proto_order

replace github.com/synerex/synerex_sxutil => ../synerex_sxutil
