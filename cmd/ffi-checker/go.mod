// This is a nested module so checker-only dependencies never enter Bucky's
// root module and root-level `go test ./...` does not run network-facing audits.
module github.com/ardanlabs/bucky/cmd/ffi-checker

go 1.26.0
