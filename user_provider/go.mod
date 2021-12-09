module user_provider

go 1.16

require (
	github.com/gin-gonic/gin v1.7.7
	github.com/golang/protobuf v1.5.2
	github.com/synerex/synerex_api v0.5.1
	github.com/synerex/synerex_beta_example/proto_order v0.0.0-00010101000000-000000000000
	github.com/synerex/synerex_sxutil v0.7.0
)

replace github.com/synerex/synerex_beta_example/proto_order => ../proto_order
