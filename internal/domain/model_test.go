package domain

import "testing"

func TestMinimalDocumentIsValid(t *testing.T) {
	document := ScanDocument{
		SchemaVersion: SchemaVersion,
		ToolVersion:   "0.1.0-mvp",
		Scan: ScanMetadata{
			StartedAt:   "2026-09-02T20:00:00Z",
			CompletedAt: "2026-09-02T20:00:01Z",
			Roots:       []string{"/Users/example/Projects"},
			Exclusions:  []string{},
			ReadOnly:    true,
		},
		Projects:           []Project{},
		InstalledResources: []InstalledResource{},
		Relations:          []Relation{},
		Diagnostics:        []Diagnostic{},
	}

	if err := document.Validate(); err != nil {
		t.Fatalf("expected valid document, got %v", err)
	}
}

func TestWritableScanIsRejected(t *testing.T) {
	document := ScanDocument{
		SchemaVersion: SchemaVersion,
		ToolVersion:   "0.1.0-mvp",
		Scan: ScanMetadata{
			StartedAt:   "2026-09-02T20:00:00Z",
			CompletedAt: "2026-09-02T20:00:01Z",
			Roots:       []string{"/Users/example/Projects"},
			ReadOnly:    false,
		},
	}

	if err := document.Validate(); err == nil {
		t.Fatal("expected writable Phase 0 scan to be rejected")
	}
}

func TestDockerRelationCannotReferenceInstalledHostResource(t *testing.T) {
	resourceID := "resource-host"
	document := ScanDocument{
		SchemaVersion: SchemaVersion,
		ToolVersion:   "0.1.0-mvp",
		Scan: ScanMetadata{
			StartedAt:   "2026-09-02T20:00:00Z",
			CompletedAt: "2026-09-02T20:00:01Z",
			Roots:       []string{"/Users/example/Projects"},
			ReadOnly:    true,
		},
		Relations: []Relation{
			{
				ProjectID:     "project",
				RequirementID: "requirement",
				Environment:   EnvironmentDocker,
				ResourceID:    &resourceID,
				MatchStatus:   Matched,
				Rationale:     "invalid fixture",
				Evidence:      []Evidence{},
				Warnings:      []string{},
			},
		},
	}

	if err := document.Validate(); err == nil {
		t.Fatal("expected Docker relation with HOST resource to be rejected")
	}
}

func TestDockerDaemonRelationMayReferenceDockerResource(t *testing.T) {
	resourceID := "resource-docker-image"
	document := ScanDocument{
		SchemaVersion: SchemaVersion,
		ToolVersion:   "0.2.0-dev",
		Scan: ScanMetadata{
			StartedAt:   "2026-09-05T20:00:00Z",
			CompletedAt: "2026-09-05T20:00:01Z",
			Roots:       []string{"/Users/example/Projects"},
			ReadOnly:    true,
		},
		Relations: []Relation{{
			ProjectID:     "project",
			RequirementID: "requirement",
			Environment:   EnvironmentDockerDaemon,
			ResourceID:    &resourceID,
			MatchStatus:   Matched,
			Rationale:     "declared image is present in the local daemon",
			Evidence:      []Evidence{},
			Warnings:      []string{},
		}},
	}
	if err := document.Validate(); err != nil {
		t.Fatalf("Docker daemon relation should accept a local Docker resource: %v", err)
	}
}
