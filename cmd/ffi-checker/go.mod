// This is a nested module so checker-only dependencies never enter Bucky's
// root module and root-level `go test ./...` does not run network-facing audits.
module github.com/ardanlabs/bucky/cmd/ffi-checker

go 1.26.0

require golang.org/x/tools v0.48.0

require (
	golang.org/x/mod v0.38.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
)
