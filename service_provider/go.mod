module service_provider

go 1.16

replace github.com/synerex/synerex_beta_example/proto_order => ../proto_order

require (
	github.com/golang/protobuf v1.5.2
	github.com/synerex/synerex_api v0.5.1
	github.com/synerex/synerex_beta_example/proto_order v0.0.0-20211205141607-6c5b768ee197
	github.com/synerex/synerex_sxutil v0.7.0
)
