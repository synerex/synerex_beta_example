module user_provider

go 1.16

require (
	github.com/gin-gonic/gin v1.7.7
	github.com/golang/protobuf v1.5.2
	github.com/gorilla/websocket v1.4.2
	github.com/synerex/synerex_api v0.5.1
	github.com/synerex/synerex_beta_example/proto_order v0.0.0-00010101000000-000000000000
	github.com/synerex/synerex_sxutil v0.7.0
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.28.0 // indirect
	go.opentelemetry.io/otel v1.3.0
	go.opentelemetry.io/otel/exporters/jaeger v1.3.0 // indirect
	go.opentelemetry.io/otel/trace v1.3.0
)

replace github.com/synerex/synerex_beta_example/proto_order => ../proto_order

replace github.com/synerex/synerex_sxutil => ../synerex_sxutil
