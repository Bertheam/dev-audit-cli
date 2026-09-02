package analyzers

import (
	"context"
	"crypto/sha256"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"dev-environment-auditor/internal/domain"
)

func TestAnalyzeExplicitFlutterAndroidFixture(t *testing.T) {
	projectRoot := repositoryFixturePath(t, "flutter_explicit")
	project := domain.Project{ID: "project-explicit", Path: projectRoot, Kind: domain.ProjectHybrid}
	analyzer := New(OSFileSystem{})

	first := analyzer.Analyze(context.Background(), project, Config{})
	second := analyzer.Analyze(context.Background(), project, Config{})

	expected := []struct {
		ecosystem  string
		component  string
		version    string
		confidence domain.Confidence
	}{
		{ecosystem: "android", component: "android_gradle_plugin", version: "8.7.3", confidence: domain.RequiredExplicitly},
		{ecosystem: "android", component: "compile_sdk", version: "35", confidence: domain.RequiredExplicitly},
		{ecosystem: "android", component: "min_sdk", version: "23", confidence: domain.RequiredExplicitly},
		{ecosystem: "android", component: "ndk", version: "27.0.12077973", confidence: domain.RequiredExplicitly},
		{ecosystem: "android", component: "target_sdk", version: "35", confidence: domain.RequiredExplicitly},
		{ecosystem: "dart", component: "dart_sdk", version: ">=3.9.0 <4.0.0", confidence: domain.RequiredExplicitly},
		{ecosystem: "flutter", component: "flutter_sdk", version: "3.35.0", confidence: domain.RequiredExplicitly},
		{ecosystem: "gradle", component: "gradle", version: "8.12", confidence: domain.RequiredExplicitly},
		{ecosystem: "java", component: "jdk", version: "17", confidence: domain.RequiredExplicitly},
		{ecosystem: "kotlin", component: "kotlin_gradle_plugin", version: "2.1.0", confidence: domain.RequiredExplicitly},
	}
	for _, requirement := range expected {
		if findRequirement(first.Requirements, requirement.ecosystem, requirement.component, requirement.version, requirement.confidence) == nil {
			t.Errorf("missing requirement %s/%s %s (%s): %#v", requirement.ecosystem, requirement.component, requirement.version, requirement.confidence, first.Requirements)
		}
	}
	if len(first.Requirements) != len(expected) {
		t.Fatalf("expected %d requirements, got %d: %#v", len(expected), len(first.Requirements), first.Requirements)
	}
	if len(first.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", first.Diagnostics)
	}
	if first.Stats.FilesRead != 7 {
		t.Fatalf("expected 7 allowlisted files, got %#v", first.Stats)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("identical analysis must be deterministic")
	}

	kotlin := findRequirement(first.Requirements, "kotlin", "kotlin_gradle_plugin", "2.1.0", domain.RequiredExplicitly)
	if kotlin == nil || len(kotlin.Evidence) != 4 {
		t.Fatalf("expected direct and version-catalog evidence for Kotlin, got %#v", kotlin)
	}
	for _, requirement := range first.Requirements {
		if len(requirement.Evidence) == 0 {
			t.Fatalf("requirement has no evidence: %#v", requirement)
		}
		for _, evidence := range requirement.Evidence {
			if evidence.FilePath == nil || !filepath.IsAbs(*evidence.FilePath) ||
				evidence.Key == nil || evidence.LineHint == nil || *evidence.LineHint < 1 ||
				evidence.ObservedValue == nil || evidence.RuleID == "" {
				t.Fatalf("incomplete evidence: %#v", evidence)
			}
		}
	}
}

func TestAnalyzeDynamicGradleValuesDoesNotInventVersions(t *testing.T) {
	projectRoot := repositoryFixturePath(t, "android_dynamic")
	result := New(OSFileSystem{}).Analyze(context.Background(), domain.Project{
		ID:   "project-dynamic",
		Path: projectRoot,
		Kind: domain.ProjectAndroid,
	}, Config{})

	expectedUnknown := []struct {
		ecosystem string
		component string
	}{
		{ecosystem: "android", component: "android_gradle_plugin"},
		{ecosystem: "android", component: "compile_sdk"},
		{ecosystem: "android", component: "ndk"},
	}
	for _, expected := range expectedUnknown {
		requirement := findRequirement(result.Requirements, expected.ecosystem, expected.component, "", domain.ConfidenceUnknown)
		if requirement == nil {
			t.Fatalf("missing unknown requirement for %s/%s: %#v", expected.ecosystem, expected.component, result.Requirements)
		}
		if requirement.VersionConstraint != nil {
			t.Fatalf("dynamic value must not become a version: %#v", requirement)
		}
		for _, evidence := range requirement.Evidence {
			if evidence.ObservedValue != nil &&
				(strings.Contains(*evidence.ObservedValue, "FIXTURE_NDK_VERSION") ||
					strings.Contains(*evidence.ObservedValue, "compileSdk") ||
					strings.Contains(*evidence.ObservedValue, "agpVersion")) {
				t.Fatalf("dynamic expression details should be minimized: %#v", evidence)
			}
		}
	}
	if len(result.Requirements) != len(expectedUnknown) {
		t.Fatalf("expected only 3 unknown requirements, got %#v", result.Requirements)
	}
}

func TestAnalyzeLegacyGradleAndFVMFormats(t *testing.T) {
	t.Run("legacy Gradle and Groovy DSL", func(t *testing.T) {
		root := t.TempDir()
		writeAnalyzerFixture(t, filepath.Join(root, "settings.gradle"), "rootProject.name = 'legacy'\n")
		writeAnalyzerFixture(t, filepath.Join(root, "build.gradle"), `
buildscript {
    dependencies {
        classpath 'com.android.tools.build:gradle:8.2.2'
        classpath 'org.jetbrains.kotlin:kotlin-gradle-plugin:1.9.22'
    }
}
plugins {
    id 'com.android.application' version '8.2.2' apply false
}
`)
		writeAnalyzerFixture(t, filepath.Join(root, "app", "build.gradle"), `
android {
    compileSdkVersion 34
    ndkVersion "26.3.11579264"
    defaultConfig {
        minSdkVersion 21
        targetSdkVersion 34
    }
    externalNativeBuild {
        cmake {
            version "3.22.1"
        }
    }
    compileOptions {
        sourceCompatibility JavaVersion.VERSION_17
    }
}
kotlin {
    jvmToolchain(17)
}
`)

		result := New(OSFileSystem{}).Analyze(context.Background(), domain.Project{
			ID: "project-legacy-gradle", Path: root, Kind: domain.ProjectAndroid,
		}, Config{})
		assertRequirement(t, result.Requirements, "android", "android_gradle_plugin", "8.2.2", domain.RequiredExplicitly)
		assertRequirement(t, result.Requirements, "kotlin", "kotlin_gradle_plugin", "1.9.22", domain.RequiredExplicitly)
		assertRequirement(t, result.Requirements, "android", "compile_sdk", "34", domain.RequiredExplicitly)
		assertRequirement(t, result.Requirements, "android", "min_sdk", "21", domain.RequiredExplicitly)
		assertRequirement(t, result.Requirements, "android", "target_sdk", "34", domain.RequiredExplicitly)
		assertRequirement(t, result.Requirements, "android", "ndk", "26.3.11579264", domain.RequiredExplicitly)
		assertRequirement(t, result.Requirements, "android", "cmake", "3.22.1", domain.RequiredExplicitly)
		assertRequirement(t, result.Requirements, "java", "jdk", "17", domain.RequiredExplicitly)
		assertRequirement(t, result.Requirements, "java", "jdk", "17", domain.ProbablyRequired)
	})

	t.Run("legacy FVM JSON", func(t *testing.T) {
		root := t.TempDir()
		writeAnalyzerFixture(t, filepath.Join(root, "pubspec.yaml"), "name: legacy_fvm\n")
		writeAnalyzerFixture(t, filepath.Join(root, ".fvm", "fvm_config.json"), "{\"flutterSdkVersion\":\"3.19.6\"}\n")
		result := New(OSFileSystem{}).Analyze(context.Background(), domain.Project{
			ID: "project-legacy-fvm", Path: root, Kind: domain.ProjectFlutter,
		}, Config{})
		assertRequirement(t, result.Requirements, "flutter", "flutter_sdk", "3.19.6", domain.RequiredExplicitly)
	})
}

func TestRecognizedGradleIndirectionIsOnlyProbable(t *testing.T) {
	root := t.TempDir()
	writeAnalyzerFixture(t, filepath.Join(root, "settings.gradle.kts"), "rootProject.name = \"indirect\"\n")
	writeAnalyzerFixture(t, filepath.Join(root, "app", "build.gradle.kts"), `
android {
    compileSdk = flutter.compileSdkVersion
    defaultConfig {
        minSdk = libs.versions.minSdk.get().toInt()
    }
}
java {
    toolchain.languageVersion.set(JavaLanguageVersion.of(libs.versions.java.get().toInt()))
}
kotlin {
    jvmToolchain(providers.gradleProperty("javaVersion").get().toInt())
}
`)
	result := New(OSFileSystem{}).Analyze(context.Background(), domain.Project{
		ID: "project-indirect", Path: root, Kind: domain.ProjectAndroid,
	}, Config{})
	if requirement := findRequirement(result.Requirements, "android", "compile_sdk", "", domain.ProbablyRequired); requirement == nil || requirement.VersionConstraint != nil {
		t.Fatalf("expected unresolved Flutter indirection, got %#v", requirement)
	}
	if requirement := findRequirement(result.Requirements, "android", "min_sdk", "", domain.ProbablyRequired); requirement == nil || requirement.VersionConstraint != nil {
		t.Fatalf("expected unresolved catalog indirection, got %#v", requirement)
	}
	if requirement := findRequirement(result.Requirements, "java", "jdk", "", domain.ProbablyRequired); requirement == nil || requirement.VersionConstraint != nil {
		t.Fatalf("expected unresolved Java catalog indirection, got %#v", requirement)
	}
	if requirement := findRequirement(result.Requirements, "java", "jdk", "", domain.ConfidenceUnknown); requirement == nil || requirement.VersionConstraint != nil {
		t.Fatalf("expected unresolved Kotlin toolchain expression, got %#v", requirement)
	}
}

func TestAnalyzeFVMFlavorVersions(t *testing.T) {
	root := t.TempDir()
	writeAnalyzerFixture(t, filepath.Join(root, "pubspec.yaml"), "name: flavored\n")
	writeAnalyzerFixture(t, filepath.Join(root, ".fvmrc"), `{
  "flutter": "3.35.0",
  "flavors": {
    "development": "beta",
    "production": "3.35.1"
  }
}
`)
	result := New(OSFileSystem{}).Analyze(context.Background(), domain.Project{
		ID: "project-flavors", Path: root, Kind: domain.ProjectFlutter,
	}, Config{})
	assertRequirement(t, result.Requirements, "flutter", "flutter_sdk", "3.35.0", domain.RequiredExplicitly)
	assertRequirement(t, result.Requirements, "flutter", "flutter_sdk", "beta", domain.RequiredExplicitly)
	assertRequirement(t, result.Requirements, "flutter", "flutter_sdk", "3.35.1", domain.RequiredExplicitly)
	if !hasAnalyzerDiagnostic(result.Diagnostics, "ANALYZER_MULTIPLE_EXPLICIT_REQUIREMENTS") {
		t.Fatalf("expected multiple-constraints diagnostic: %#v", result.Diagnostics)
	}
}

func TestUnsupportedVersionTokenIsNotExposed(t *testing.T) {
	root := t.TempDir()
	writeAnalyzerFixture(t, filepath.Join(root, "pubspec.yaml"), "name: guarded\n")
	writeAnalyzerFixture(t, filepath.Join(root, ".fvmrc"), "{\"flutter\":\"https://user:super-secret@example.invalid/sdk\"}\n")
	result := New(OSFileSystem{}).Analyze(context.Background(), domain.Project{
		ID: "project-guarded", Path: root, Kind: domain.ProjectFlutter,
	}, Config{})
	requirement := findRequirement(result.Requirements, "flutter", "flutter_sdk", "", domain.ConfidenceUnknown)
	if requirement == nil || requirement.VersionConstraint != nil {
		t.Fatalf("unsupported version must stay unknown: %#v", result.Requirements)
	}
	for _, evidence := range requirement.Evidence {
		if evidence.ObservedValue != nil && strings.Contains(*evidence.ObservedValue, "super-secret") {
			t.Fatalf("unsupported version leaked into evidence: %#v", evidence)
		}
	}
}

func TestMalformedInputsAreDiagnosedAndOtherEvidenceSurvives(t *testing.T) {
	root := t.TempDir()
	writeAnalyzerFixture(t, filepath.Join(root, ".fvmrc"), "{not-json\n")
	writeAnalyzerFixture(t, filepath.Join(root, "pubspec.yaml"), "name: malformed\nenvironment: [not, a, mapping]\n")
	writeAnalyzerFixture(t, filepath.Join(root, "android", "settings.gradle.kts"), `plugins {
    id("com.android.application") version "8.7.3" apply false
}
`)
	writeAnalyzerFixture(t, filepath.Join(root, "android", "gradle", "wrapper", "gradle-wrapper.properties"), "distributionUrl=https://example.invalid/custom.zip\n")

	result := New(OSFileSystem{}).Analyze(context.Background(), domain.Project{
		ID: "project-malformed", Path: root, Kind: domain.ProjectHybrid,
	}, Config{})
	assertRequirement(t, result.Requirements, "android", "android_gradle_plugin", "8.7.3", domain.RequiredExplicitly)
	for _, code := range []string{
		"ANALYZER_FVM_MALFORMED",
		"ANALYZER_GRADLE_DISTRIBUTION_UNSUPPORTED",
		"ANALYZER_PUBSPEC_ENVIRONMENT_UNSUPPORTED",
	} {
		if !hasAnalyzerDiagnostic(result.Diagnostics, code) {
			t.Errorf("missing diagnostic %s: %#v", code, result.Diagnostics)
		}
	}
}

func TestVersionCatalogUnresolvedReferenceStaysUnknown(t *testing.T) {
	root := t.TempDir()
	writeAnalyzerFixture(t, filepath.Join(root, "settings.gradle.kts"), "rootProject.name = \"catalog\"\n")
	writeAnalyzerFixture(t, filepath.Join(root, "build.gradle.kts"), "plugins { alias(libs.plugins.android.application) apply false }\n")
	writeAnalyzerFixture(t, filepath.Join(root, "gradle", "libs.versions.toml"), `[plugins]
android-application = { id = "com.android.application", version.ref = "missing" }
`)

	result := New(OSFileSystem{}).Analyze(context.Background(), domain.Project{
		ID: "project-catalog", Path: root, Kind: domain.ProjectAndroid,
	}, Config{})
	requirement := findRequirement(result.Requirements, "android", "android_gradle_plugin", "", domain.ConfidenceUnknown)
	if requirement == nil || requirement.VersionConstraint != nil {
		t.Fatalf("expected unknown catalog version, got %#v", requirement)
	}
	if !hasAnalyzerDiagnostic(result.Diagnostics, "ANALYZER_VERSION_CATALOG_REFERENCE_UNRESOLVED") {
		t.Fatalf("expected unresolved catalog diagnostic: %#v", result.Diagnostics)
	}
}

func TestAnalysisLimitsAndSymlinks(t *testing.T) {
	t.Run("file size", func(t *testing.T) {
		root := t.TempDir()
		writeAnalyzerFixture(t, filepath.Join(root, ".fvmrc"), "{\"flutter\":\"3.35.0\",\"padding\":\"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx\"}\n")
		writeAnalyzerFixture(t, filepath.Join(root, "pubspec.yaml"), "name: fixture\n")
		result := New(OSFileSystem{}).Analyze(context.Background(), domain.Project{
			ID: "project-size", Path: root, Kind: domain.ProjectFlutter,
		}, Config{Limits: Limits{MaxFileBytes: 20}})
		if !hasAnalyzerDiagnostic(result.Diagnostics, "ANALYZER_MAX_FILE_SIZE_REACHED") {
			t.Fatalf("expected file size diagnostic: %#v", result.Diagnostics)
		}
		if findRequirement(result.Requirements, "flutter", "flutter_sdk", "3.35.0", domain.RequiredExplicitly) != nil {
			t.Fatal("oversized file must not be parsed")
		}
	})

	t.Run("symlinked input", func(t *testing.T) {
		root := t.TempDir()
		external := filepath.Join(t.TempDir(), "external-fvmrc")
		writeAnalyzerFixture(t, external, "{\"flutter\":\"9.9.9\"}\n")
		writeAnalyzerFixture(t, filepath.Join(root, "pubspec.yaml"), "name: fixture\n")
		if err := os.Symlink(external, filepath.Join(root, ".fvmrc")); err != nil {
			t.Fatalf("create symlink: %v", err)
		}
		result := New(OSFileSystem{}).Analyze(context.Background(), domain.Project{
			ID: "project-symlink", Path: root, Kind: domain.ProjectFlutter,
		}, Config{})
		if !hasAnalyzerDiagnostic(result.Diagnostics, "ANALYZER_SYMLINK_SKIPPED") {
			t.Fatalf("expected symlink diagnostic: %#v", result.Diagnostics)
		}
		if findRequirement(result.Requirements, "flutter", "flutter_sdk", "9.9.9", domain.RequiredExplicitly) != nil {
			t.Fatal("symlink target must not be parsed")
		}
	})

	t.Run("symlinked legacy FVM directory", func(t *testing.T) {
		root := t.TempDir()
		externalDirectory := t.TempDir()
		writeAnalyzerFixture(t, filepath.Join(externalDirectory, "fvm_config.json"), "{\"flutterSdkVersion\":\"9.9.8\"}\n")
		writeAnalyzerFixture(t, filepath.Join(root, "pubspec.yaml"), "name: fixture\n")
		if err := os.Symlink(externalDirectory, filepath.Join(root, ".fvm")); err != nil {
			t.Fatalf("create legacy FVM symlink: %v", err)
		}
		result := New(OSFileSystem{}).Analyze(context.Background(), domain.Project{
			ID: "project-fvm-symlink", Path: root, Kind: domain.ProjectFlutter,
		}, Config{})
		if !hasAnalyzerDiagnostic(result.Diagnostics, "ANALYZER_SYMLINK_SKIPPED") {
			t.Fatalf("expected legacy FVM symlink diagnostic: %#v", result.Diagnostics)
		}
		if findRequirement(result.Requirements, "flutter", "flutter_sdk", "9.9.8", domain.RequiredExplicitly) != nil {
			t.Fatal("legacy FVM symlink target must not be parsed")
		}
	})

	t.Run("symlinked project root", func(t *testing.T) {
		realRoot := t.TempDir()
		parent := t.TempDir()
		linkedRoot := filepath.Join(parent, "linked-project")
		if err := os.Symlink(realRoot, linkedRoot); err != nil {
			t.Fatalf("create project symlink: %v", err)
		}
		result := New(OSFileSystem{}).Analyze(context.Background(), domain.Project{
			ID: "project-root-symlink", Path: linkedRoot, Kind: domain.ProjectAndroid,
		}, Config{})
		if !hasAnalyzerDiagnostic(result.Diagnostics, "ANALYZER_SYMLINK_ROOT_REJECTED") {
			t.Fatalf("expected root symlink rejection: %#v", result.Diagnostics)
		}
	})

	t.Run("entry and depth budgets", func(t *testing.T) {
		root := t.TempDir()
		writeAnalyzerFixture(t, filepath.Join(root, "settings.gradle.kts"), "rootProject.name = \"limited\"\n")
		writeAnalyzerFixture(t, filepath.Join(root, "one", "two", "three", "build.gradle.kts"), "android { compileSdk = 99 }\n")
		result := New(OSFileSystem{}).Analyze(context.Background(), domain.Project{
			ID: "project-budgets", Path: root, Kind: domain.ProjectAndroid,
		}, Config{Limits: Limits{MaxEntries: 3, MaxDepth: 2}})
		if !hasAnalyzerDiagnostic(result.Diagnostics, "ANALYZER_MAX_ENTRIES_REACHED") &&
			!hasAnalyzerDiagnostic(result.Diagnostics, "ANALYZER_MAX_DEPTH_REACHED") {
			t.Fatalf("expected a bounded walk diagnostic: %#v", result.Diagnostics)
		}
		if findRequirement(result.Requirements, "android", "compile_sdk", "99", domain.RequiredExplicitly) != nil {
			t.Fatal("file beyond the walk budget must not be parsed")
		}
	})
}

func TestAnalyzerReadsOnlyAllowlistedFilesAndKeepsPartialEvidence(t *testing.T) {
	root := t.TempDir()
	settingsPath := filepath.Join(root, "settings.gradle.kts")
	buildPath := filepath.Join(root, "build.gradle.kts")
	writeAnalyzerFixture(t, settingsPath, `plugins {
    id("com.android.application") version "8.6.1" apply false
}
`)
	writeAnalyzerFixture(t, buildPath, "android { compileSdk = 35 }\n")
	writeAnalyzerFixture(t, filepath.Join(root, "gradle.properties"), "secretToken=do-not-read\ncompileSdk=99\n")

	result := New(faultOpenFileSystem{failPath: buildPath}).Analyze(context.Background(), domain.Project{
		ID: "project-partial", Path: root, Kind: domain.ProjectAndroid,
	}, Config{})
	assertRequirement(t, result.Requirements, "android", "android_gradle_plugin", "8.6.1", domain.RequiredExplicitly)
	if !hasAnalyzerDiagnostic(result.Diagnostics, "ANALYZER_FILE_UNAVAILABLE") {
		t.Fatalf("expected failed file diagnostic: %#v", result.Diagnostics)
	}
	for _, requirement := range result.Requirements {
		for _, evidence := range requirement.Evidence {
			if evidence.ObservedValue != nil && strings.Contains(*evidence.ObservedValue, "do-not-read") {
				t.Fatal("non-allowlisted gradle.properties content leaked into evidence")
			}
		}
	}
}

func TestAnalysisCancellationAndInvalidAdapters(t *testing.T) {
	root := t.TempDir()
	writeAnalyzerFixture(t, filepath.Join(root, "settings.gradle.kts"), "rootProject.name = \"cancelled\"\n")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := New(OSFileSystem{}).Analyze(ctx, domain.Project{
		ID: "project-cancelled", Path: root, Kind: domain.ProjectAndroid,
	}, Config{})
	if !hasAnalyzerDiagnostic(result.Diagnostics, "ANALYZER_CANCELLED") {
		t.Fatalf("expected cancellation diagnostic: %#v", result.Diagnostics)
	}

	result = New(nil).Analyze(context.Background(), domain.Project{Path: root}, Config{})
	if !hasAnalyzerDiagnostic(result.Diagnostics, "ANALYZER_FILESYSTEM_UNAVAILABLE") {
		t.Fatalf("expected missing filesystem diagnostic: %#v", result.Diagnostics)
	}

	missing := filepath.Join(t.TempDir(), "missing")
	result = New(OSFileSystem{}).Analyze(context.Background(), domain.Project{Path: missing}, Config{})
	if !hasAnalyzerDiagnostic(result.Diagnostics, "ANALYZER_PROJECT_UNAVAILABLE") {
		t.Fatalf("expected missing project diagnostic: %#v", result.Diagnostics)
	}
}

func TestAnalyzerLeavesRepositoryFixturesUnchanged(t *testing.T) {
	fixtureRoot := repositoryFixturePath(t, "")
	before := snapshotAnalyzerFiles(t, fixtureRoot)

	analyzer := New(OSFileSystem{})
	projects := []domain.Project{
		{ID: "project-explicit", Path: filepath.Join(fixtureRoot, "flutter_explicit"), Kind: domain.ProjectHybrid},
		{ID: "project-dynamic", Path: filepath.Join(fixtureRoot, "android_dynamic"), Kind: domain.ProjectAndroid},
	}
	for index := 0; index < 10; index++ {
		for _, project := range projects {
			analyzer.Analyze(context.Background(), project, Config{})
		}
	}

	after := snapshotAnalyzerFiles(t, fixtureRoot)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("analysis changed fixture content or modification times")
	}
}

func TestAnalyzeProjectsAttachesPartialResults(t *testing.T) {
	explicitRoot := repositoryFixturePath(t, "flutter_explicit")
	missingRoot := filepath.Join(t.TempDir(), "missing")
	projects, diagnostics := New(OSFileSystem{}).AnalyzeProjects(context.Background(), []domain.Project{
		{ID: "a", Path: explicitRoot, Kind: domain.ProjectHybrid, Requirements: []domain.Requirement{}},
		{ID: "b", Path: missingRoot, Kind: domain.ProjectAndroid, Requirements: []domain.Requirement{}},
	}, Config{})
	if len(projects[0].Requirements) == 0 {
		t.Fatal("valid project requirements should survive another project's error")
	}
	if len(projects[1].Requirements) != 0 || !hasAnalyzerDiagnostic(diagnostics, "ANALYZER_PROJECT_UNAVAILABLE") {
		t.Fatalf("unexpected partial analysis result: %#v %#v", projects, diagnostics)
	}
}

func assertRequirement(
	t *testing.T,
	requirements []domain.Requirement,
	ecosystem string,
	component string,
	version string,
	confidence domain.Confidence,
) {
	t.Helper()
	if findRequirement(requirements, ecosystem, component, version, confidence) == nil {
		t.Fatalf("missing requirement %s/%s %s (%s): %#v", ecosystem, component, version, confidence, requirements)
	}
}

func findRequirement(
	requirements []domain.Requirement,
	ecosystem string,
	component string,
	version string,
	confidence domain.Confidence,
) *domain.Requirement {
	for index := range requirements {
		requirement := &requirements[index]
		if requirement.Ecosystem != ecosystem || requirement.Component != component || requirement.Confidence != confidence {
			continue
		}
		if version == "" && requirement.VersionConstraint == nil {
			return requirement
		}
		if requirement.VersionConstraint != nil && *requirement.VersionConstraint == version {
			return requirement
		}
	}
	return nil
}

func hasAnalyzerDiagnostic(diagnostics []domain.Diagnostic, code string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}

func repositoryFixturePath(t *testing.T, name string) string {
	t.Helper()
	root, err := filepath.Abs("../../testdata")
	if err != nil {
		t.Fatalf("resolve fixture root: %v", err)
	}
	if name != "" {
		return filepath.Join(root, name)
	}
	return root
}

func writeAnalyzerFixture(t *testing.T, filePath, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("create fixture directory: %v", err)
	}
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}

type analyzerFileSnapshot struct {
	hash       [sha256.Size]byte
	modifiedAt time.Time
}

type faultOpenFileSystem struct {
	OSFileSystem
	failPath string
}

func (fileSystem faultOpenFileSystem) Open(name string) (fs.File, error) {
	if name == fileSystem.failPath {
		return nil, fs.ErrPermission
	}
	return fileSystem.OSFileSystem.Open(name)
}

func snapshotAnalyzerFiles(t *testing.T, root string) map[string]analyzerFileSnapshot {
	t.Helper()
	snapshot := make(map[string]analyzerFileSnapshot)
	err := filepath.WalkDir(root, func(currentPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		content, err := os.ReadFile(currentPath)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		snapshot[currentPath] = analyzerFileSnapshot{
			hash:       sha256.Sum256(content),
			modifiedAt: info.ModTime(),
		}
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot fixtures: %v", err)
	}
	return snapshot
}
