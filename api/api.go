// Package api holds the OpenAPI contract, embedded so the service can serve it.
package api

import _ "embed"

//go:embed openapi.yaml
var OpenAPI []byte
