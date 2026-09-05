package dockerinventory

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"dev-environment-auditor/internal/domain"
)

type fakeRunner struct {
	path        string
	lookPathErr error
	results     map[string]CommandResult
	calls       [][]string
}

func (runner *fakeRunner) LookPath(string) (string, error) {
	if runner.lookPathErr != nil {
		return "", runner.lookPathErr
	}
	return runner.path, nil
}

func (runner *fakeRunner) Run(_ context.Context, name string, arguments []string, _ int64) CommandResult {
	call := append([]string{name}, arguments...)
	runner.calls = append(runner.calls, call)
	return runner.results[commandKey(arguments)]
}

func commandKey(arguments []string) string {
	if len(arguments) >= 2 {
		return arguments[0] + " " + arguments[1]
	}
	return strings.Join(arguments, " ")
}

func TestInspectInventoriesAllowlistedDockerResources(t *testing.T) {
	imageID := "sha256:" + strings.Repeat("a", 64)
	containerID := strings.Repeat("b", 64)
	cacheID := "ic1gfidvev5nciupzz53alel4"
	runner := &fakeRunner{
		path: "/usr/local/bin/docker",
		results: map[string]CommandResult{
			"version --format": {Stdout: []byte(`{"Client":{"Version":"29.0.1"},"Server":{"Version":"29.0.1"}}`)},
			"image ls": {Stdout: []byte(strings.Join([]string{
				jsonFields(imageID, "postgres", "16", "sha256:"+strings.Repeat("c", 64), "2026-09-01 10:00:00 +0000 UTC", "72.9MB"),
				jsonFields(imageID, "postgres", "latest", "sha256:"+strings.Repeat("c", 64), "2026-09-01 10:00:00 +0000 UTC", "72.9MB"),
			}, "\n"))},
			"container ls": {Stdout: []byte(jsonFields(
				containerID, "postgres:16", "database", "exited", "Exited (0) 2 days ago",
				"2026-08-01 10:00:00 +0000 UTC", "35.58kB (virtual 109.2MB)",
			))},
			"buildx ls": {Stdout: []byte(strings.Join([]string{
				jsonFields("default", "docker", "", "", "", "2026-09-05T10:00:00Z", "default"),
				jsonFields("default", "default", "running", "v0.26.2", "linux/arm64", "", "default"),
			}, "\n"))},
			"buildx du": {Stdout: []byte(`{"CreatedAt":"2025-07-29T12:36:01Z","ID":"` + cacheID + `","LastUsedAt":"2025-08-05T09:24:09Z","Mutable":false,"Reclaimable":true,"Shared":true,"Size":"829889526","Type":"regular","UsageCount":1}`)},
		},
	}

	result := New(runner).Inspect(context.Background(), Config{CommandTimeout: time.Second})
	if len(result.Resources) != 4 || result.Stats.CommandsRun != 5 || result.Stats.ResourcesFound != 4 {
		t.Fatalf("unexpected Docker inventory: %#v", result)
	}
	if !reflect.DeepEqual(result.CompleteComponents, []string{"docker_image"}) {
		t.Fatalf("complete Docker scopes = %#v", result.CompleteComponents)
	}
	image := findDockerResource(result.Resources, "docker_image")
	if image == nil || image.SizeBytes == nil || *image.SizeBytes != 72_900_000 ||
		!hasDockerMetadata(image.Metadata, "reference", "postgres:16") ||
		!hasDockerMetadata(image.Metadata, "reference", "postgres:latest") {
		t.Fatalf("image was not deduplicated correctly: %#v", image)
	}
	container := findDockerResource(result.Resources, "docker_container")
	if container == nil || container.SizeBytes == nil || *container.SizeBytes != 35_580 ||
		!hasDockerMetadata(container.Metadata, "sensitivity", "sensitive_mutable_data") {
		t.Fatalf("unexpected container: %#v", container)
	}
	builder := findDockerResource(result.Resources, "docker_builder")
	if builder == nil || builder.Version == nil || *builder.Version != "v0.26.2" {
		t.Fatalf("unexpected builder: %#v", builder)
	}
	cache := findDockerResource(result.Resources, "docker_build_cache")
	if cache == nil || cache.SizeBytes == nil || *cache.SizeBytes != 829_889_526 ||
		!hasDockerMetadata(cache.Metadata, "reclaimable_by_docker", "true") {
		t.Fatalf("unexpected build cache: %#v", cache)
	}
	for _, resource := range result.Resources {
		if resource.ReferenceStatus != domain.ReferenceUnknown || !strings.HasPrefix(resource.Path, "docker://") {
			t.Fatalf("Docker resource must remain local and unknown: %#v", resource)
		}
	}
	for _, call := range runner.calls {
		joined := strings.Join(call, " ")
		for _, forbidden := range []string{" prune", " rm", " stop", " start", " run", " pull", " build "} {
			if strings.Contains(joined, forbidden) {
				t.Fatalf("destructive or mutating Docker command invoked: %q", joined)
			}
		}
	}
}

func TestInspectTreatsMissingClientAndDaemonAsInformational(t *testing.T) {
	missing := New(&fakeRunner{lookPathErr: os.ErrNotExist}).Inspect(context.Background(), Config{})
	if !hasDockerDiagnostic(missing.Diagnostics, "DOCKER_CLIENT_NOT_FOUND", domain.SeverityInfo) || len(missing.Resources) != 0 {
		t.Fatalf("unexpected missing-client result: %#v", missing)
	}

	runner := &fakeRunner{
		path: "/usr/local/bin/docker",
		results: map[string]CommandResult{
			"version --format": {
				Stdout: []byte(`{"Client":{"Version":"29.0.1"},"Server":null}`),
				Err:    errors.New("daemon unavailable"),
			},
		},
	}
	unavailable := New(runner).Inspect(context.Background(), Config{})
	if !hasDockerDiagnostic(unavailable.Diagnostics, "DOCKER_DAEMON_UNAVAILABLE", domain.SeverityInfo) ||
		!hasDockerDiagnostic(unavailable.Diagnostics, "DOCKER_CLIENT_FOUND", domain.SeverityInfo) ||
		len(unavailable.Resources) != 0 || len(runner.calls) != 1 {
		t.Fatalf("unexpected daemon-unavailable result: %#v", unavailable)
	}
}

func TestInspectBoundsOutputAndResourceCount(t *testing.T) {
	runner := &fakeRunner{
		path: "/usr/local/bin/docker",
		results: map[string]CommandResult{
			"version --format": {Stdout: []byte(`{"Client":{"Version":"29"},"Server":{"Version":"29"}}`)},
			"image ls": {
				Stdout:        []byte("truncated"),
				OutputLimited: true,
			},
			"container ls": {Stdout: []byte(jsonFields(strings.Repeat("a", 64), "alpine", "one", "exited", "Exited", "date", "0B"))},
			"buildx ls":    {Stdout: []byte(jsonFields("default", "docker", "", "", "", "date", "default"))},
			"buildx du":    {Stdout: []byte(`{"ID":"abcdefghijklmnopqrstuvwx","Size":"10","Reclaimable":true}`)},
		},
	}
	result := New(runner).Inspect(context.Background(), Config{MaxResources: 1})
	if len(result.Resources) != 1 || !hasDockerDiagnostic(result.Diagnostics, "DOCKER_INVENTORY_OUTPUT_LIMIT_REACHED", domain.SeverityWarning) ||
		!hasDockerDiagnostic(result.Diagnostics, "DOCKER_INVENTORY_MAX_RESOURCES_REACHED", domain.SeverityWarning) {
		t.Fatalf("bounds were not enforced: %#v", result)
	}
	if len(result.CompleteComponents) != 0 {
		t.Fatalf("truncated image inventory must not be complete: %#v", result.CompleteComponents)
	}
}

func TestParsingRejectsMalformedRowsAndUnderstandsSizes(t *testing.T) {
	if resources, diagnostics := parseImages([]byte("not-json"), 10); len(resources) != 0 ||
		!hasDockerDiagnostic(diagnostics, "DOCKER_IMAGE_OUTPUT_MALFORMED", domain.SeverityWarning) {
		t.Fatalf("malformed image row was accepted: %#v %#v", resources, diagnostics)
	}
	for input, want := range map[string]int64{
		"0B":      0,
		"1.5kB":   1_500,
		"2MiB":    2 << 20,
		"1.25 GB": 1_250_000_000,
	} {
		got := parseDockerSize(input)
		if got == nil || *got != want {
			t.Fatalf("parseDockerSize(%q) = %v, want %d", input, got, want)
		}
	}
	if parseDockerSize("unknown") != nil || parseExactBytes("-1") != nil {
		t.Fatal("invalid sizes must remain unknown")
	}
	if got := parseBuildCacheSize("1.5MB"); got == nil || *got != 1_500_000 {
		t.Fatalf("human-readable BuildKit size was not parsed: %v", got)
	}
}

func TestBoundedBufferStopsAtLimit(t *testing.T) {
	buffer := newBoundedBuffer(4)
	written, err := buffer.Write([]byte("abcdef"))
	if written != 4 || !errors.Is(err, errOutputLimit) || !buffer.Limited() || !reflect.DeepEqual(buffer.Bytes(), []byte("abcd")) {
		t.Fatalf("unexpected bounded buffer: written=%d err=%v limited=%v bytes=%q", written, err, buffer.Limited(), buffer.Bytes())
	}
}

func jsonFields(values ...string) string {
	encoded := make([]string, len(values))
	for index, value := range values {
		content, _ := json.Marshal(value)
		encoded[index] = string(content)
	}
	return strings.Join(encoded, "\t")
}

func findDockerResource(resources []domain.InstalledResource, component string) *domain.InstalledResource {
	for index := range resources {
		if resources[index].Component == component {
			return &resources[index]
		}
	}
	return nil
}

func hasDockerMetadata(metadata []domain.MetadataEntry, key, value string) bool {
	for _, item := range metadata {
		if item.Key == key && item.Value == value {
			return true
		}
	}
	return false
}

func hasDockerDiagnostic(diagnostics []domain.Diagnostic, code string, severity domain.DiagnosticSeverity) bool {
	for _, item := range diagnostics {
		if item.Code == code && item.Severity == severity {
			return true
		}
	}
	return false
}
