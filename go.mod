module github.com/kirill-scherba/mail-mcp

go 1.26.4

replace github.com/kirill-scherba/opencode-client v0.1.1 => ../opencode-client

require (
	github.com/kirill-scherba/opencode-client v0.1.1
	golang.org/x/oauth2 v0.36.0
)

require cloud.google.com/go/compute/metadata v0.3.0 // indirect
