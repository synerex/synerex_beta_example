module intg_provider

go 1.16

require (
	github.com/synerex/synerex_api v0.5.1
	github.com/synerex/synerex_beta_example/proto_order v0.0.0-20211205141607-6c5b768ee197
	github.com/synerex/synerex_sxutil v0.7.0
	go.opentelemetry.io/otel v1.3.0
	go.opentelemetry.io/otel/trace v1.3.0
	google.golang.org/protobuf v1.27.1
)

replace github.com/synerex/synerex_beta_example/proto_order => ../proto_order

replace github.com/synerex/synerex_sxutil => ../synerex_sxutil
