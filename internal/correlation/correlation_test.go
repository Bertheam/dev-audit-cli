package correlation

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"dev-environment-auditor/internal/domain"
)

func TestCorrelateMatchesSupportedExplicitRequirements(t *testing.T) {
	project := domain.Project{
		ID:   "project-app",
		Path: "/work/app",
		Kind: domain.ProjectHybrid,
		Requirements: []domain.Requirement{
			requirement("compile", "android", "compile_sdk", "35", domain.RequiredExplicitly),
			requirement("ndk", "android", "ndk", "27.0.12077973", domain.RequiredExplicitly),
			requirement("cmake", "android", "cmake", "3.22.1", domain.RequiredExplicitly),
			requirement("agp", "android", "android_gradle_plugin", "8.7.3", domain.RequiredExplicitly),
			requirement("flutter", "flutter", "flutter_sdk", "3.35.0", domain.RequiredExplicitly),
			requirement("dart", "dart", "dart_sdk", ">=3.9.0 <4.0.0", domain.RequiredExplicitly),
			requirement("gradle", "gradle", "gradle", "8.12", domain.RequiredExplicitly),
			requirement("kotlin", "kotlin", "kotlin_gradle_plugin", "2.1.0", domain.RequiredExplicitly),
			requirement("jdk", "java", "jdk", "17", domain.RequiredExplicitly),
		},
	}
	resources := []domain.InstalledResource{
		resource("jdk17", "java", "jdk", "17.0.12"),
		resource("kotlin210", "kotlin", "kotlin_gradle_plugin", "2.1.0"),
		resource("gradle812", "gradle", "gradle", "8.12"),
		flutterResource("flutter335", "3.35.0", "stable", "3.9.0"),
		resource("agp873", "android", "android_gradle_plugin", "8.7.3"),
		resource("cmake3221", "android", "cmake", "3.22.1"),
		resource("ndk27", "android", "ndk", "27.0.12077973"),
		resource("platform35", "android", "android_sdk_platform", "35"),
	}

	result := Correlate([]domain.Project{project}, resources, completeCoverage())

	if len(result.Relations) != len(project.Requirements) {
		t.Fatalf("got %d relations, want %d", len(result.Relations), len(project.Requirements))
	}
	for _, relation := range result.Relations {
		if relation.Environment != domain.EnvironmentHost {
			t.Errorf("relation %s environment = %s, want HOST", relation.RequirementID, relation.Environment)
		}
		if relation.MatchStatus != domain.Matched || relation.ResourceID == nil {
			t.Errorf("relation %s = %s (%v), want unique match", relation.RequirementID, relation.MatchStatus, relation.ResourceID)
		}
		if len(relation.Evidence) != 1 || relation.Evidence[0].RuleID != "test.rule" {
			t.Errorf("relation %s did not preserve evidence: %#v", relation.RequirementID, relation.Evidence)
		}
	}
	for _, installed := range result.Resources {
		if installed.ReferenceStatus != domain.Referenced {
			t.Errorf("resource %s = %s, want REFERENCED", installed.ID, installed.ReferenceStatus)
		}
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", result.Diagnostics)
	}
}

func TestMissingRequiresCompleteInventoryScope(t *testing.T) {
	project := projectWith(requirement("gradle", "gradle", "gradle", "8.12", domain.RequiredExplicitly))
	wrongVersion := resource("gradle811", "gradle", "gradle", "8.11")

	incomplete := Correlate([]domain.Project{project}, []domain.InstalledResource{wrongVersion}, Coverage{
		ProjectDiscoveryComplete:    true,
		RequirementAnalysisComplete: true,
	})
	if incomplete.Relations[0].MatchStatus != domain.MatchUnknown {
		t.Fatalf("incomplete inventory = %s, want UNKNOWN", incomplete.Relations[0].MatchStatus)
	}
	if incomplete.Resources[0].ReferenceStatus != domain.NoReferenceFound {
		t.Fatalf("known non-match = %s, want NO_REFERENCE_FOUND in complete project coverage", incomplete.Resources[0].ReferenceStatus)
	}

	complete := Correlate([]domain.Project{project}, []domain.InstalledResource{wrongVersion}, Coverage{
		ProjectDiscoveryComplete:    true,
		RequirementAnalysisComplete: true,
		CompleteInventoryScopes: []InventoryScope{
			{Ecosystem: "gradle", Component: "gradle"},
		},
	})
	if complete.Relations[0].MatchStatus != domain.Missing {
		t.Fatalf("complete inventory = %s, want MISSING", complete.Relations[0].MatchStatus)
	}
}

func TestCorrelateDockerImageAgainstDaemonReferences(t *testing.T) {
	project := domain.Project{
		ID:   "project-stack",
		Path: "/work/stack",
		Kind: domain.ProjectDocker,
		Requirements: []domain.Requirement{
			requirement("postgres", "docker", "image", "docker.io/library/postgres:16", domain.RequiredExplicitly),
			requirement("nginx", "docker", "image", "nginx", domain.RequiredExplicitly),
		},
	}
	postgres := resourceWithoutVersion("postgres-image", "docker", "docker_image")
	postgres.Metadata = []domain.MetadataEntry{
		{Key: "reference", Value: "postgres:latest"},
		{Key: "reference", Value: "postgres:16"},
	}

	result := Correlate([]domain.Project{project}, []domain.InstalledResource{postgres}, Coverage{
		ProjectDiscoveryComplete:    true,
		RequirementAnalysisComplete: true,
		CompleteInventoryScopes: []InventoryScope{
			{Ecosystem: "docker", Component: "docker_image"},
		},
	})
	if len(result.Relations) != 2 {
		t.Fatalf("expected two Docker daemon relations: %#v", result.Relations)
	}
	statuses := map[string]domain.Relation{}
	for _, relation := range result.Relations {
		statuses[relation.RequirementID] = relation
		if relation.Environment != domain.EnvironmentDockerDaemon {
			t.Fatalf("Docker image relation environment = %s", relation.Environment)
		}
	}
	if statuses["postgres"].MatchStatus != domain.Matched || statuses["postgres"].ResourceID == nil {
		t.Fatalf("Docker Hub aliases should match the local image: %#v", statuses["postgres"])
	}
	if statuses["nginx"].MatchStatus != domain.Missing {
		t.Fatalf("complete daemon image inventory should prove nginx missing: %#v", statuses["nginx"])
	}
	if result.Resources[0].ReferenceStatus != domain.Referenced {
		t.Fatalf("matched Docker image should be referenced: %#v", result.Resources[0])
	}
}

func TestNormalizeDockerImageReference(t *testing.T) {
	tests := map[string]string{
		"nginx":                             "docker.io/library/nginx:latest",
		"postgres:16":                       "docker.io/library/postgres:16",
		"library/postgres:16":               "docker.io/library/postgres:16",
		"index.docker.io/library/nginx:1.2": "docker.io/library/nginx:1.2",
		"docker.io/nginx":                   "docker.io/library/nginx:latest",
		"ghcr.io/Example/App:Release":       "ghcr.io/example/app:Release",
		"localhost:5000/team/app":           "localhost:5000/team/app:latest",
	}
	for input, want := range tests {
		got, ok := normalizeDockerImageReference(input)
		if !ok || got != want {
			t.Errorf("normalizeDockerImageReference(%q) = %q, %v; want %q", input, got, ok, want)
		}
	}
	if _, ok := normalizeDockerImageReference("${IMAGE}"); ok {
		t.Fatal("dynamic image reference must be rejected")
	}
}

func TestUnknownVersionsAndAmbiguityProtectCandidateResources(t *testing.T) {
	tests := []struct {
		name        string
		resources   []domain.InstalledResource
		wantStatus  domain.MatchStatus
		wantUnknown int
	}{
		{
			name: "two exact matches",
			resources: []domain.InstalledResource{
				resource("one", "gradle", "gradle", "8.12"),
				resource("two", "gradle", "gradle", "8.12"),
			},
			wantStatus:  domain.Ambiguous,
			wantUnknown: 2,
		},
		{
			name: "one exact and one unversioned",
			resources: []domain.InstalledResource{
				resource("one", "gradle", "gradle", "8.12"),
				resourceWithoutVersion("unknown", "gradle", "gradle"),
			},
			wantStatus:  domain.Ambiguous,
			wantUnknown: 2,
		},
		{
			name: "only unversioned",
			resources: []domain.InstalledResource{
				resourceWithoutVersion("unknown", "gradle", "gradle"),
			},
			wantStatus:  domain.MatchUnknown,
			wantUnknown: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			project := projectWith(requirement("gradle", "gradle", "gradle", "8.12", domain.RequiredExplicitly))
			result := Correlate([]domain.Project{project}, test.resources, completeCoverage())
			if result.Relations[0].MatchStatus != test.wantStatus {
				t.Fatalf("status = %s, want %s", result.Relations[0].MatchStatus, test.wantStatus)
			}
			unknown := 0
			for _, installed := range result.Resources {
				if installed.ReferenceStatus == domain.ReferenceUnknown {
					unknown++
				}
			}
			if unknown != test.wantUnknown {
				t.Fatalf("UNKNOWN resources = %d, want %d: %#v", unknown, test.wantUnknown, result.Resources)
			}
		})
	}
}

func TestNonExplicitAndUnsupportedRequirementsRemainUnknown(t *testing.T) {
	project := projectWith(
		requirementWithoutVersion("dynamic", "gradle", "gradle", domain.ProbablyRequired),
		requirement("pre", "dart", "dart_sdk", ">=3.9.0-0 <4.0.0", domain.RequiredExplicitly),
		requirement("min", "android", "min_sdk", "23", domain.RequiredExplicitly),
	)
	resources := []domain.InstalledResource{
		resource("gradle", "gradle", "gradle", "8.12"),
		flutterResource("flutter", "3.35.0", "stable", "3.9.0"),
		resource("build-tools", "android", "android_build_tools", "35.0.0"),
	}

	result := Correlate([]domain.Project{project}, resources, completeCoverage())
	for _, relation := range result.Relations {
		if relation.MatchStatus != domain.MatchUnknown {
			t.Errorf("relation %s = %s, want UNKNOWN", relation.RequirementID, relation.MatchStatus)
		}
	}
	if statusForResource(t, result.Resources, "gradle") != domain.ReferenceUnknown {
		t.Fatal("a probable Gradle requirement must protect possible Gradle candidates")
	}
	if statusForResource(t, result.Resources, "flutter") != domain.ReferenceUnknown {
		t.Fatal("an unsupported Dart constraint must protect possible Flutter candidates")
	}
	if statusForResource(t, result.Resources, "build-tools") != domain.ReferenceUnknown {
		t.Fatal("an unsupported inventory component must never be labelled unreferenced")
	}
	if !hasDiagnostic(result.Diagnostics, "CORRELATION_CONSTRAINT_UNSUPPORTED") ||
		!hasDiagnostic(result.Diagnostics, "CORRELATION_COMPONENT_NOT_INVENTORIED") {
		t.Fatalf("missing conservative diagnostics: %#v", result.Diagnostics)
	}
}

func TestReferenceStatusRequiresCompleteProjectAndAnalyzerCoverage(t *testing.T) {
	unused := resource("jdk21", "java", "jdk", "21.0.4")
	tests := []struct {
		name     string
		coverage Coverage
		want     domain.ReferenceStatus
	}{
		{name: "no coverage", coverage: Coverage{}, want: domain.ReferenceUnknown},
		{
			name:     "projects only",
			coverage: Coverage{ProjectDiscoveryComplete: true},
			want:     domain.ReferenceUnknown,
		},
		{
			name:     "analysis only",
			coverage: Coverage{RequirementAnalysisComplete: true},
			want:     domain.ReferenceUnknown,
		},
		{
			name: "complete analyzed coverage",
			coverage: Coverage{
				ProjectDiscoveryComplete:    true,
				RequirementAnalysisComplete: true,
			},
			want: domain.NoReferenceFound,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := Correlate(nil, []domain.InstalledResource{unused}, test.coverage)
			if result.Resources[0].ReferenceStatus != test.want {
				t.Fatalf("status = %s, want %s", result.Resources[0].ReferenceStatus, test.want)
			}
		})
	}
}

func TestFlutterChannelAndJDKFeatureNormalization(t *testing.T) {
	project := projectWith(
		requirement("flutter", "flutter", "flutter_sdk", "stable", domain.RequiredExplicitly),
		requirement("jdk", "java", "jdk", "8", domain.RequiredExplicitly),
	)
	resources := []domain.InstalledResource{
		flutterResource("stable-sdk", "3.35.0", "stable", "3.9.0"),
		resource("jdk8", "java", "jdk", "1.8.0_442"),
	}

	result := Correlate([]domain.Project{project}, resources, completeCoverage())
	for _, relation := range result.Relations {
		if relation.MatchStatus != domain.Matched {
			t.Errorf("relation %s = %s, want MATCHED", relation.RequirementID, relation.MatchStatus)
		}
	}
}

func TestCorrelateIsDeterministicAndDoesNotMutateInputs(t *testing.T) {
	line := 7
	path := "/work/app/pubspec.yaml"
	value := ">=3.9.0 <4.0.0"
	project := projectWith(requirement("dart", "dart", "dart_sdk", value, domain.RequiredExplicitly))
	project.Requirements[0].Evidence[0].FilePath = &path
	project.Requirements[0].Evidence[0].LineHint = &line
	project.Requirements[0].Evidence[0].Command = []string{"never", "run"}
	resources := []domain.InstalledResource{
		flutterResource("z", "3.35.0", "stable", "3.9.0"),
		resource("a", "java", "jdk", "21.0.4"),
	}
	projectsBefore := mustJSON(t, []domain.Project{project})
	resourcesBefore := mustJSON(t, resources)

	first := Correlate([]domain.Project{project}, resources, completeCoverage())
	second := Correlate([]domain.Project{project}, resources, completeCoverage())
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("repeated results differ:\n%#v\n%#v", first, second)
	}
	if string(mustJSON(t, []domain.Project{project})) != string(projectsBefore) {
		t.Fatal("project input was mutated")
	}
	if string(mustJSON(t, resources)) != string(resourcesBefore) {
		t.Fatal("resource input was mutated")
	}

	*first.Projects[0].Requirements[0].VersionConstraint = "changed"
	first.Projects[0].Requirements[0].Evidence[0].Command[0] = "changed"
	first.Resources[0].Metadata[0].Value = "changed"
	if *project.Requirements[0].VersionConstraint != value ||
		project.Requirements[0].Evidence[0].Command[0] != "never" {
		t.Fatal("nested project values alias the input")
	}
	if strings.EqualFold(resources[0].Metadata[0].Value, "changed") {
		t.Fatal("resource metadata aliases the input")
	}
}

func TestDartConstraintSubset(t *testing.T) {
	tests := []struct {
		constraint string
		version    string
		want       bool
		supported  bool
	}{
		{constraint: ">=3.9.0 <4.0.0", version: "3.9.0", want: true, supported: true},
		{constraint: ">=3.9.0 <4.0.0", version: "4.0.0", want: false, supported: true},
		{constraint: "^3.2.0", version: "3.13.0", want: true, supported: true},
		{constraint: "^3.2.0", version: "4.0.0", want: false, supported: true},
		{constraint: "^0.1.2", version: "0.1.9", want: true, supported: true},
		{constraint: "^0.1.2", version: "0.2.0", want: false, supported: true},
		{constraint: "3.9.0", version: "3.9.0", want: true, supported: true},
		{constraint: "any", version: "99.0.0", want: true, supported: true},
		{constraint: ">=3.9.0-0 <4.0.0", version: "3.9.0", supported: false},
		{constraint: ">=3.9.0 || <4.0.0", version: "3.9.0", supported: false},
	}
	for _, test := range tests {
		t.Run(test.constraint+"/"+test.version, func(t *testing.T) {
			matcher, supported := compileDartConstraint(test.constraint)
			if supported != test.supported {
				t.Fatalf("supported = %v, want %v", supported, test.supported)
			}
			if !supported {
				return
			}
			version, valid := parseStableSemanticVersion(test.version)
			if !valid {
				t.Fatalf("test version %q should parse", test.version)
			}
			if got := matcher(version); got != test.want {
				t.Fatalf("match = %v, want %v", got, test.want)
			}
		})
	}
}

func completeCoverage() Coverage {
	return Coverage{
		ProjectDiscoveryComplete:    true,
		RequirementAnalysisComplete: true,
		CompleteInventoryScopes: []InventoryScope{
			{Ecosystem: "android", Component: "android_sdk_platform"},
			{Ecosystem: "android", Component: "ndk"},
			{Ecosystem: "android", Component: "cmake"},
			{Ecosystem: "android", Component: "android_gradle_plugin"},
			{Ecosystem: "flutter", Component: "flutter_sdk"},
			{Ecosystem: "gradle", Component: "gradle"},
			{Ecosystem: "kotlin", Component: "kotlin_gradle_plugin"},
			{Ecosystem: "java", Component: "jdk"},
		},
	}
}

func projectWith(requirements ...domain.Requirement) domain.Project {
	return domain.Project{
		ID:           "project-app",
		Path:         "/work/app",
		Kind:         domain.ProjectHybrid,
		Requirements: requirements,
		Warnings:     []string{},
	}
}

func requirement(
	id string,
	ecosystem string,
	component string,
	version string,
	confidence domain.Confidence,
) domain.Requirement {
	return domain.Requirement{
		ID:                id,
		Ecosystem:         ecosystem,
		Component:         component,
		VersionConstraint: stringPointer(version),
		Confidence:        confidence,
		Evidence: []domain.Evidence{
			{SourceType: "FILE", RuleID: "test.rule", Command: []string{}},
		},
		Warnings: []string{},
	}
}

func requirementWithoutVersion(
	id string,
	ecosystem string,
	component string,
	confidence domain.Confidence,
) domain.Requirement {
	requirement := requirement(id, ecosystem, component, "placeholder", confidence)
	requirement.VersionConstraint = nil
	return requirement
}

func resource(id string, ecosystem string, component string, version string) domain.InstalledResource {
	return domain.InstalledResource{
		ID:              id,
		Ecosystem:       ecosystem,
		Component:       component,
		Version:         stringPointer(version),
		Path:            "/sdk/" + id,
		ReferenceStatus: domain.ReferenceUnknown,
		Metadata:        []domain.MetadataEntry{},
		Warnings:        []string{},
	}
}

func resourceWithoutVersion(id string, ecosystem string, component string) domain.InstalledResource {
	installed := resource(id, ecosystem, component, "placeholder")
	installed.Version = nil
	return installed
}

func flutterResource(id string, version string, channel string, dartVersion string) domain.InstalledResource {
	installed := resource(id, "flutter", "flutter_sdk", version)
	installed.Metadata = []domain.MetadataEntry{
		{Key: "channel", Value: channel},
		{Key: "dart_sdk_version", Value: dartVersion},
	}
	return installed
}

func stringPointer(value string) *string {
	return &value
}

func statusForResource(
	t *testing.T,
	resources []domain.InstalledResource,
	id string,
) domain.ReferenceStatus {
	t.Helper()
	for _, installed := range resources {
		if installed.ID == id {
			return installed.ReferenceStatus
		}
	}
	t.Fatalf("resource %q not found", id)
	return ""
}

func hasDiagnostic(diagnostics []domain.Diagnostic, code string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("cannot marshal test value: %v", err)
	}
	return encoded
}
