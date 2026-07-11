// Package docs embeds static documentation assets (OpenAPI spec) so they can
// be served without depending on files on disk at runtime — important in the
// minimal Docker image where only the binary is shipped.
package docs

import _ "embed"

// OpenAPIYAML holds the embedded OpenAPI specification. The blank import of
// `embed` plus the //go:embed directive above the var are how the standard
// library exposes embedded file contents as a byte slice.
//
//go:embed openapi.yaml
var OpenAPIYAML []byte
