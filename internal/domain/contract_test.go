package domain

import (
	"encoding/json"
	"os"
	"testing"
)

func TestMinimalExampleMatchesDomainContract(t *testing.T) {
	content, err := os.ReadFile("../../examples/scan-v1.minimal.json")
	if err != nil {
		t.Fatalf("read minimal example: %v", err)
	}

	var document ScanDocument
	if err := json.Unmarshal(content, &document); err != nil {
		t.Fatalf("decode minimal example: %v", err)
	}
	if err := document.Validate(); err != nil {
		t.Fatalf("validate minimal example: %v", err)
	}
}
