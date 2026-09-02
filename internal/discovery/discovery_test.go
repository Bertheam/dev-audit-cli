package discovery

import (
	"context"
	"crypto/sha256"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"dev-environment-auditor/internal/domain"
)

func TestDiscoverFlutterAndAndroidProjects(t *testing.T) {
	root := t.TempDir()
	flutterRoot := filepath.Join(root, "a_flutter")
	androidRoot := filepath.Join(root, "b_android")
	gradleOnlyRoot := filepath.Join(root, "c_gradle_only")

	createFlutterProject(t, flutterRoot, true)
	createAndroidProject(t, androidRoot)
	writeFixture(t, filepath.Join(gradleOnlyRoot, "settings.gradle.kts"), "rootProject.name = \"not-proven-android\"\n")

	discoverer := New(OSFileSystem{})
	first := discoverer.Discover(context.Background(), Config{Roots: []string{root}})
	second := discoverer.Discover(context.Background(), Config{Roots: []string{root}})

	if len(first.Projects) != 2 {
		t.Fatalf("expected 2 projects, got %d: %#v", len(first.Projects), first.Projects)
	}
	if first.Projects[0].Path != flutterRoot || first.Projects[0].Kind != domain.ProjectHybrid {
		t.Fatalf("expected hybrid Flutter project first, got %#v", first.Projects[0])
	}
	if first.Projects[1].Path != androidRoot || first.Projects[1].Kind != domain.ProjectAndroid {
		t.Fatalf("expected standalone Android project second, got %#v", first.Projects[1])
	}
	if first.Projects[0].ID != second.Projects[0].ID || first.Projects[1].ID != second.Projects[1].ID {
		t.Fatal("expected stable project identifiers across identical scans")
	}
	if containsProject(first.Projects, filepath.Join(flutterRoot, "android")) {
		t.Fatal("nested Flutter android directory must not be reported as a separate project")
	}
	if !hasDiagnostic(first.Diagnostics, "DISCOVERY_ANDROID_CANDIDATE_INCOMPLETE") {
		t.Fatal("expected an incomplete Gradle-only candidate diagnostic")
	}
	if first.Stats.ProjectsFound != 2 || first.Stats.RootsScanned != 1 {
		t.Fatalf("unexpected stats: %#v", first.Stats)
	}
}

func TestDiscoverHonorsDefaultAndConfiguredExclusions(t *testing.T) {
	root := t.TempDir()
	includedRoot := filepath.Join(root, "included")
	excludedRoot := filepath.Join(root, "archive", "old_app")
	defaultExcludedRoot := filepath.Join(root, "build", "generated_app")

	createFlutterProject(t, includedRoot, false)
	createFlutterProject(t, excludedRoot, false)
	createFlutterProject(t, defaultExcludedRoot, false)

	result := New(OSFileSystem{}).Discover(context.Background(), Config{
		Roots:      []string{root, includedRoot},
		Exclusions: []string{"archive/**"},
	})

	if len(result.Projects) != 1 || result.Projects[0].Path != includedRoot {
		t.Fatalf("expected only included project, got %#v", result.Projects)
	}
	if !hasDiagnostic(result.Diagnostics, "DISCOVERY_ROOT_ALREADY_COVERED") {
		t.Fatal("expected nested explicit root to be reported as already covered")
	}
}

func TestDiscoverDoesNotFollowSymlinks(t *testing.T) {
	root := t.TempDir()
	externalRoot := t.TempDir()
	externalProject := filepath.Join(externalRoot, "external_flutter")
	createFlutterProject(t, externalProject, false)

	symlinkPath := filepath.Join(root, "linked_project")
	if err := os.Symlink(externalProject, symlinkPath); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	result := New(OSFileSystem{}).Discover(context.Background(), Config{Roots: []string{root}})
	if len(result.Projects) != 0 {
		t.Fatalf("expected no project through symlink, got %#v", result.Projects)
	}
	if !hasDiagnostic(result.Diagnostics, "DISCOVERY_SYMLINK_SKIPPED") {
		t.Fatal("expected skipped symlink diagnostic")
	}
}

func TestDiscoverRejectsSymlinkRoot(t *testing.T) {
	realRoot := t.TempDir()
	symlinkParent := t.TempDir()
	symlinkRoot := filepath.Join(symlinkParent, "root_link")
	if err := os.Symlink(realRoot, symlinkRoot); err != nil {
		t.Fatalf("create symlink root: %v", err)
	}

	result := New(OSFileSystem{}).Discover(context.Background(), Config{Roots: []string{symlinkRoot}})
	if len(result.Roots) != 0 || result.Stats.RootsScanned != 0 {
		t.Fatalf("symlink root must not be scanned: %#v", result)
	}
	if !hasDiagnostic(result.Diagnostics, "DISCOVERY_SYMLINK_ROOT_REJECTED") {
		t.Fatal("expected rejected symlink root diagnostic")
	}
}

func TestDiscoverEnforcesEntryBudget(t *testing.T) {
	root := t.TempDir()
	createFlutterProject(t, filepath.Join(root, "app"), false)

	result := New(OSFileSystem{}).Discover(context.Background(), Config{
		Roots:  []string{root},
		Limits: Limits{MaxEntries: 2, MaxDepth: DefaultMaxDepth},
	})

	if !hasDiagnostic(result.Diagnostics, "DISCOVERY_MAX_ENTRIES_REACHED") {
		t.Fatal("expected max entries diagnostic")
	}
	if result.Stats.EntriesVisited != 3 {
		t.Fatalf("expected visit count to include stopping entry, got %d", result.Stats.EntriesVisited)
	}
}

func TestDiscoverEnforcesDepthBudget(t *testing.T) {
	root := t.TempDir()
	deepProject := filepath.Join(root, "one", "two", "app")
	createFlutterProject(t, deepProject, false)

	result := New(OSFileSystem{}).Discover(context.Background(), Config{
		Roots:  []string{root},
		Limits: Limits{MaxEntries: DefaultMaxEntries, MaxDepth: 2},
	})

	if containsProject(result.Projects, deepProject) {
		t.Fatal("project below max depth must not be discovered")
	}
	if !hasDiagnostic(result.Diagnostics, "DISCOVERY_MAX_DEPTH_REACHED") {
		t.Fatal("expected max depth diagnostic")
	}
}

func TestDiscoverReportsUnavailableRootsAndFiles(t *testing.T) {
	t.Run("no roots", func(t *testing.T) {
		result := New(OSFileSystem{}).Discover(context.Background(), Config{})
		if !hasDiagnostic(result.Diagnostics, "DISCOVERY_NO_ROOTS") {
			t.Fatal("expected explicit roots requirement diagnostic")
		}
	})

	t.Run("missing root", func(t *testing.T) {
		missingRoot := filepath.Join(t.TempDir(), "missing")
		result := New(OSFileSystem{}).Discover(context.Background(), Config{Roots: []string{missingRoot}})
		if !hasDiagnostic(result.Diagnostics, "DISCOVERY_ROOT_UNAVAILABLE") {
			t.Fatal("expected missing root diagnostic")
		}
	})

	t.Run("file root", func(t *testing.T) {
		rootFile := filepath.Join(t.TempDir(), "not-a-directory")
		writeFixture(t, rootFile, "fixture\n")
		result := New(OSFileSystem{}).Discover(context.Background(), Config{Roots: []string{rootFile}})
		if !hasDiagnostic(result.Diagnostics, "DISCOVERY_ROOT_NOT_DIRECTORY") {
			t.Fatal("expected file root diagnostic")
		}
	})

	for name, injectedError := range map[string]error{
		"permission denied": fs.ErrPermission,
		"path disappeared":  fs.ErrNotExist,
	} {
		t.Run(name, func(t *testing.T) {
			fileSystem := &faultFileSystem{walkError: injectedError}
			result := New(fileSystem).Discover(context.Background(), Config{Roots: []string{"/virtual/root"}})
			if !hasDiagnostic(result.Diagnostics, "DISCOVERY_ENTRY_UNAVAILABLE") {
				t.Fatalf("expected entry diagnostic for %v", injectedError)
			}
		})
	}
}

func TestDiscoverRejectsInvalidExclusionAndCancelledContext(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result := New(OSFileSystem{}).Discover(ctx, Config{
		Roots:      []string{root},
		Exclusions: []string{"[invalid"},
	})

	if !hasDiagnostic(result.Diagnostics, "DISCOVERY_INVALID_EXCLUSION") {
		t.Fatal("expected invalid exclusion diagnostic")
	}
	if !hasDiagnostic(result.Diagnostics, "DISCOVERY_CANCELLED") {
		t.Fatal("expected cancellation diagnostic")
	}
}

func TestDiscoveryLeavesRepositoryFixturesUnchanged(t *testing.T) {
	fixtureRoot, err := filepath.Abs("../../testdata")
	if err != nil {
		t.Fatalf("resolve fixture root: %v", err)
	}
	before := snapshotFiles(t, fixtureRoot)

	result := New(OSFileSystem{}).Discover(context.Background(), Config{Roots: []string{fixtureRoot}})
	if len(result.Projects) != 2 {
		t.Fatalf("expected 2 repository fixtures, got %d: %#v", len(result.Projects), result.Projects)
	}

	after := snapshotFiles(t, fixtureRoot)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("discovery changed fixture content or modification times")
	}
}

func TestNilFilesystemIsDiagnosed(t *testing.T) {
	result := New(nil).Discover(context.Background(), Config{Roots: []string{"/unused"}})
	if !hasDiagnostic(result.Diagnostics, "DISCOVERY_FILESYSTEM_UNAVAILABLE") {
		t.Fatal("expected missing filesystem adapter diagnostic")
	}
}

func createFlutterProject(t *testing.T, projectRoot string, withAndroid bool) {
	t.Helper()
	writeFixture(t, filepath.Join(projectRoot, "pubspec.yaml"), "name: fixture\n")
	writeFixture(t, filepath.Join(projectRoot, ".metadata"), "version: synthetic\n")
	if withAndroid {
		writeFixture(t, filepath.Join(projectRoot, "android", "settings.gradle.kts"), "rootProject.name = \"fixture\"\n")
		writeFixture(t, filepath.Join(projectRoot, "android", "gradle", "wrapper", "gradle-wrapper.properties"), "distributionUrl=fixture\n")
	}
}

func createAndroidProject(t *testing.T, projectRoot string) {
	t.Helper()
	writeFixture(t, filepath.Join(projectRoot, "settings.gradle.kts"), "rootProject.name = \"fixture\"\n")
	writeFixture(t, filepath.Join(projectRoot, "app", "src", "main", "AndroidManifest.xml"), "<manifest />\n")
}

func writeFixture(t *testing.T, filePath, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("create fixture directory: %v", err)
	}
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}

func containsProject(projects []domain.Project, projectPath string) bool {
	for _, project := range projects {
		if project.Path == projectPath {
			return true
		}
	}
	return false
}

func hasDiagnostic(diagnostics []domain.Diagnostic, code string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}

type fileSnapshot struct {
	hash       [sha256.Size]byte
	modifiedAt time.Time
}

func snapshotFiles(t *testing.T, root string) map[string]fileSnapshot {
	t.Helper()
	snapshot := make(map[string]fileSnapshot)
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
		snapshot[currentPath] = fileSnapshot{
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

type faultFileSystem struct {
	walkError error
}

func (fileSystem *faultFileSystem) Lstat(string) (fs.FileInfo, error) {
	return fakeDirectoryInfo{name: "root"}, nil
}

func (fileSystem *faultFileSystem) WalkDir(root string, fn fs.WalkDirFunc) error {
	rootEntry := fs.FileInfoToDirEntry(fakeDirectoryInfo{name: filepath.Base(root)})
	if err := fn(root, rootEntry, nil); err != nil {
		return err
	}
	return fn(filepath.Join(root, "unavailable"), nil, fileSystem.walkError)
}

type fakeDirectoryInfo struct {
	name string
}

func (info fakeDirectoryInfo) Name() string  { return info.name }
func (fakeDirectoryInfo) Size() int64        { return 0 }
func (fakeDirectoryInfo) Mode() fs.FileMode  { return fs.ModeDir | 0o755 }
func (fakeDirectoryInfo) ModTime() time.Time { return time.Unix(0, 0) }
func (fakeDirectoryInfo) IsDir() bool        { return true }
func (fakeDirectoryInfo) Sys() any           { return nil }

func TestDiagnosticMessagesDoNotEchoInjectedErrorsForKnownCases(t *testing.T) {
	knownCases := []struct {
		injectedError error
		expected      string
	}{
		{injectedError: fs.ErrPermission, expected: "message: permission denied"},
		{injectedError: fs.ErrNotExist, expected: "message: path does not exist"},
	}
	for _, testCase := range knownCases {
		diagnostic := fileErrorDiagnostic("CODE", "message", "/safe/path", testCase.injectedError)
		if diagnostic.Message != testCase.expected {
			t.Fatalf("expected stable message %q, got %q", testCase.expected, diagnostic.Message)
		}
	}

	unknown := errors.New("synthetic unknown failure")
	diagnostic := fileErrorDiagnostic("CODE", "message", "/safe/path", unknown)
	if !strings.Contains(diagnostic.Message, unknown.Error()) {
		t.Fatalf("unexpected error should remain diagnosable, got %q", diagnostic.Message)
	}
}
