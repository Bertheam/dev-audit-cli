package application_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"dev-environment-auditor/internal/analyzers"
	"dev-environment-auditor/internal/application"
	"dev-environment-auditor/internal/discovery"
	"dev-environment-auditor/internal/domain"
	"dev-environment-auditor/internal/environments"
	"dev-environment-auditor/internal/inventory"
	"dev-environment-auditor/internal/report"
)

type sequenceClock struct {
	values []time.Time
	index  int
}

func (clock *sequenceClock) Now() time.Time {
	if clock.index >= len(clock.values) {
		return clock.values[len(clock.values)-1]
	}
	value := clock.values[clock.index]
	clock.index++
	return value
}

func TestScannerRunsReadOnlyPipelineAndProducesSchemaValidDocument(t *testing.T) {
	flutterRoot := createFlutterSDK(t)
	versionPath := filepath.Join(flutterRoot, "bin", "cache", "flutter.version.json")
	beforeContent, beforeInfo := snapshot(t, versionPath)
	clock := &sequenceClock{values: []time.Time{
		time.Date(2026, 9, 2, 20, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 2, 20, 0, 1, 0, time.UTC),
	}}
	scanner := application.NewScannerWithAdapters(
		discovery.New(discovery.OSFileSystem{}),
		analyzers.New(analyzers.OSFileSystem{}),
		environments.New(environments.OSFileSystem{}),
		inventory.New(inventory.OSFileSystem{}),
		clock,
		"test-version",
	)

	document := scanner.Scan(context.Background(), application.Config{
		ProjectRoots:    []string{filepath.Join("..", "..", "testdata")},
		FlutterSDKRoots: []string{flutterRoot},
	})

	if err := document.Validate(); err != nil {
		t.Fatalf("document validation failed: %v", err)
	}
	encoded, err := report.RenderJSON(document)
	if err != nil {
		t.Fatalf("schema-valid JSON expected: %v", err)
	}
	if err := report.ValidateJSON(encoded); err != nil {
		t.Fatalf("JSON schema validation failed: %v", err)
	}
	if document.Scan.StartedAt != "2026-09-02T20:00:00Z" || document.Scan.CompletedAt != "2026-09-02T20:00:01Z" {
		t.Fatalf("unexpected injected timestamps: %#v", document.Scan)
	}
	if len(document.Projects) != 2 {
		t.Fatalf("projects = %d, want 2", len(document.Projects))
	}
	if len(document.InstalledResources) != 1 {
		t.Fatalf("resources = %d, want 1", len(document.InstalledResources))
	}
	if document.InstalledResources[0].ReferenceStatus != domain.Referenced {
		t.Fatalf("Flutter resource = %s, want REFERENCED", document.InstalledResources[0].ReferenceStatus)
	}
	matched := 0
	for _, relation := range document.Relations {
		if relation.MatchStatus == domain.Matched {
			matched++
		}
	}
	if matched != 2 {
		t.Fatalf("matched relations = %d, want Flutter and Dart matches", matched)
	}
	if hasDiagnostic(document.Diagnostics, "INVENTORY_NOT_CONFIGURED") {
		t.Fatal("configured Flutter inventory was reported as absent")
	}
	afterContent, afterInfo := snapshot(t, versionPath)
	if string(afterContent) != string(beforeContent) || !afterInfo.ModTime().Equal(beforeInfo.ModTime()) {
		t.Fatal("scan modified Flutter inventory metadata")
	}
}

func TestScannerPreservesInvalidExplicitRootForPartialReport(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	clock := &sequenceClock{values: []time.Time{time.Unix(1, 0), time.Unix(2, 0)}}
	scanner := application.NewScanner(clock, "test-version")
	document := scanner.Scan(context.Background(), application.Config{ProjectRoots: []string{missing}})

	if len(document.Scan.Roots) != 1 || document.Scan.Roots[0] != missing {
		t.Fatalf("explicit failed root was not retained: %#v", document.Scan.Roots)
	}
	if !hasSeverity(document.Diagnostics, domain.SeverityError) {
		t.Fatalf("expected partial error diagnostic: %#v", document.Diagnostics)
	}
	if _, err := report.RenderJSON(document); err != nil {
		t.Fatalf("partial report should still satisfy schema: %v", err)
	}
}

func TestHeuristicInventoryNeverProducesMissingConclusion(t *testing.T) {
	flutterRoot := createFlutterSDK(t)
	versionPath := filepath.Join(flutterRoot, "bin", "cache", "flutter.version.json")
	if err := os.WriteFile(
		versionPath,
		[]byte(`{"frameworkVersion":"9.9.9","channel":"stable","dartSdkVersion":"9.9.9"}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	scanner := application.NewScanner(application.SystemClock{}, "test-version")
	document := scanner.Scan(context.Background(), application.Config{
		ProjectRoots:               []string{filepath.Join("..", "..", "testdata", "flutter_explicit")},
		FlutterSDKRoots:            []string{flutterRoot},
		HeuristicInventoryFamilies: []string{"flutter"},
	})

	for _, relation := range document.Relations {
		if relation.MatchStatus == domain.Missing {
			t.Fatalf("heuristic inventory produced MISSING: %#v", relation)
		}
	}
}

func TestHeuristicProjectDiscoveryNeverProducesNoReferenceFound(t *testing.T) {
	projectRoot := t.TempDir()
	jdkRoot := filepath.Join(t.TempDir(), "jdk")
	if err := os.MkdirAll(filepath.Join(jdkRoot, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{
		filepath.Join(jdkRoot, "release"):      `JAVA_VERSION="21"`,
		filepath.Join(jdkRoot, "bin", "java"):  "fixture",
		filepath.Join(jdkRoot, "bin", "javac"): "fixture",
	} {
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	path := projectRoot
	scanner := application.NewScanner(application.SystemClock{}, "test-version")
	document := scanner.Scan(context.Background(), application.Config{
		ProjectRoots:              []string{projectRoot},
		JDKRoots:                  []string{jdkRoot},
		ProjectDiscoveryHeuristic: true,
		PreScanDiagnostics: []domain.Diagnostic{{
			Code:     "AUTODETECT_ROOT_FOUND",
			Severity: domain.SeverityInfo,
			Scope:    "autodetect/projects",
			Message:  "fixture",
			Path:     &path,
		}},
	})

	if len(document.InstalledResources) != 1 {
		t.Fatalf("resources = %d, want 1", len(document.InstalledResources))
	}
	if document.InstalledResources[0].ReferenceStatus != domain.ReferenceUnknown {
		t.Fatalf("heuristic project coverage produced %s", document.InstalledResources[0].ReferenceStatus)
	}
	if !hasDiagnostic(document.Diagnostics, "AUTODETECT_ROOT_FOUND") {
		t.Fatalf("pre-scan diagnostic was not preserved: %#v", document.Diagnostics)
	}
}

func TestScannerSeparatesHostAndDockerJDKRelations(t *testing.T) {
	projectRoot := t.TempDir()
	for path, content := range map[string]string{
		filepath.Join(projectRoot, "settings.gradle.kts"):                            "rootProject.name = \"api\"\n",
		filepath.Join(projectRoot, "gradle", "wrapper", "gradle-wrapper.properties"): "distributionUrl=fixture\n",
		filepath.Join(projectRoot, "build.gradle.kts"): `java {
    toolchain {
        languageVersion = JavaLanguageVersion.of(21)
    }
}
`,
		filepath.Join(projectRoot, "Dockerfile"): "FROM eclipse-temurin:21-jdk-alpine AS builder\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	document := application.NewScanner(application.SystemClock{}, "test-version").Scan(
		context.Background(),
		application.Config{ProjectRoots: []string{projectRoot}},
	)
	var hostStatus, dockerStatus domain.MatchStatus
	for _, relation := range document.Relations {
		if relation.Environment == domain.EnvironmentHost {
			hostStatus = relation.MatchStatus
		}
		if relation.Environment == domain.EnvironmentDocker {
			dockerStatus = relation.MatchStatus
			if relation.ResourceID != nil {
				t.Fatalf("Docker relation references a HOST resource: %#v", relation)
			}
		}
	}
	if hostStatus != domain.MatchUnknown || dockerStatus != domain.Matched {
		t.Fatalf("expected HOST UNKNOWN and DOCKER MATCHED, got HOST %s DOCKER %s: %#v",
			hostStatus, dockerStatus, document.Relations)
	}
	if _, err := report.RenderJSON(document); err != nil {
		t.Fatalf("environment-aware report must satisfy schema: %v", err)
	}
}

func createFlutterSDK(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "flutter")
	paths := []string{
		filepath.Join(root, "bin", "cache"),
		filepath.Join(root, "packages", "flutter"),
	}
	for _, path := range paths {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("create fixture directory: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "bin", "flutter"), []byte("fixture\n"), 0o755); err != nil {
		t.Fatalf("create Flutter marker: %v", err)
	}
	metadata := []byte(`{"frameworkVersion":"3.35.0","channel":"stable","dartSdkVersion":"3.9.0"}`)
	if err := os.WriteFile(filepath.Join(root, "bin", "cache", "flutter.version.json"), metadata, 0o644); err != nil {
		t.Fatalf("create Flutter metadata: %v", err)
	}
	return root
}

func snapshot(t *testing.T, path string) ([]byte, os.FileInfo) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat fixture: %v", err)
	}
	return content, info
}

func hasDiagnostic(diagnostics []domain.Diagnostic, code string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}

func hasSeverity(diagnostics []domain.Diagnostic, severity domain.DiagnosticSeverity) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == severity {
			return true
		}
	}
	return false
}
