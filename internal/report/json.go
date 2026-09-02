package report

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"dev-environment-auditor/internal/domain"
	contractschemas "dev-environment-auditor/schemas"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const schemaResourceName = "embedded://dev-environment-auditor/scan-v1.schema.json"

var (
	scanSchemaOnce sync.Once
	scanSchema     *jsonschema.Schema
	scanSchemaErr  error
)

// RenderJSON produces an indented JSON v1 document and validates the exact
// bytes against the embedded schema before returning them.
func RenderJSON(document domain.ScanDocument) ([]byte, error) {
	normalized, err := Normalize(document)
	if err != nil {
		return nil, fmt.Errorf("normalize scan document: %w", err)
	}
	if err := normalized.Validate(); err != nil {
		return nil, fmt.Errorf("validate scan document: %w", err)
	}
	content, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode scan document: %w", err)
	}
	content = append(content, '\n')
	if err := ValidateJSON(content); err != nil {
		return nil, fmt.Errorf("validate JSON v1: %w", err)
	}
	return content, nil
}

// DecodeJSON rejects unknown fields, trailing documents and any value that
// does not satisfy the embedded JSON v1 schema.
func DecodeJSON(content []byte) (domain.ScanDocument, error) {
	if err := ValidateJSON(content); err != nil {
		return domain.ScanDocument{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var document domain.ScanDocument
	if err := decoder.Decode(&document); err != nil {
		return domain.ScanDocument{}, fmt.Errorf("decode scan document: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return domain.ScanDocument{}, errors.New("decode scan document: trailing JSON value")
		}
		return domain.ScanDocument{}, fmt.Errorf("decode scan document trailer: %w", err)
	}
	if err := document.Validate(); err != nil {
		return domain.ScanDocument{}, fmt.Errorf("validate scan document: %w", err)
	}
	return Normalize(document)
}

// ValidateJSON evaluates raw JSON against the schema shipped in the binary.
func ValidateJSON(content []byte) error {
	schema, err := compiledScanSchema()
	if err != nil {
		return fmt.Errorf("compile embedded scan schema: %w", err)
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(content))
	if err != nil {
		return fmt.Errorf("decode JSON instance: %w", err)
	}
	if err := schema.Validate(instance); err != nil {
		return fmt.Errorf("scan-v1 schema: %w", err)
	}
	return nil
}

func compiledScanSchema() (*jsonschema.Schema, error) {
	scanSchemaOnce.Do(func() {
		document, err := jsonschema.UnmarshalJSON(bytes.NewReader(contractschemas.ScanV1))
		if err != nil {
			scanSchemaErr = err
			return
		}
		compiler := jsonschema.NewCompiler()
		compiler.AssertFormat()
		if err := compiler.AddResource(schemaResourceName, document); err != nil {
			scanSchemaErr = err
			return
		}
		scanSchema, scanSchemaErr = compiler.Compile(schemaResourceName)
	})
	return scanSchema, scanSchemaErr
}
