package autodetect

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dev-environment-auditor/internal/domain"
)

type fakeEnvironment struct {
	home        string
	workingDir  string
	variables   map[string]string
	executables map[string]string
}

func (environment fakeEnvironment) UserHomeDir() (string, error) {
	return environment.home, nil
}

func (environment fakeEnvironment) Getwd() (string, error) {
	return environment.workingDir, nil
}

func (environment fakeEnvironment) LookupEnv(key string) (string, bool) {
	value, ok := environment.variables[key]
	return value, ok
}

func (environment fakeEnvironment) LookPath(file string) (string, error) {
	if value, ok := environment.executables[file]; ok {
		return value, nil
	}
	return "", os.ErrNotExist
}

func TestDetectUsesEnvironmentPathAndConventionalLocations(t *testing.T) {
	home := t.TempDir()
	project := filepath.Join(home, "Documents", "projects", "mobile")
	createFlutterProject(t, project)
	android := filepath.Join(home, "custom", "android")
	createAndroidSDK(t, android)
	flutter := filepath.Join(home, "custom", "flutter-sdk")
	createFlutterSDK(t, flutter)
	gradle := filepath.Join(home, ".gradle")
	createDirectory(t, filepath.Join(gradle, "wrapper", "dists"))
	jdk := filepath.Join(home, "custom", "jdk-21")
	createJDK(t, jdk)
	fvm := filepath.Join(home, "custom", "fvm")
	createFlutterSDK(t, filepath.Join(fvm, "3.35.0"))

	detector := NewWithAdapters(osFileSystem{}, fakeEnvironment{
		home:       home,
		workingDir: filepath.Join(project, "android"),
		variables: map[string]string{
			"ANDROID_HOME":     android,
			"FVM_CACHE_PATH":   fvm,
			"GRADLE_USER_HOME": gradle,
			"JAVA_HOME":        jdk,
		},
		executables: map[string]string{
			"flutter": filepath.Join(flutter, "bin", "flutter"),
			"java":    filepath.Join(jdk, "bin", "java"),
			"javac":   filepath.Join(jdk, "bin", "javac"),
		},
	}, "test")

	result := detector.Detect(context.Background(), Config{
		NeedProjectRoots: true,
		NeedAndroid:      true,
		NeedFlutter:      true,
		NeedFVM:          true,
		NeedGradle:       true,
		NeedJDK:          true,
		DeepSearch:       false,
	})

	assertPaths(t, result.Roots.ProjectRoots, project)
	assertPaths(t, result.Roots.AndroidSDKRoots, android)
	assertPaths(t, result.Roots.FlutterSDKRoots, flutter)
	assertPaths(t, result.Roots.FVMCacheRoots, fvm)
	assertPaths(t, result.Roots.GradleUserHomeRoots, gradle)
	assertPaths(t, result.Roots.JDKRoots, jdk)
	if !result.ProjectDiscoveryHeuristic {
		t.Fatal("automatically found project roots must remain heuristic")
	}
	assertFamilies(t, result.HeuristicInventoryFamilies, FamilyAndroid, FamilyFlutter, FamilyGradle, FamilyJava)
	if countDiagnostic(result.Diagnostics, "AUTODETECT_ROOT_FOUND") != 6 {
		t.Fatalf("unexpected diagnostics: %#v", result.Diagnostics)
	}
}

func TestDetectAndroidAVDUsesOfficialEnvironmentAndConventionOrder(t *testing.T) {
	home := t.TempDir()
	environmentRoot := filepath.Join(home, "custom-avds")
	createAndroidAVD(t, filepath.Join(environmentRoot, "Pixel_9_API_35.avd"))
	conventionalRoot := filepath.Join(home, ".android", "avd")
	createAndroidAVD(t, filepath.Join(conventionalRoot, "Pixel_8_API_34.avd"))

	detector := NewWithAdapters(osFileSystem{}, fakeEnvironment{
		home:       home,
		workingDir: home,
		variables: map[string]string{
			"ANDROID_AVD_HOME": environmentRoot,
		},
		executables: map[string]string{},
	}, "test")
	result := detector.Detect(context.Background(), Config{NeedAndroidAVD: true, DeepSearch: false})

	assertPaths(t, result.Roots.AndroidAVDRoots, conventionalRoot, environmentRoot)
	assertFamilies(t, result.HeuristicInventoryFamilies, FamilyAndroidAVD)
	if countDiagnostic(result.Diagnostics, "AUTODETECT_ROOT_FOUND") != 2 {
		t.Fatalf("unexpected AVD detection diagnostics: %#v", result.Diagnostics)
	}
}

func TestDetectAndroidAVDDeepSearchFindsNonstandardRoot(t *testing.T) {
	home := t.TempDir()
	avdRoot := filepath.Join(home, "Documents", "emulators", "profiles")
	createAndroidAVD(t, filepath.Join(avdRoot, "Tablet_API_35.avd"))
	detector := NewWithAdapters(osFileSystem{}, fakeEnvironment{
		home:        home,
		workingDir:  home,
		variables:   map[string]string{},
		executables: map[string]string{},
	}, "test")

	result := detector.Detect(context.Background(), Config{NeedAndroidAVD: true, DeepSearch: true})
	assertPaths(t, result.Roots.AndroidAVDRoots, avdRoot)
}

func TestDetectDeepSearchFindsNonstandardRootsAndFiltersFlutterSDKProject(t *testing.T) {
	home := t.TempDir()
	project := filepath.Join(home, "Documents", "clients", "nested", "mobile-app")
	createFlutterProject(t, project)
	flutter := filepath.Join(home, "Documents", "toolchains", "unexpected", "flutter")
	createFlutterSDK(t, flutter)
	writeFile(t, filepath.Join(flutter, "pubspec.yaml"), "name: flutter\n")
	writeFile(t, filepath.Join(flutter, ".metadata"), "version: fixture\n")
	android := filepath.Join(home, "Documents", "toolchains", "android-custom")
	createAndroidSDK(t, android)
	jdk := filepath.Join(home, "Documents", "toolchains", "jdks", "temurin-21")
	createJDK(t, jdk)

	detector := NewWithAdapters(osFileSystem{}, fakeEnvironment{
		home:        home,
		workingDir:  home,
		variables:   map[string]string{},
		executables: map[string]string{},
	}, "test")
	result := detector.Detect(context.Background(), Config{
		NeedProjectRoots: true,
		NeedAndroid:      true,
		NeedFlutter:      true,
		NeedJDK:          true,
		DeepSearch:       true,
	})

	assertPaths(t, result.Roots.ProjectRoots, project)
	assertPaths(t, result.Roots.AndroidSDKRoots, android)
	assertPaths(t, result.Roots.FlutterSDKRoots, flutter)
	assertPaths(t, result.Roots.JDKRoots, jdk)
	for _, root := range result.Roots.ProjectRoots {
		if pathWithin(flutter, root) {
			t.Fatalf("Flutter SDK was incorrectly retained as a project: %s", root)
		}
	}
}

func TestDetectDeepSearchFindsDockerOnlyProject(t *testing.T) {
	home := t.TempDir()
	project := filepath.Join(home, "Documents", "clients", "docker-stack")
	writeFile(t, filepath.Join(project, "compose.yml"), "services:\n  db:\n    image: postgres:16\n")
	detector := NewWithAdapters(osFileSystem{}, fakeEnvironment{
		home:        home,
		workingDir:  home,
		variables:   map[string]string{},
		executables: map[string]string{},
	}, "test")

	result := detector.Detect(context.Background(), Config{NeedProjectRoots: true, DeepSearch: true})
	assertPaths(t, result.Roots.ProjectRoots, project)
}

func TestDetectDeepSearchIsBounded(t *testing.T) {
	home := t.TempDir()
	for index := range 20 {
		createDirectory(t, filepath.Join(home, "Documents", "tree", strings.Repeat("x", index+1)))
	}
	detector := NewWithAdapters(osFileSystem{}, fakeEnvironment{
		home:        home,
		workingDir:  home,
		variables:   map[string]string{},
		executables: map[string]string{},
	}, "test")

	result := detector.Detect(context.Background(), Config{
		NeedProjectRoots: true,
		NeedAndroid:      true,
		DeepSearch:       true,
		Limits:           Limits{MaxEntries: 3, MaxDepth: 10},
	})

	if countDiagnostic(result.Diagnostics, "AUTODETECT_MAX_ENTRIES_REACHED") != 1 {
		t.Fatalf("expected bounded-search diagnostic, got %#v", result.Diagnostics)
	}
	if countDiagnostic(result.Diagnostics, "AUTODETECT_PROJECT_FALLBACK") != 1 {
		t.Fatalf("expected working-directory fallback, got %#v", result.Diagnostics)
	}
	if len(result.Roots.ProjectRoots) != 1 || result.Roots.ProjectRoots[0] != home {
		t.Fatalf("unexpected fallback roots: %#v", result.Roots.ProjectRoots)
	}
}

func TestDetectDeepSearchRejectsGenericPlatformAndCMakeDirectories(t *testing.T) {
	home := t.TempDir()
	createDirectory(t, filepath.Join(home, "Documents", "xcode", "Developer", "Platforms"))
	createDirectory(t, filepath.Join(home, "Documents", "server", "lib", "cmake"))
	detector := NewWithAdapters(osFileSystem{}, fakeEnvironment{
		home:        home,
		workingDir:  home,
		variables:   map[string]string{},
		executables: map[string]string{},
	}, "test")

	result := detector.Detect(context.Background(), Config{
		NeedAndroid: true,
		DeepSearch:  true,
	})

	if len(result.Roots.AndroidSDKRoots) != 0 {
		t.Fatalf("generic directories were mistaken for Android SDKs: %#v", result.Roots.AndroidSDKRoots)
	}
	if countDiagnostic(result.Diagnostics, "AUTODETECT_FAMILY_NOT_FOUND") != 1 {
		t.Fatalf("missing-family diagnostic expected: %#v", result.Diagnostics)
	}
}

func TestDetectDeepSearchContinuesAfterPerRootBudget(t *testing.T) {
	home := t.TempDir()
	for index := range 12 {
		createDirectory(t, filepath.Join(home, "Documents", "aaa-large", strings.Repeat("x", index+1)))
	}
	project := filepath.Join(home, "Documents", "zzz-client", "mobile")
	createFlutterProject(t, project)
	detector := NewWithAdapters(osFileSystem{}, fakeEnvironment{
		home:        home,
		workingDir:  home,
		variables:   map[string]string{},
		executables: map[string]string{},
	}, "test")

	result := detector.Detect(context.Background(), Config{
		NeedProjectRoots: true,
		DeepSearch:       true,
		Limits: Limits{
			MaxEntries:        100,
			MaxEntriesPerRoot: 6,
			MaxDepth:          10,
		},
	})

	assertPaths(t, result.Roots.ProjectRoots, project)
	if countDiagnostic(result.Diagnostics, "AUTODETECT_MAX_ENTRIES_PER_ROOT_REACHED") == 0 {
		t.Fatalf("expected a per-root budget diagnostic: %#v", result.Diagnostics)
	}
}

func createFlutterProject(t *testing.T, root string) {
	t.Helper()
	createDirectory(t, filepath.Join(root, "android"))
	writeFile(t, filepath.Join(root, "pubspec.yaml"), "name: fixture\n")
}

func createFlutterSDK(t *testing.T, root string) {
	t.Helper()
	createDirectory(t, filepath.Join(root, "packages", "flutter"))
	writeFile(t, filepath.Join(root, "bin", "flutter"), "#!/bin/sh\n")
}

func createAndroidSDK(t *testing.T, root string) {
	t.Helper()
	createDirectory(t, filepath.Join(root, "platforms"))
	createDirectory(t, filepath.Join(root, "build-tools"))
}

func createAndroidAVD(t *testing.T, root string) {
	t.Helper()
	writeFile(t, filepath.Join(root, "config.ini"), "target=android-35\n")
}

func createJDK(t *testing.T, root string) {
	t.Helper()
	writeFile(t, filepath.Join(root, "release"), "JAVA_VERSION=\"21\"\n")
	writeFile(t, filepath.Join(root, "bin", "java"), "fixture\n")
	writeFile(t, filepath.Join(root, "bin", "javac"), "fixture\n")
}

func createDirectory(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("create directory %s: %v", path, err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	createDirectory(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write file %s: %v", path, err)
	}
}

func assertPaths(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("paths = %#v, want %#v", got, want)
	}
	for index := range want {
		expected := want[index]
		if resolved, err := filepath.EvalSymlinks(expected); err == nil {
			expected = resolved
		}
		if got[index] != expected {
			t.Fatalf("paths = %#v, want %#v", got, want)
		}
	}
}

func assertFamilies(t *testing.T, got []Family, want ...Family) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("families = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("families = %#v, want %#v", got, want)
		}
	}
}

func countDiagnostic(diagnostics []domain.Diagnostic, code string) int {
	count := 0
	for _, value := range diagnostics {
		if value.Code == code {
			count++
		}
	}
	return count
}
