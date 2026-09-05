package environments

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"dev-environment-auditor/internal/domain"
)

func TestDockerJDKMatchesWithoutClaimingHostInstallation(t *testing.T) {
	root := t.TempDir()
	writeDockerFixture(t, filepath.Join(root, "Dockerfile"), `FROM --platform=linux/arm64 eclipse-temurin:21-jdk-alpine AS builder
FROM eclipse-temurin:21-jre-alpine
`)
	constraint := "21"
	project := domain.Project{
		ID:   "project-api",
		Path: root,
		Kind: domain.ProjectAndroid,
		Requirements: []domain.Requirement{
			{
				ID:                "requirement-jdk-21",
				Ecosystem:         "java",
				Component:         "jdk",
				VersionConstraint: &constraint,
				Confidence:        domain.RequiredExplicitly,
				Evidence:          []domain.Evidence{},
				Warnings:          []string{},
			},
		},
		Warnings: []string{},
	}

	result := New(OSFileSystem{}).AnalyzeProjects(context.Background(), []domain.Project{project}, Config{})
	if len(result.Relations) != 1 {
		t.Fatalf("expected one Docker relation, got %#v", result.Relations)
	}
	relation := result.Relations[0]
	if relation.Environment != domain.EnvironmentDocker || relation.MatchStatus != domain.Matched {
		t.Fatalf("unexpected Docker relation: %#v", relation)
	}
	if relation.ResourceID != nil {
		t.Fatalf("Docker declaration must not masquerade as an installed resource: %#v", relation)
	}
	if len(relation.Evidence) != 1 || relation.Evidence[0].LineHint == nil || *relation.Evidence[0].LineHint != 1 {
		t.Fatalf("expected only the JDK builder image as evidence: %#v", relation.Evidence)
	}
	if result.Stats.FilesRead != 1 {
		t.Fatalf("expected one Dockerfile read, got %#v", result.Stats)
	}
	if !result.Complete {
		t.Fatalf("static Dockerfile analysis should be complete: %#v", result.Diagnostics)
	}
}

func TestDockerJDKMismatchStaysUnknown(t *testing.T) {
	root := t.TempDir()
	writeDockerFixture(t, filepath.Join(root, "Dockerfile"), "FROM amazoncorretto:17-alpine\n")
	constraint := "21"
	project := jdkProject(root, constraint)

	result := New(OSFileSystem{}).AnalyzeProjects(context.Background(), []domain.Project{project}, Config{})
	if len(result.Relations) != 1 || result.Relations[0].MatchStatus != domain.MatchUnknown {
		t.Fatalf("Docker mismatch must stay unknown, got %#v", result.Relations)
	}
}

func TestDynamicDockerImageIsDiagnosedWithoutInventingRelation(t *testing.T) {
	root := t.TempDir()
	writeDockerFixture(t, filepath.Join(root, "Dockerfile.dev"), "ARG JAVA_IMAGE\nFROM ${JAVA_IMAGE}\n")
	project := jdkProject(root, "21")

	result := New(OSFileSystem{}).AnalyzeProjects(context.Background(), []domain.Project{project}, Config{})
	if len(result.Relations) != 0 {
		t.Fatalf("dynamic image must not create a relation: %#v", result.Relations)
	}
	if !hasDockerDiagnostic(result.Diagnostics, "DOCKER_IMAGE_DYNAMIC") {
		t.Fatalf("expected dynamic-image diagnostic: %#v", result.Diagnostics)
	}
	if result.Complete {
		t.Fatal("dynamic image must make Docker reference coverage incomplete")
	}
}

func TestDockerfileAndComposeImagesBecomeDeduplicatedRequirements(t *testing.T) {
	root := t.TempDir()
	writeDockerFixture(t, filepath.Join(root, "Dockerfile"), `FROM node:24 AS builder
FROM builder AS packaged
FROM alpine:3.22
`)
	writeDockerFixture(t, filepath.Join(root, "compose.yaml"), `services:
  web:
    image: "node:24" # same declaration as the Dockerfile
  database:
    image: postgres:16
  generated:
    image: ${APP_IMAGE:-example/app:latest}
`)
	writeDockerFixture(t, filepath.Join(root, "vendor", "dependency", "Dockerfile"), "FROM php:8.4\n")
	project := domain.Project{ID: "project-stack", Path: root, Kind: domain.ProjectDocker, Requirements: []domain.Requirement{}, Warnings: []string{}}

	result := New(OSFileSystem{}).AnalyzeProjects(context.Background(), []domain.Project{project}, Config{})
	if len(result.Projects) != 1 || len(result.Projects[0].Requirements) != 3 {
		t.Fatalf("expected node, alpine and postgres requirements: %#v", result.Projects)
	}
	constraints := map[string]int{}
	for _, requirement := range result.Projects[0].Requirements {
		if requirement.Ecosystem != "docker" || requirement.Component != "image" ||
			requirement.Confidence != domain.RequiredExplicitly || requirement.VersionConstraint == nil {
			t.Fatalf("unexpected Docker image requirement: %#v", requirement)
		}
		constraints[*requirement.VersionConstraint] = len(requirement.Evidence)
	}
	if constraints["node:24"] != 2 || constraints["alpine:3.22"] != 1 || constraints["postgres:16"] != 1 {
		t.Fatalf("unexpected declarations or evidence counts: %#v", constraints)
	}
	if _, internalStage := constraints["builder"]; internalStage {
		t.Fatal("a multi-stage alias must not become an external image requirement")
	}
	if !hasDockerDiagnostic(result.Diagnostics, "DOCKER_IMAGE_DYNAMIC") {
		t.Fatalf("dynamic Compose image should remain diagnosed: %#v", result.Diagnostics)
	}
	if result.Stats.FilesRead != 2 {
		t.Fatalf("expected both declaration files to be read: %#v", result.Stats)
	}
	if result.Complete {
		t.Fatal("dynamic Compose image must make Docker reference coverage incomplete")
	}
}

func TestComposeParserIgnoresNestedImageKeysAndReportsInlineMappings(t *testing.T) {
	project := domain.Project{ID: "project-stack", Path: "/work/stack", Kind: domain.ProjectDocker}
	declarations, diagnostics := parseComposeFile(project, "/work/stack/compose.yml", []byte(`services:
  app:
    build:
      args:
        image: must-not-match
    image: ghcr.io/example/app:1.2.3
  inline: { image: nginx:latest }
`))
	if len(declarations) != 1 || declarations[0].image != "ghcr.io/example/app:1.2.3" {
		t.Fatalf("unexpected Compose declarations: %#v", declarations)
	}
	if !hasDockerDiagnostic(diagnostics, "DOCKER_COMPOSE_STRUCTURE_UNSUPPORTED") {
		t.Fatalf("inline service mapping should be reported: %#v", diagnostics)
	}
}

func TestDockerSearchSilentlySkipsSymlinksAlreadyCoveredByDiscovery(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "Dockerfile")
	writeDockerFixture(t, target, "FROM eclipse-temurin:21-jdk\n")
	if err := os.Symlink(target, filepath.Join(root, "Dockerfile")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "Dockerfile.dev")); err != nil {
		t.Fatal(err)
	}

	result := New(OSFileSystem{}).AnalyzeProjects(
		context.Background(),
		[]domain.Project{jdkProject(root, "21")},
		Config{},
	)
	if len(result.Relations) != 0 {
		t.Fatalf("symlinked Dockerfiles must not be read: %#v", result.Relations)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Discovery owns the symlink summary; Docker analysis must not duplicate it: %#v", result.Diagnostics)
	}
}

func TestDockerImageJDKFeatureKnownFormats(t *testing.T) {
	tests := map[string]int{
		"eclipse-temurin:21-jdk-alpine":               21,
		"amazoncorretto:17-alpine":                    17,
		"gradle:8.14.3-jdk24-alpine":                  24,
		"maven:3.9.9-eclipse-temurin-21-alpine":       21,
		"registry.example:5000/library/openjdk:8-jdk": 8,
	}
	for image, expected := range tests {
		feature, isJDK, known := dockerImageJDKFeature(image)
		if !isJDK || !known || feature != expected {
			t.Errorf("%s = %d, %v, %v; want %d, true, true", image, feature, isJDK, known, expected)
		}
	}
	if _, isJDK, _ := dockerImageJDKFeature("eclipse-temurin:21-jre-alpine"); isJDK {
		t.Fatal("JRE image must not be classified as a JDK")
	}
	if _, isJDK, _ := dockerImageJDKFeature("alpine:3.22"); isJDK {
		t.Fatal("unrelated image must not be classified as a JDK")
	}
}

func jdkProject(root, constraint string) domain.Project {
	return domain.Project{
		ID:   "project-api",
		Path: root,
		Kind: domain.ProjectAndroid,
		Requirements: []domain.Requirement{
			{
				ID:                "requirement-jdk",
				Ecosystem:         "java",
				Component:         "jdk",
				VersionConstraint: &constraint,
				Confidence:        domain.RequiredExplicitly,
				Evidence:          []domain.Evidence{},
				Warnings:          []string{},
			},
		},
		Warnings: []string{},
	}
}

func writeDockerFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func hasDockerDiagnostic(diagnostics []domain.Diagnostic, code string) bool {
	_, found := dockerDiagnosticByCode(diagnostics, code)
	return found
}

func dockerDiagnosticByCode(diagnostics []domain.Diagnostic, code string) (domain.Diagnostic, bool) {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return diagnostic, true
		}
	}
	return domain.Diagnostic{}, false
}
