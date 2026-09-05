package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dev-environment-auditor/internal/application"
	"dev-environment-auditor/internal/autodetect"
	"dev-environment-auditor/internal/domain"
	"dev-environment-auditor/internal/report"
)

func TestVersion(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if exitCode := run([]string{"version"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
	if strings.TrimSpace(stdout.String()) != version {
		t.Fatalf("expected version %q, got %q", version, stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
}

func TestUnsupportedCommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if exitCode := run([]string{"unknown"}, &stdout, &stderr); exitCode != exitUsage {
		t.Fatalf("expected exit code 2, got %d", exitCode)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected empty stdout, got %q", stdout.String())
	}
}

func TestScanJSONPassesExplicitConfiguration(t *testing.T) {
	var captured application.Config
	deadlineObserved := false
	dependencies := commandDependencies{
		scan: func(ctx context.Context, config application.Config) domain.ScanDocument {
			captured = config
			_, deadlineObserved = ctx.Deadline()
			return validDocument()
		},
		writeFile: func(string, []byte) error { return errors.New("should not write a file") },
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	arguments := []string{
		"scan",
		"--root", "/projects/one",
		"--root", "/projects/two",
		"--exclude", "build",
		"--android-sdk-root", "/sdk/android",
		"--android-avd-root", "/sdk/avd",
		"--flutter-sdk-root", "/sdk/flutter",
		"--fvm-cache-root", "/cache/fvm",
		"--gradle-user-home", "/cache/gradle",
		"--jdk-root", "/jdks",
		"--format", "json",
		"--timeout", "2s",
		"--old-after-days", "90",
	}

	if exitCode := runWithDependencies(arguments, &stdout, &stderr, dependencies); exitCode != exitSuccess {
		t.Fatalf("scan exit = %d, stderr=%q", exitCode, stderr.String())
	}
	if !deadlineObserved {
		t.Fatal("scan context has no deadline")
	}
	if len(captured.ProjectRoots) != 2 || captured.ProjectRoots[1] != "/projects/two" {
		t.Fatalf("project roots not forwarded: %#v", captured)
	}
	if len(captured.AndroidSDKRoots) != 1 || len(captured.AndroidAVDRoots) != 1 || len(captured.FlutterSDKRoots) != 1 ||
		len(captured.FVMCacheRoots) != 1 || len(captured.GradleUserHomeRoots) != 1 || len(captured.JDKRoots) != 1 {
		t.Fatalf("typed inventory roots not forwarded: %#v", captured)
	}
	if !captured.DockerInventory {
		t.Fatal("Docker inventory should be enabled by default")
	}
	if captured.OldAfterDays != 90 {
		t.Fatalf("age policy not forwarded: %#v", captured)
	}
	if _, err := report.DecodeJSON(stdout.Bytes()); err != nil {
		t.Fatalf("stdout is not schema-valid JSON: %v\n%s", err, stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}

func TestScanAutoDetectsEveryOmittedRootFamily(t *testing.T) {
	var captured application.Config
	detectionCalled := false
	dependencies := commandDependencies{
		detect: func(_ context.Context, config autodetect.Config) autodetect.Result {
			detectionCalled = true
			if !config.NeedProjectRoots || !config.NeedAndroid || !config.NeedAndroidAVD || !config.NeedFlutter ||
				!config.NeedFVM || !config.NeedGradle || !config.NeedJDK || !config.DeepSearch {
				t.Fatalf("unexpected detection config: %#v", config)
			}
			path := "/detected/projects"
			return autodetect.Result{
				Roots: autodetect.Roots{
					ProjectRoots:        []string{path},
					AndroidSDKRoots:     []string{"/detected/android"},
					AndroidAVDRoots:     []string{"/detected/avd"},
					FlutterSDKRoots:     []string{"/detected/flutter"},
					FVMCacheRoots:       []string{"/detected/fvm"},
					GradleUserHomeRoots: []string{"/detected/gradle"},
					JDKRoots:            []string{"/detected/jdk"},
				},
				Diagnostics: []domain.Diagnostic{{
					Code:     "AUTODETECT_ROOT_FOUND",
					Severity: domain.SeverityInfo,
					Scope:    "autodetect/projects",
					Message:  "fixture",
					Path:     &path,
				}},
				HeuristicInventoryFamilies: []autodetect.Family{
					autodetect.FamilyAndroid,
					autodetect.FamilyAndroidAVD,
					autodetect.FamilyFlutter,
					autodetect.FamilyGradle,
					autodetect.FamilyJava,
				},
				ProjectDiscoveryHeuristic: true,
			}
		},
		scan: func(_ context.Context, config application.Config) domain.ScanDocument {
			captured = config
			return validDocument()
		},
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exitCode := runWithDependencies(
		[]string{"scan", "--format", "json"},
		&stdout,
		&stderr,
		dependencies,
	); exitCode != exitSuccess {
		t.Fatalf("scan exit = %d, stderr=%q", exitCode, stderr.String())
	}
	if !detectionCalled {
		t.Fatal("automatic detector was not called")
	}
	if len(captured.ProjectRoots) != 1 || captured.ProjectRoots[0] != "/detected/projects" ||
		len(captured.AndroidSDKRoots) != 1 || len(captured.AndroidAVDRoots) != 1 || len(captured.FlutterSDKRoots) != 1 ||
		len(captured.FVMCacheRoots) != 1 || len(captured.GradleUserHomeRoots) != 1 ||
		len(captured.JDKRoots) != 1 {
		t.Fatalf("detected roots not forwarded: %#v", captured)
	}
	if !captured.ProjectDiscoveryHeuristic || len(captured.HeuristicInventoryFamilies) != 5 {
		t.Fatalf("heuristic coverage not forwarded: %#v", captured)
	}
	if len(captured.PreScanDiagnostics) != 1 || captured.PreScanDiagnostics[0].Code != "AUTODETECT_ROOT_FOUND" {
		t.Fatalf("detection diagnostics not forwarded: %#v", captured.PreScanDiagnostics)
	}
}

func TestScanExplicitOnlyModeStillRequiresProjectRoot(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exitCode := runWithDependencies(
		[]string{"scan", "--auto-detect=false"},
		&stdout,
		&stderr,
		commandDependencies{},
	); exitCode != exitUsage {
		t.Fatalf("scan exit = %d, want %d", exitCode, exitUsage)
	}
	if !strings.Contains(stderr.String(), "requires at least one --root") {
		t.Fatalf("unexpected error: %q", stderr.String())
	}
}

func TestScanExitCodes(t *testing.T) {
	tests := []struct {
		name      string
		arguments []string
		document  domain.ScanDocument
		writerErr error
		want      int
	}{
		{name: "missing root", arguments: []string{"scan"}, document: validDocument(), want: exitUsage},
		{name: "bad format", arguments: []string{"scan", "--root", "/work", "--format", "yaml"}, document: validDocument(), want: exitUsage},
		{name: "bad timeout", arguments: []string{"scan", "--root", "/work", "--timeout", "0s"}, document: validDocument(), want: exitUsage},
		{name: "bad age policy", arguments: []string{"scan", "--root", "/work", "--old-after-days", "0"}, document: validDocument(), want: exitUsage},
		{name: "partial diagnostics", arguments: []string{"scan", "--root", "/work"}, document: documentWithError(), want: exitPartial},
		{name: "invalid document", arguments: []string{"scan", "--root", "/work", "--format", "json"}, document: domain.ScanDocument{}, want: exitOperationalError},
		{name: "output failure", arguments: []string{"scan", "--root", "/work", "--output", "/report"}, document: validDocument(), writerErr: errors.New("disk full"), want: exitOperationalError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dependencies := commandDependencies{
				scan:      func(context.Context, application.Config) domain.ScanDocument { return test.document },
				writeFile: func(string, []byte) error { return test.writerErr },
			}
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			if got := runWithDependencies(test.arguments, &stdout, &stderr, dependencies); got != test.want {
				t.Fatalf("exit = %d, want %d, stdout=%q stderr=%q", got, test.want, stdout.String(), stderr.String())
			}
		})
	}
}

func TestExplainReadsValidatedReport(t *testing.T) {
	content, err := report.RenderJSON(validDocument())
	if err != nil {
		t.Fatal(err)
	}
	dependencies := commandDependencies{
		readFile: func(path string) ([]byte, error) {
			if path != "report.json" {
				t.Fatalf("unexpected report path %q", path)
			}
			return content, nil
		},
		writeFile: func(string, []byte) error { return errors.New("should not write") },
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exitCode := runWithDependencies(
		[]string{"explain", "--report", "report.json", "resource-one"},
		&stdout,
		&stderr,
		dependencies,
	); exitCode != exitSuccess {
		t.Fatalf("explain exit = %d, stderr=%q", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `Installed resource "resource-one"`) {
		t.Fatalf("unexpected explanation: %s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := runWithDependencies(
		[]string{"explain", "--report", "report.json", "missing"},
		&stdout,
		&stderr,
		dependencies,
	); exitCode != exitPartial {
		t.Fatalf("missing item exit = %d, want %d", exitCode, exitPartial)
	}
}

func TestExplainRejectsInvalidInputAndSelfOverwrite(t *testing.T) {
	invalidDependencies := commandDependencies{
		readFile: func(string) ([]byte, error) { return []byte(`{"invalid":true}`), nil },
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exitCode := runWithDependencies(
		[]string{"explain", "--report", "report.json", "id"},
		&stdout,
		&stderr,
		invalidDependencies,
	); exitCode != exitOperationalError {
		t.Fatalf("invalid report exit = %d, want %d", exitCode, exitOperationalError)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := runWithDependencies(
		[]string{"explain", "--report", "report.json", "--output", "./report.json", "id"},
		&stdout,
		&stderr,
		invalidDependencies,
	); exitCode != exitUsage {
		t.Fatalf("self-overwrite exit = %d, want %d", exitCode, exitUsage)
	}
}

func TestPlanReadsValidatedReportAndEmitsSchemaValidJSON(t *testing.T) {
	document := validDocument()
	document.InstalledResources[0] = manualDockerCacheResource()
	content, err := report.RenderJSON(document)
	if err != nil {
		t.Fatal(err)
	}
	dependencies := commandDependencies{
		readFile: func(path string) ([]byte, error) {
			if path != "report.json" {
				t.Fatalf("unexpected report path %q", path)
			}
			return content, nil
		},
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runWithDependencies(
		[]string{"plan", "--report", "report.json", "--resource", "resource-cache", "--format", "json"},
		&stdout,
		&stderr,
		dependencies,
	)
	if exitCode != exitSuccess {
		t.Fatalf("plan exit = %d, stderr=%q", exitCode, stderr.String())
	}
	plan, err := report.DecodePlanJSON(stdout.Bytes())
	if err != nil {
		t.Fatalf("stdout is not a valid immutable plan: %v\n%s", err, stdout.String())
	}
	if plan.Selection.Mode != domain.SelectionExplicitResourceIDs || len(plan.Items) != 1 ||
		plan.Items[0].OfficialCommand[0] != "docker" {
		t.Fatalf("unexpected plan: %#v", plan)
	}
}

func TestPlanReportsPartialManualSelectionAndRefusesSelfOverwrite(t *testing.T) {
	content, err := report.RenderJSON(validDocument())
	if err != nil {
		t.Fatal(err)
	}
	dependencies := commandDependencies{readFile: func(string) ([]byte, error) { return content, nil }}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exitCode := runWithDependencies(
		[]string{"plan", "--report", "report.json", "--resource", "missing", "--format", "json"},
		&stdout, &stderr, dependencies,
	); exitCode != exitPartial {
		t.Fatalf("missing manual resource exit = %d, want %d, stderr=%q", exitCode, exitPartial, stderr.String())
	}
	plan, err := report.DecodePlanJSON(stdout.Bytes())
	if err != nil || len(plan.Excluded) != 1 || plan.Excluded[0].Reasons[0] != domain.ExclusionResourceNotFound {
		t.Fatalf("unexpected missing-resource plan: %#v, err=%v", plan, err)
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := runWithDependencies(
		[]string{"plan", "--report", "report.json", "--output", "./report.json"},
		&stdout, &stderr, dependencies,
	); exitCode != exitUsage {
		t.Fatalf("self-overwrite exit = %d, want %d", exitCode, exitUsage)
	}
}

func TestPlanUsesNewFileWriter(t *testing.T) {
	content, err := report.RenderJSON(validDocument())
	if err != nil {
		t.Fatal(err)
	}
	writtenPath := ""
	dependencies := commandDependencies{
		readFile: func(string) ([]byte, error) { return content, nil },
		writeFile: func(string, []byte) error {
			return errors.New("overwrite-capable writer must not be used")
		},
		writeNewFile: func(path string, payload []byte) error {
			writtenPath = path
			if !strings.Contains(string(payload), "SIMULATION_ONLY") {
				t.Fatalf("unexpected plan payload: %s", payload)
			}
			return nil
		},
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exitCode := runWithDependencies(
		[]string{"plan", "--report", "report.json", "--output", "plan.txt"},
		&stdout, &stderr, dependencies,
	); exitCode != exitSuccess {
		t.Fatalf("plan exit = %d, stderr=%q", exitCode, stderr.String())
	}
	if writtenPath != "plan.txt" || stdout.Len() != 0 {
		t.Fatalf("unexpected output destination: path=%q stdout=%q", writtenPath, stdout.String())
	}
}

func TestWritePrivateFileRejectsSymlinkAndUsesPrivateMode(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "report.json")
	if err := writePrivateFile(target, []byte("report")); err != nil {
		t.Fatalf("write report: %v", err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 600", info.Mode().Perm())
	}

	symlink := filepath.Join(directory, "link.json")
	if err := os.Symlink(target, symlink); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	if err := writePrivateFile(symlink, []byte("changed")); err == nil {
		t.Fatal("expected output symlink rejection")
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "report" {
		t.Fatalf("symlink target changed: %q", content)
	}
	if !sameCleanPath(symlink, target) {
		t.Fatal("symlink alias should be recognized as the same input report")
	}
}

func TestWriteNewPrivateFileNeverOverwritesExistingPlan(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "plan.json")
	if err := writeNewPrivateFile(path, []byte("first")); err != nil {
		t.Fatalf("write new plan: %v", err)
	}
	if err := writeNewPrivateFile(path, []byte("second")); err == nil || !strings.Contains(err.Error(), "never overwritten") {
		t.Fatalf("expected immutable overwrite rejection, got %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "first" {
		t.Fatalf("existing plan changed: %q", content)
	}
}

func TestHelpCommandsSucceed(t *testing.T) {
	for _, arguments := range [][]string{{"help"}, {"scan", "--help"}, {"explain", "--help"}, {"plan", "--help"}} {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		if exitCode := runWithDependencies(arguments, &stdout, &stderr, commandDependencies{}); exitCode != exitSuccess {
			t.Errorf("%v exit = %d", arguments, exitCode)
		}
	}
}

func manualDockerCacheResource() domain.InstalledResource {
	size := int64(2048)
	evidence := []domain.Evidence{{SourceType: "test", RuleID: "test.rule"}}
	return domain.InstalledResource{
		ID: "resource-cache", Ecosystem: "docker", Component: "docker_build_cache", Path: "docker://build_cache/cache123456789",
		SizeBytes:              &size,
		PotentiallyReclaimable: &domain.SpaceEstimate{Bytes: size, Rationale: "fixture", Evidence: evidence},
		ReferenceStatus:        domain.ReferenceUnknown,
		Classifications: []domain.ResourceClassification{
			{Category: domain.CategoryReconstructible, Rationale: "fixture", Evidence: evidence},
			{Category: domain.CategoryUnknown, Rationale: "fixture", Evidence: evidence},
		},
		Metadata: []domain.MetadataEntry{{Key: "management", Value: "docker buildx"}},
		Warnings: []string{},
	}
}

func validDocument() domain.ScanDocument {
	version := "3.35.0"
	size := int64(1024)
	return domain.ScanDocument{
		SchemaVersion: domain.SchemaVersion,
		ToolVersion:   version,
		Scan: domain.ScanMetadata{
			StartedAt:   time.Date(2026, 9, 2, 20, 0, 0, 0, time.UTC).Format(time.RFC3339),
			CompletedAt: time.Date(2026, 9, 2, 20, 0, 1, 0, time.UTC).Format(time.RFC3339),
			Roots:       []string{"/work"},
			Exclusions:  []string{},
			ReadOnly:    true,
		},
		Projects: []domain.Project{},
		InstalledResources: []domain.InstalledResource{
			{
				ID:              "resource-one",
				Ecosystem:       "flutter",
				Component:       "flutter_sdk",
				Version:         &version,
				Path:            "/sdk/flutter",
				SizeBytes:       &size,
				ReferenceStatus: domain.ReferenceUnknown,
				Metadata:        []domain.MetadataEntry{},
				Warnings:        []string{},
			},
		},
		Relations:   []domain.Relation{},
		Diagnostics: []domain.Diagnostic{},
	}
}

func documentWithError() domain.ScanDocument {
	document := validDocument()
	document.Diagnostics = []domain.Diagnostic{
		{Code: "TEST_ERROR", Severity: domain.SeverityError, Scope: "test", Message: "partial failure"},
	}
	return document
}
