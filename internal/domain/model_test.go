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

func TestClassificationContractRejectsUnsafeOrUnexplainedStates(t *testing.T) {
	evidence := []Evidence{{SourceType: "test", RuleID: "test.rule"}}
	tests := []struct {
		name           string
		resource       InstalledResource
		oldAfterDays   int
		wantValidation bool
	}{
		{
			name: "valid probable orphan",
			resource: InstalledResource{
				ID: "resource", Ecosystem: "docker", Component: "docker_build_cache", Path: "docker://cache/id",
				ReferenceStatus: NoReferenceFound, Metadata: []MetadataEntry{}, Warnings: []string{},
				Classifications: []ResourceClassification{
					{Category: CategoryReconstructible, Rationale: "fixture", Evidence: evidence},
					{Category: CategoryOld, Rationale: "fixture", Evidence: evidence},
					{Category: CategoryProbableOrphan, Rationale: "fixture", Evidence: evidence},
				},
			},
			oldAfterDays:   180,
			wantValidation: true,
		},
		{
			name: "used and unknown",
			resource: InstalledResource{
				ID: "resource", Ecosystem: "java", Component: "jdk", Path: "/jdk",
				ReferenceStatus: Referenced, Metadata: []MetadataEntry{}, Warnings: []string{},
				Classifications: []ResourceClassification{
					{Category: CategoryUsed, Rationale: "fixture", Evidence: evidence},
					{Category: CategoryUnknown, Rationale: "fixture", Evidence: evidence},
				},
			},
			oldAfterDays: 180,
		},
		{
			name: "orphan without age",
			resource: InstalledResource{
				ID: "resource", Ecosystem: "java", Component: "jdk", Path: "/jdk",
				ReferenceStatus: NoReferenceFound, Metadata: []MetadataEntry{}, Warnings: []string{},
				Classifications: []ResourceClassification{
					{Category: CategoryReconstructible, Rationale: "fixture", Evidence: evidence},
					{Category: CategoryProbableOrphan, Rationale: "fixture", Evidence: evidence},
				},
			},
			oldAfterDays: 180,
		},
		{
			name: "classification without evidence",
			resource: InstalledResource{
				ID: "resource", Ecosystem: "java", Component: "jdk", Path: "/jdk",
				ReferenceStatus: ReferenceUnknown, Metadata: []MetadataEntry{}, Warnings: []string{},
				Classifications: []ResourceClassification{{Category: CategoryUnknown, Rationale: "fixture"}},
			},
			oldAfterDays: 180,
		},
		{
			name: "invalid policy",
			resource: InstalledResource{
				ID: "resource", Ecosystem: "java", Component: "jdk", Path: "/jdk",
				ReferenceStatus: ReferenceUnknown, Metadata: []MetadataEntry{}, Warnings: []string{},
			},
			oldAfterDays: 0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document := ScanDocument{
				SchemaVersion: SchemaVersion,
				ToolVersion:   "test",
				Scan: ScanMetadata{
					StartedAt: "2026-09-05T12:00:00Z", CompletedAt: "2026-09-05T12:00:01Z",
					Roots: []string{"/work"}, Exclusions: []string{}, ReadOnly: true,
					ClassificationPolicy: &ClassificationPolicy{OldAfterDays: test.oldAfterDays},
				},
				InstalledResources: []InstalledResource{test.resource},
			}
			err := document.Validate()
			if test.wantValidation && err != nil {
				t.Fatalf("expected valid classification contract: %v", err)
			}
			if !test.wantValidation && err == nil {
				t.Fatal("expected classification contract validation error")
			}
		})
	}
}
