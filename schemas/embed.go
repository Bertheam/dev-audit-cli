// Package schemas embeds the versioned report contracts in the CLI binary.
package schemas

import _ "embed"

// ScanV1 is the JSON Schema 2020-12 contract for scan documents.
//
//go:embed scan-v1.schema.json
var ScanV1 []byte
