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
		"--flutter-sdk-root", "/sdk/flutter",
		"--fvm-cache-root", "/cache/fvm",
		"--gradle-user-home", "/cache/gradle",
		"--jdk-root", "/jdks",
		"--format", "json",
		"--timeout", "2s",
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
	if len(captured.AndroidSDKRoots) != 1 || len(captured.FlutterSDKRoots) != 1 ||
		len(captured.FVMCacheRoots) != 1 || len(captured.GradleUserHomeRoots) != 1 || len(captured.JDKRoots) != 1 {
		t.Fatalf("typed inventory roots not forwarded: %#v", captured)
	}
	if _, err := report.DecodeJSON(stdout.Bytes()); err != nil {
		t.Fatalf("stdout is not schema-valid JSON: %v\n%s", err, stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %q", stderr.String())
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

func TestHelpCommandsSucceed(t *testing.T) {
	for _, arguments := range [][]string{{"help"}, {"scan", "--help"}, {"explain", "--help"}} {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		if exitCode := runWithDependencies(arguments, &stdout, &stderr, commandDependencies{}); exitCode != exitSuccess {
			t.Errorf("%v exit = %d", arguments, exitCode)
		}
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
