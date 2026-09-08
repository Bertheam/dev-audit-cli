package report

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	"dev-environment-auditor/internal/domain"
)

func TestRenderJSONMatchesGoldenAndSchema(t *testing.T) {
	content, err := RenderJSON(sampleDocument())
	if err != nil {
		t.Fatalf("render JSON: %v", err)
	}
	assertGolden(t, "testdata/scan.golden.json", content)
	if err := ValidateJSON(content); err != nil {
		t.Fatalf("validate rendered JSON: %v", err)
	}
	decoded, err := DecodeJSON(content)
	if err != nil {
		t.Fatalf("decode rendered JSON: %v", err)
	}
	if decoded.Projects[0].ID != "project-one" {
		t.Fatalf("unexpected decoded project: %#v", decoded.Projects)
	}
}

func TestRepositoryMinimalExampleMatchesEmbeddedSchema(t *testing.T) {
	content, err := os.ReadFile("../../examples/scan-v1.minimal.json")
	if err != nil {
		t.Fatalf("read minimal example: %v", err)
	}
	if err := ValidateJSON(content); err != nil {
		t.Fatalf("minimal example violates embedded schema: %v", err)
	}
}

func TestRenderTerminalMatchesGolden(t *testing.T) {
	content, err := RenderTerminal(sampleDocument())
	if err != nil {
		t.Fatalf("render terminal: %v", err)
	}
	assertGolden(t, "testdata/terminal.golden", content)
}

func TestValidateJSONRejectsSchemaViolations(t *testing.T) {
	valid, err := RenderJSON(sampleDocument())
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		content []byte
	}{
		{name: "additional property", content: bytes.Replace(valid, []byte(`"schema_version": "1.0"`), []byte(`"schema_version": "1.0", "extra": true`), 1)},
		{name: "bad timestamp", content: bytes.Replace(valid, []byte(`"2026-09-02T20:00:00Z"`), []byte(`"not-a-time"`), 1)},
		{name: "bad enum", content: bytes.Replace(valid, []byte(`"MATCHED"`), []byte(`"GUESSED"`), 1)},
		{name: "Docker relation with HOST resource", content: bytes.Replace(valid, []byte(`"environment": "DOCKER",`), []byte(`"environment": "DOCKER", "resource_id": "resource-one",`), 1)},
		{name: "trailing value", content: append(append([]byte(nil), valid...), []byte("{}")...)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateJSON(test.content); err == nil {
				t.Fatal("expected schema validation error")
			}
		})
	}
}

func TestLegacyRelationWithoutEnvironmentMeansHost(t *testing.T) {
	content, err := RenderJSON(sampleDocument())
	if err != nil {
		t.Fatal(err)
	}
	legacy := bytes.Replace(content, []byte("      \"environment\": \"HOST\",\n"), nil, 1)
	document, err := DecodeJSON(legacy)
	if err != nil {
		t.Fatalf("legacy v1 relation should remain readable: %v", err)
	}
	rendered, err := RenderTerminalWithOptions(document, TerminalOptions{Verbose: true})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(rendered, []byte("HOST:MATCHED")) {
		t.Fatalf("legacy relation was not interpreted as HOST:\n%s", rendered)
	}
}

func TestExplainProjectRequirementAndResource(t *testing.T) {
	document := sampleDocument()
	tests := []struct {
		id       string
		contains []string
	}{
		{id: "project-one", contains: []string{"PROJECT · project-one", "REQUIREMENTS  1"}},
		{id: "requirement-one", contains: []string{"REQUIREMENT · requirement-one", "HOST  MATCHED", "DOCKER  MATCHED", "test.rule"}},
		{id: "resource-one", contains: []string{"RESOURCE · resource-one", "Reference    ✓ REFERENCED", "MATCHED RELATIONS"}},
	}
	for _, test := range tests {
		t.Run(test.id, func(t *testing.T) {
			content, found, err := Explain(document, test.id)
			if err != nil || !found {
				t.Fatalf("explain = found %v, err %v", found, err)
			}
			for _, expected := range test.contains {
				if !strings.Contains(string(content), expected) {
					t.Errorf("explanation does not contain %q:\n%s", expected, content)
				}
			}
		})
	}
	if content, found, err := Explain(document, "missing"); err != nil || found || content != nil {
		t.Fatalf("missing explanation = %q, %v, %v", content, found, err)
	}
}

func TestRenderAndExplainPotentiallyReclaimableSpace(t *testing.T) {
	document := sampleDocument()
	document.InstalledResources[0].PotentiallyReclaimable = &domain.SpaceEstimate{
		Bytes:     1024,
		Rationale: "fixture conservative estimate",
		Evidence: []domain.Evidence{{
			SourceType: "test",
			RuleID:     "classification.space.fixture.v1",
		}},
	}
	if _, err := RenderJSON(document); err != nil {
		t.Fatalf("space estimate must satisfy the JSON schema: %v", err)
	}
	terminal, err := RenderTerminal(document)
	if err != nil || !bytes.Contains(terminal, []byte("reclaimable 1.0 KiB")) {
		t.Fatalf("terminal did not separate potential space: err=%v\n%s", err, terminal)
	}
	explanation, found, err := Explain(document, "resource-one")
	if err != nil || !found || !bytes.Contains(explanation, []byte("fixture conservative estimate")) ||
		!bytes.Contains(explanation, []byte("classification.space.fixture.v1")) {
		t.Fatalf("explain did not expose estimate rationale and evidence: found=%v err=%v\n%s", found, err, explanation)
	}
}

func TestLegacyResourceWithoutClassificationsRemainsReadable(t *testing.T) {
	document := sampleDocument()
	document.Scan.ClassificationPolicy = nil
	document.InstalledResources[0].Classifications = nil
	content, err := RenderJSON(document)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeJSON(content)
	if err != nil {
		t.Fatalf("legacy v1 resource should remain readable: %v", err)
	}
	if len(decoded.InstalledResources[0].Classifications) != 0 {
		t.Fatalf("legacy resource classifications changed: %#v", decoded.InstalledResources[0])
	}
}

func TestRenderAndExplainDockerDaemonRelation(t *testing.T) {
	document := sampleDocument()
	constraint := "postgres:16"
	resourceID := "resource-docker-image"
	document.Projects[0].Requirements = append(document.Projects[0].Requirements, domain.Requirement{
		ID:                "requirement-docker-image",
		Ecosystem:         "docker",
		Component:         "image",
		VersionConstraint: &constraint,
		Confidence:        domain.RequiredExplicitly,
		Evidence:          []domain.Evidence{},
		Warnings:          []string{},
	})
	document.InstalledResources = append(document.InstalledResources, domain.InstalledResource{
		ID:              resourceID,
		Ecosystem:       "docker",
		Component:       "docker_image",
		Version:         &constraint,
		Path:            "docker://image/sha256%3Afixture",
		ReferenceStatus: domain.Referenced,
		Metadata:        []domain.MetadataEntry{{Key: "reference", Value: constraint}},
		Warnings:        []string{},
	})
	document.Relations = append(document.Relations, domain.Relation{
		ProjectID:     "project-one",
		RequirementID: "requirement-docker-image",
		Environment:   domain.EnvironmentDockerDaemon,
		ResourceID:    &resourceID,
		MatchStatus:   domain.Matched,
		Rationale:     "the declared image is present in the local daemon",
		Evidence:      []domain.Evidence{},
		Warnings:      []string{},
	})

	terminal, err := RenderTerminalWithOptions(document, TerminalOptions{Verbose: true})
	if err != nil || !bytes.Contains(terminal, []byte("DOCKER_DAEMON:MATCHED")) ||
		!bytes.Contains(terminal, []byte(`docker://image/sha256%3Afixture`)) {
		t.Fatalf("terminal did not expose Docker daemon relation: err=%v\n%s", err, terminal)
	}
	explanation, found, err := Explain(document, "requirement-docker-image")
	if err != nil || !found || !bytes.Contains(explanation, []byte("DOCKER_DAEMON  MATCHED")) {
		t.Fatalf("explain did not expose Docker daemon relation: found=%v err=%v\n%s", found, err, explanation)
	}
}

func TestTerminalEscapesControlCharacters(t *testing.T) {
	document := sampleDocument()
	document.Projects[0].Path = "/work/\x1b[31mred"
	content, err := RenderTerminal(document)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(content, []byte{0x1b}) || !bytes.Contains(content, []byte(`\x1b`)) {
		t.Fatalf("terminal control character was not escaped: %q", content)
	}
}

func TestTerminalColorIsExplicitAndSemanticTextRemainsVisible(t *testing.T) {
	plain, err := RenderTerminalWithOptions(sampleDocument(), TerminalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	colored, err := RenderTerminalWithOptions(sampleDocument(), TerminalOptions{Color: true})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(plain, []byte{0x1b}) {
		t.Fatalf("plain terminal output contains ANSI escapes: %q", plain)
	}
	if !bytes.Contains(colored, []byte{0x1b}) || !bytes.Contains(colored, []byte("✓")) {
		t.Fatalf("colored output lacks ANSI styling or semantic symbols: %q", colored)
	}
}

func TestTerminalCompactViewLimitsProjectsAndVerboseRestoresDetails(t *testing.T) {
	document := sampleDocument()
	for index := 2; index <= 13; index++ {
		document.Projects = append(document.Projects, domain.Project{
			ID:           fmt.Sprintf("project-%02d", index),
			Path:         fmt.Sprintf("/work/project-%02d", index),
			Kind:         domain.ProjectDocker,
			Requirements: []domain.Requirement{},
			Warnings:     []string{},
		})
	}
	compact, err := RenderTerminal(document)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(compact, []byte("1 more projects hidden")) || bytes.Contains(compact, []byte("TEST_INFO")) {
		t.Fatalf("compact view did not hide secondary detail:\n%s", compact)
	}
	verbose, err := RenderTerminalWithOptions(document, TerminalOptions{Verbose: true})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(verbose, []byte("more projects hidden")) || !bytes.Contains(verbose, []byte("TEST_INFO")) {
		t.Fatalf("verbose view did not restore all detail:\n%s", verbose)
	}
}

func sampleDocument() domain.ScanDocument {
	constraint := "3.35.0"
	filePath := "/work/app/.fvmrc"
	key := "flutter"
	line := 2
	observed := "3.35.0"
	version := "3.35.0"
	size := int64(2048)
	resourceID := "resource-one"
	diagnosticPath := "/work/app"
	evidence := []domain.Evidence{
		{
			SourceType:    "file",
			FilePath:      &filePath,
			Key:           &key,
			LineHint:      &line,
			Command:       []string{},
			ObservedValue: &observed,
			RuleID:        "test.rule",
		},
	}
	return domain.ScanDocument{
		SchemaVersion: domain.SchemaVersion,
		ToolVersion:   "test-version",
		Scan: domain.ScanMetadata{
			StartedAt:   "2026-09-02T20:00:00Z",
			CompletedAt: "2026-09-02T20:00:01Z",
			Roots:       []string{"/work/z", "/work/a"},
			Exclusions:  []string{"build", ".git"},
			ReadOnly:    true,
			ClassificationPolicy: &domain.ClassificationPolicy{
				OldAfterDays: 180,
			},
		},
		Projects: []domain.Project{
			{
				ID:   "project-one",
				Path: "/work/app",
				Kind: domain.ProjectFlutter,
				Requirements: []domain.Requirement{
					{
						ID:                "requirement-one",
						Ecosystem:         "flutter",
						Component:         "flutter_sdk",
						VersionConstraint: &constraint,
						Confidence:        domain.RequiredExplicitly,
						Evidence:          evidence,
						Warnings:          []string{},
					},
				},
				Warnings: []string{},
			},
		},
		InstalledResources: []domain.InstalledResource{
			{
				ID:              resourceID,
				Ecosystem:       "flutter",
				Component:       "flutter_sdk",
				Version:         &version,
				Path:            "/sdk/flutter",
				SizeBytes:       &size,
				ReferenceStatus: domain.Referenced,
				Classifications: []domain.ResourceClassification{{
					Category:  domain.CategoryUsed,
					Rationale: "At least one analyzed project requirement is correlated with this resource.",
					Evidence: []domain.Evidence{{
						SourceType:    "correlation",
						Key:           stringPointer("reference_status"),
						ObservedValue: stringPointer("REFERENCED"),
						RuleID:        "classification.used.reference.v1",
					}},
				}},
				Metadata: []domain.MetadataEntry{
					{Key: "channel", Value: "stable"},
					{Key: "dart_sdk_version", Value: "3.9.0"},
				},
				Warnings: []string{},
			},
		},
		Relations: []domain.Relation{
			{
				ProjectID:     "project-one",
				RequirementID: "requirement-one",
				Environment:   domain.EnvironmentHost,
				ResourceID:    &resourceID,
				MatchStatus:   domain.Matched,
				Rationale:     "one exact installed SDK satisfies the requirement",
				Evidence:      evidence,
				Warnings:      []string{},
			},
			{
				ProjectID:     "project-one",
				RequirementID: "requirement-one",
				Environment:   domain.EnvironmentDocker,
				MatchStatus:   domain.Matched,
				Rationale:     "one Dockerfile declaration satisfies the requirement",
				Evidence:      evidence,
				Warnings:      []string{"declarative Docker match only"},
			},
		},
		Diagnostics: []domain.Diagnostic{
			{
				Code:     "TEST_INFO",
				Severity: domain.SeverityInfo,
				Scope:    "test",
				Message:  "fixture diagnostic",
				Path:     &diagnosticPath,
			},
		},
	}
}

func stringPointer(value string) *string {
	return &value
}

func assertGolden(t *testing.T, path string, actual []byte) {
	t.Helper()
	expected, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden file: %v", err)
	}
	if !bytes.Equal(actual, expected) {
		t.Fatalf("golden mismatch for %s\n--- actual ---\n%s\n--- expected ---\n%s", path, actual, expected)
	}
}
