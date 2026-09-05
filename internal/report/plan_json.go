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

const cleanupPlanSchemaResourceName = "embedded://dev-environment-auditor/cleanup-plan-v1.schema.json"

var (
	cleanupPlanSchemaOnce sync.Once
	cleanupPlanSchema     *jsonschema.Schema
	cleanupPlanSchemaErr  error
)

// RenderPlanJSON emits the exact immutable plan without reordering it.
func RenderPlanJSON(plan domain.CleanupPlan) ([]byte, error) {
	if err := plan.Validate(); err != nil {
		return nil, fmt.Errorf("validate cleanup plan: %w", err)
	}
	content, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode cleanup plan: %w", err)
	}
	content = append(content, '\n')
	if err := ValidatePlanJSON(content); err != nil {
		return nil, fmt.Errorf("validate cleanup plan JSON v1: %w", err)
	}
	return content, nil
}

// DecodePlanJSON rejects unknown fields, trailing values, schema violations
// and any content whose content-addressed plan ID no longer matches.
func DecodePlanJSON(content []byte) (domain.CleanupPlan, error) {
	if err := ValidatePlanJSON(content); err != nil {
		return domain.CleanupPlan{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var plan domain.CleanupPlan
	if err := decoder.Decode(&plan); err != nil {
		return domain.CleanupPlan{}, fmt.Errorf("decode cleanup plan: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return domain.CleanupPlan{}, errors.New("decode cleanup plan: trailing JSON value")
		}
		return domain.CleanupPlan{}, fmt.Errorf("decode cleanup plan trailer: %w", err)
	}
	if err := plan.Validate(); err != nil {
		return domain.CleanupPlan{}, fmt.Errorf("validate cleanup plan: %w", err)
	}
	return plan, nil
}

func ValidatePlanJSON(content []byte) error {
	schema, err := compiledCleanupPlanSchema()
	if err != nil {
		return fmt.Errorf("compile embedded cleanup plan schema: %w", err)
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(content))
	if err != nil {
		return fmt.Errorf("decode JSON instance: %w", err)
	}
	if err := schema.Validate(instance); err != nil {
		return fmt.Errorf("cleanup-plan-v1 schema: %w", err)
	}
	return nil
}

func compiledCleanupPlanSchema() (*jsonschema.Schema, error) {
	cleanupPlanSchemaOnce.Do(func() {
		document, err := jsonschema.UnmarshalJSON(bytes.NewReader(contractschemas.CleanupPlanV1))
		if err != nil {
			cleanupPlanSchemaErr = err
			return
		}
		compiler := jsonschema.NewCompiler()
		compiler.AssertFormat()
		if err := compiler.AddResource(cleanupPlanSchemaResourceName, document); err != nil {
			cleanupPlanSchemaErr = err
			return
		}
		cleanupPlanSchema, cleanupPlanSchemaErr = compiler.Compile(cleanupPlanSchemaResourceName)
	})
	return cleanupPlanSchema, cleanupPlanSchemaErr
}
