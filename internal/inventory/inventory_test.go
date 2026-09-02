package inventory

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

func TestInspectExplicitAndroidFlutterFVMAndJDKRoots(t *testing.T) {
	fixture := createInventoryFixture(t)
	config := Config{
		AndroidSDKRoots:     []string{fixture.androidRoot},
		FlutterSDKRoots:     []string{fixture.flutterRoot},
		FVMCacheRoots:       []string{fixture.fvmRoot},
		GradleUserHomeRoots: []string{fixture.gradleRoot},
		JDKRoots:            []string{fixture.jdkRoot},
	}
	inventory := New(OSFileSystem{})

	first := inventory.Inspect(context.Background(), config)
	second := inventory.Inspect(context.Background(), config)

	expected := []struct {
		ecosystem string
		component string
		version   string
	}{
		{ecosystem: "android", component: "android_build_tools", version: "35.0.0"},
		{ecosystem: "android", component: "android_sdk_platform", version: "35"},
		{ecosystem: "android", component: "cmake", version: "3.22.1"},
		{ecosystem: "android", component: "ndk", version: "27.0.12077973"},
		{ecosystem: "flutter", component: "flutter_sdk", version: "3.19.6"},
		{ecosystem: "flutter", component: "flutter_sdk", version: "3.35.0"},
		{ecosystem: "gradle", component: "gradle", version: "8.12"},
		{ecosystem: "java", component: "jdk", version: "17.0.12"},
		{ecosystem: "kotlin", component: "kotlin_gradle_plugin", version: "2.1.0"},
		{ecosystem: "android", component: "android_gradle_plugin", version: "8.7.3"},
	}
	if len(first.Resources) != len(expected) {
		t.Fatalf("expected %d resources, got %d: %#v", len(expected), len(first.Resources), first.Resources)
	}
	for _, expectedResource := range expected {
		resource := findResource(first.Resources, expectedResource.ecosystem, expectedResource.component, expectedResource.version)
		if resource == nil {
			t.Errorf("missing %s/%s %s: %#v", expectedResource.ecosystem, expectedResource.component, expectedResource.version, first.Resources)
			continue
		}
		if resource.SizeBytes == nil || *resource.SizeBytes != logicalRegularSize(t, resource.Path) {
			t.Errorf("unexpected logical size for %#v", resource)
		}
		if resource.ReferenceStatus != domain.ReferenceUnknown {
			t.Errorf("Lot 3 must not infer references: %#v", resource)
		}
		if !hasMetadata(resource.Metadata, "size_metric", "logical_regular_file_bytes") ||
			!hasMetadata(resource.Metadata, "size_status", "complete") {
			t.Errorf("missing size semantics: %#v", resource.Metadata)
		}
	}
	if len(first.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", first.Diagnostics)
	}
	if first.Stats.RootsScanned != 5 || first.Stats.ResourcesFound != len(expected) {
		t.Fatalf("unexpected inventory stats: %#v", first.Stats)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("identical inventories must be deterministic")
	}

	flutter := findResource(first.Resources, "flutter", "flutter_sdk", "3.35.0")
	if flutter == nil || !hasMetadata(flutter.Metadata, "channel", "stable") ||
		!hasMetadata(flutter.Metadata, "version_source", "bin/cache/flutter.version.json:frameworkVersion") ||
		!hasMetadata(flutter.Metadata, "dart_sdk_version", "3.9.0") {
		t.Fatalf("unexpected Flutter metadata: %#v", flutter)
	}
	fvmFlutter := findResource(first.Resources, "flutter", "flutter_sdk", "3.19.6")
	if fvmFlutter == nil || !hasMetadata(fvmFlutter.Metadata, "dart_sdk_version", "3.3.4") {
		t.Fatalf("expected legacy bundled Dart metadata: %#v", fvmFlutter)
	}
	jdk := findResource(first.Resources, "java", "jdk", "17.0.12")
	if jdk == nil || filepath.Base(jdk.Path) != "Home" {
		t.Fatalf("expected JDK JAVA_HOME path, got %#v", jdk)
	}
}

func TestInventoryLeavesFilesUnchanged(t *testing.T) {
	fixture := createInventoryFixture(t)
	before := snapshotInventoryFiles(t, fixture.root)
	config := Config{
		AndroidSDKRoots:     []string{fixture.androidRoot},
		FlutterSDKRoots:     []string{fixture.flutterRoot},
		FVMCacheRoots:       []string{fixture.fvmRoot},
		GradleUserHomeRoots: []string{fixture.gradleRoot},
		JDKRoots:            []string{fixture.jdkRoot},
	}
	for index := 0; index < 10; index++ {
		New(OSFileSystem{}).Inspect(context.Background(), config)
	}
	after := snapshotInventoryFiles(t, fixture.root)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("inventory changed content or modification times")
	}
}

func TestInventoryFallbacksAndMalformedMetadata(t *testing.T) {
	base := t.TempDir()
	androidRoot := filepath.Join(base, "android")
	writeInventoryFixture(t, filepath.Join(androidRoot, "build-tools", "34.0.0", "source.properties"), "Pkg.Revision=https://user:android-secret@example.invalid\n")

	flutterRoot := filepath.Join(base, "flutter")
	createFlutterSDK(t, flutterRoot)
	writeInventoryFixture(t, filepath.Join(flutterRoot, "bin", "cache", "flutter.version.json"), "{malformed\n")
	writeInventoryFixture(t, filepath.Join(flutterRoot, "version"), "3.24.5\n")

	jdkRoot := filepath.Join(base, "jdks")
	jdkHome := filepath.Join(jdkRoot, "unsafe.jdk", "Contents", "Home")
	writeInventoryFixture(t, filepath.Join(jdkHome, "release"), "JAVA_VERSION=\"https://user:jdk-secret@example.invalid\"\n")

	result := New(OSFileSystem{}).Inspect(context.Background(), Config{
		AndroidSDKRoots: []string{androidRoot},
		FlutterSDKRoots: []string{flutterRoot},
		JDKRoots:        []string{jdkRoot},
	})
	if findResource(result.Resources, "android", "android_build_tools", "34.0.0") == nil {
		t.Fatalf("expected safe Android directory-name fallback: %#v", result.Resources)
	}
	if findResource(result.Resources, "flutter", "flutter_sdk", "3.24.5") == nil {
		t.Fatalf("expected legacy Flutter version fallback: %#v", result.Resources)
	}
	jdk := findResource(result.Resources, "java", "jdk", "")
	if jdk == nil || jdk.Version != nil {
		t.Fatalf("unsafe JDK version must remain unknown: %#v", result.Resources)
	}
	for _, resource := range result.Resources {
		if resource.Version != nil && strings.Contains(*resource.Version, "secret") {
			t.Fatalf("unsafe metadata leaked into version: %#v", resource)
		}
		for _, metadata := range resource.Metadata {
			if strings.Contains(metadata.Value, "secret") {
				t.Fatalf("unsafe metadata leaked into report: %#v", metadata)
			}
		}
	}
	if !hasInventoryDiagnostic(result.Diagnostics, "INVENTORY_FLUTTER_VERSION_JSON_MALFORMED") ||
		!hasInventoryDiagnostic(result.Diagnostics, "INVENTORY_JDK_VERSION_INVALID") {
		t.Fatalf("expected malformed metadata diagnostics: %#v", result.Diagnostics)
	}
}

func TestInventoryDoesNotFollowSymlinks(t *testing.T) {
	t.Run("root", func(t *testing.T) {
		realRoot := t.TempDir()
		linkedRoot := filepath.Join(t.TempDir(), "linked-root")
		if err := os.Symlink(realRoot, linkedRoot); err != nil {
			t.Fatalf("create root symlink: %v", err)
		}
		result := New(OSFileSystem{}).Inspect(context.Background(), Config{FlutterSDKRoots: []string{linkedRoot}})
		if len(result.Resources) != 0 || !hasInventoryDiagnostic(result.Diagnostics, "INVENTORY_SYMLINK_ROOT_REJECTED") {
			t.Fatalf("symlink root must be rejected: %#v", result)
		}
	})

	t.Run("FVM candidate", func(t *testing.T) {
		cacheRoot := t.TempDir()
		external := filepath.Join(t.TempDir(), "external-sdk")
		createFlutterSDK(t, external)
		writeInventoryFixture(t, filepath.Join(external, "version"), "9.9.9\n")
		if err := os.Symlink(external, filepath.Join(cacheRoot, "9.9.9")); err != nil {
			t.Fatalf("create FVM symlink: %v", err)
		}
		result := New(OSFileSystem{}).Inspect(context.Background(), Config{FVMCacheRoots: []string{cacheRoot}})
		if len(result.Resources) != 0 || !hasInventoryDiagnostic(result.Diagnostics, "INVENTORY_SYMLINK_CANDIDATE_SKIPPED") {
			t.Fatalf("FVM candidate symlink must be skipped: %#v", result)
		}
	})

	t.Run("size entry", func(t *testing.T) {
		flutterRoot := t.TempDir()
		createFlutterSDK(t, flutterRoot)
		writeInventoryFixture(t, filepath.Join(flutterRoot, "version"), "3.35.0\n")
		externalFile := filepath.Join(t.TempDir(), "large-secret")
		writeInventoryFixture(t, externalFile, strings.Repeat("x", 1_000))
		if err := os.Symlink(externalFile, filepath.Join(flutterRoot, "linked-cache")); err != nil {
			t.Fatalf("create size symlink: %v", err)
		}
		result := New(OSFileSystem{}).Inspect(context.Background(), Config{FlutterSDKRoots: []string{flutterRoot}})
		resource := findResource(result.Resources, "flutter", "flutter_sdk", "3.35.0")
		if resource == nil || resource.SizeBytes == nil || *resource.SizeBytes >= 1_000 {
			t.Fatalf("symlink target must not contribute to size: %#v", resource)
		}
		if !hasMetadata(resource.Metadata, "size_symlinks_ignored", "1") {
			t.Fatalf("expected ignored symlink metadata: %#v", resource.Metadata)
		}
	})

	t.Run("metadata intermediate", func(t *testing.T) {
		flutterRoot := t.TempDir()
		createFlutterSDK(t, flutterRoot)
		externalCache := t.TempDir()
		writeInventoryFixture(t, filepath.Join(externalCache, "flutter.version.json"), "{\"frameworkVersion\":\"9.9.7\"}\n")
		if err := os.Symlink(externalCache, filepath.Join(flutterRoot, "bin", "cache")); err != nil {
			t.Fatalf("create metadata symlink: %v", err)
		}
		result := New(OSFileSystem{}).Inspect(context.Background(), Config{FlutterSDKRoots: []string{flutterRoot}})
		if findResource(result.Resources, "flutter", "flutter_sdk", "9.9.7") != nil ||
			!hasInventoryDiagnostic(result.Diagnostics, "INVENTORY_METADATA_SYMLINK_SKIPPED") {
			t.Fatalf("metadata symlink must not be followed: %#v", result)
		}
	})
}

func TestIncompleteMeasurementHasNoPartialSize(t *testing.T) {
	for name, testCase := range map[string]struct {
		limits Limits
		code   string
	}{
		"entry budget": {
			limits: Limits{MaxEntriesPerResource: 2},
			code:   "INVENTORY_SIZE_MAX_ENTRIES_REACHED",
		},
		"depth budget": {
			limits: Limits{MaxDepth: 1},
			code:   "INVENTORY_SIZE_MAX_DEPTH_REACHED",
		},
	} {
		t.Run(name, func(t *testing.T) {
			flutterRoot := t.TempDir()
			createFlutterSDK(t, flutterRoot)
			writeInventoryFixture(t, filepath.Join(flutterRoot, "version"), "3.35.0\n")
			writeInventoryFixture(t, filepath.Join(flutterRoot, "deep", "one", "two", "artifact"), "1234567890")
			result := New(OSFileSystem{}).Inspect(context.Background(), Config{
				FlutterSDKRoots: []string{flutterRoot},
				Limits:          testCase.limits,
			})
			resource := findResource(result.Resources, "flutter", "flutter_sdk", "3.35.0")
			if resource == nil || resource.SizeBytes != nil || !hasMetadata(resource.Metadata, "size_status", "incomplete") {
				t.Fatalf("incomplete measurement must not expose partial size: %#v", resource)
			}
			if !hasInventoryDiagnostic(result.Diagnostics, testCase.code) {
				t.Fatalf("expected %s: %#v", testCase.code, result.Diagnostics)
			}
		})
	}
}

func TestInventoryHandlesCancellationRootsAndFilesystemErrors(t *testing.T) {
	t.Run("no roots and nil filesystem", func(t *testing.T) {
		if result := New(OSFileSystem{}).Inspect(context.Background(), Config{}); !hasInventoryDiagnostic(result.Diagnostics, "INVENTORY_NO_ROOTS") {
			t.Fatalf("expected explicit root requirement: %#v", result)
		}
		if result := New(nil).Inspect(context.Background(), Config{JDKRoots: []string{"/unused"}}); !hasInventoryDiagnostic(result.Diagnostics, "INVENTORY_FILESYSTEM_UNAVAILABLE") {
			t.Fatalf("expected missing filesystem adapter: %#v", result)
		}
	})

	t.Run("missing and duplicate roots", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "missing")
		valid := t.TempDir()
		result := New(OSFileSystem{}).Inspect(context.Background(), Config{
			FVMCacheRoots: []string{valid, valid, missing},
		})
		if !hasInventoryDiagnostic(result.Diagnostics, "INVENTORY_DUPLICATE_ROOT") ||
			!hasInventoryDiagnostic(result.Diagnostics, "INVENTORY_ROOT_UNAVAILABLE") {
			t.Fatalf("expected root diagnostics: %#v", result.Diagnostics)
		}
	})

	t.Run("cancelled", func(t *testing.T) {
		root := t.TempDir()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		result := New(OSFileSystem{}).Inspect(ctx, Config{FlutterSDKRoots: []string{root}})
		if len(result.Resources) != 0 || !hasInventoryDiagnostic(result.Diagnostics, "INVENTORY_CANCELLED") {
			t.Fatalf("expected cancellation before inventory: %#v", result)
		}
	})

	t.Run("metadata open and size walk failures", func(t *testing.T) {
		jdkRoot := t.TempDir()
		releasePath := filepath.Join(jdkRoot, "release")
		writeInventoryFixture(t, releasePath, "JAVA_VERSION=\"17.0.12\"\n")
		fileSystem := faultInventoryFileSystem{failOpen: releasePath, failWalk: jdkRoot}
		result := New(fileSystem).Inspect(context.Background(), Config{JDKRoots: []string{jdkRoot}})
		resource := findResource(result.Resources, "java", "jdk", "")
		if resource == nil || resource.SizeBytes != nil {
			t.Fatalf("resource should survive metadata and size failures: %#v", result.Resources)
		}
		if !hasInventoryDiagnostic(result.Diagnostics, "INVENTORY_METADATA_UNAVAILABLE") ||
			!hasInventoryDiagnostic(result.Diagnostics, "INVENTORY_SIZE_WALK_FAILED") {
			t.Fatalf("expected partial failure diagnostics: %#v", result.Diagnostics)
		}
	})
}

func TestCandidateAndResourceBudgets(t *testing.T) {
	for name, testCase := range map[string]struct {
		limits Limits
		code   string
	}{
		"candidates": {
			limits: Limits{MaxCandidates: 1},
			code:   "INVENTORY_MAX_CANDIDATES_REACHED",
		},
		"resources": {
			limits: Limits{MaxResources: 1},
			code:   "INVENTORY_MAX_RESOURCES_REACHED",
		},
	} {
		t.Run(name, func(t *testing.T) {
			androidRoot := t.TempDir()
			for _, version := range []string{"33.0.0", "34.0.0", "35.0.0"} {
				writeInventoryFixture(t, filepath.Join(androidRoot, "build-tools", version, "source.properties"), "Pkg.Revision="+version+"\n")
			}
			result := New(OSFileSystem{}).Inspect(context.Background(), Config{
				AndroidSDKRoots: []string{androidRoot},
				Limits:          testCase.limits,
			})
			if len(result.Resources) != 1 {
				t.Fatalf("expected one bounded resource, got %#v", result.Resources)
			}
			if !hasInventoryDiagnostic(result.Diagnostics, testCase.code) {
				t.Fatalf("expected %s: %#v", testCase.code, result.Diagnostics)
			}
		})
	}
}

func TestLegacyNDKAndDirectJDKRoots(t *testing.T) {
	base := t.TempDir()
	androidRoot := filepath.Join(base, "android")
	writeInventoryFixture(t, filepath.Join(androidRoot, "ndk-bundle", "source.properties"), "Pkg.Revision=21.4.7075529\n")
	jdkHome := filepath.Join(base, "jdk-home")
	writeInventoryFixture(t, filepath.Join(jdkHome, "release"), "JAVA_VERSION='21.0.4'\n")

	result := New(OSFileSystem{}).Inspect(context.Background(), Config{
		AndroidSDKRoots: []string{androidRoot},
		JDKRoots:        []string{jdkHome},
	})
	if findResource(result.Resources, "android", "ndk", "21.4.7075529") == nil {
		t.Fatalf("expected legacy NDK: %#v", result.Resources)
	}
	if findResource(result.Resources, "java", "jdk", "21.0.4") == nil {
		t.Fatalf("expected direct JDK home: %#v", result.Resources)
	}
}

func TestOversizedMetadataAndInvalidRoot(t *testing.T) {
	flutterRoot := t.TempDir()
	createFlutterSDK(t, flutterRoot)
	writeInventoryFixture(t, filepath.Join(flutterRoot, "version"), strings.Repeat("9", 64))
	rootFile := filepath.Join(t.TempDir(), "not-a-directory")
	writeInventoryFixture(t, rootFile, "fixture")

	result := New(OSFileSystem{}).Inspect(context.Background(), Config{
		FlutterSDKRoots: []string{flutterRoot, rootFile},
		Limits:          Limits{MaxMetadataFileBytes: 16},
	})
	resource := findResource(result.Resources, "flutter", "flutter_sdk", "")
	if resource == nil || resource.Version != nil {
		t.Fatalf("oversized version metadata must remain unknown: %#v", result.Resources)
	}
	if !hasInventoryDiagnostic(result.Diagnostics, "INVENTORY_METADATA_TOO_LARGE") ||
		!hasInventoryDiagnostic(result.Diagnostics, "INVENTORY_ROOT_NOT_DIRECTORY") {
		t.Fatalf("expected metadata and root diagnostics: %#v", result.Diagnostics)
	}
}

func TestUnknownFlutterVersionAndMarkerSymlinkAreConservative(t *testing.T) {
	t.Run("unknown version", func(t *testing.T) {
		flutterRoot := t.TempDir()
		createFlutterSDK(t, flutterRoot)
		writeInventoryFixture(t, filepath.Join(flutterRoot, "bin", "cache", "flutter.version.json"), "{\"frameworkVersion\":\"0.0.0-unknown\"}\n")
		result := New(OSFileSystem{}).Inspect(context.Background(), Config{FlutterSDKRoots: []string{flutterRoot}})
		resource := findResource(result.Resources, "flutter", "flutter_sdk", "")
		if resource == nil || resource.Version != nil || !hasInventoryDiagnostic(result.Diagnostics, "INVENTORY_FLUTTER_VERSION_INVALID") {
			t.Fatalf("unknown Flutter label must not become an installed version: %#v", result)
		}
	})

	t.Run("symlinked marker", func(t *testing.T) {
		flutterRoot := t.TempDir()
		writeInventoryFixture(t, filepath.Join(flutterRoot, "bin", "flutter"), "#!/bin/sh\n")
		externalPackages := t.TempDir()
		if err := os.Symlink(externalPackages, filepath.Join(flutterRoot, "packages")); err != nil {
			t.Fatalf("create marker symlink: %v", err)
		}
		result := New(OSFileSystem{}).Inspect(context.Background(), Config{FlutterSDKRoots: []string{flutterRoot}})
		if len(result.Resources) != 0 || !hasInventoryDiagnostic(result.Diagnostics, "INVENTORY_METADATA_SYMLINK_SKIPPED") {
			t.Fatalf("symlinked Flutter marker must be rejected: %#v", result)
		}
	})
}

func TestDirectoryReadFailureIsPartial(t *testing.T) {
	root := t.TempDir()
	fileSystem := faultInventoryFileSystem{failReadDir: root}
	result := New(fileSystem).Inspect(context.Background(), Config{FVMCacheRoots: []string{root}})
	if len(result.Resources) != 0 || !hasInventoryDiagnostic(result.Diagnostics, "INVENTORY_DIRECTORY_UNAVAILABLE") {
		t.Fatalf("expected directory read failure: %#v", result)
	}
}

func TestGradleInventoryRejectsIncompleteAndUnsafeLayouts(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		root := t.TempDir()
		result := New(OSFileSystem{}).Inspect(context.Background(), Config{GradleUserHomeRoots: []string{root}})
		if len(result.Resources) != 0 || !hasInventoryDiagnostic(result.Diagnostics, "INVENTORY_GRADLE_NO_RESOURCES_FOUND") {
			t.Fatalf("expected empty Gradle root diagnostic: %#v", result)
		}
	})

	t.Run("fixed directory is a file", func(t *testing.T) {
		root := t.TempDir()
		writeInventoryFixture(t, filepath.Join(root, "wrapper", "dists"), "not-a-directory")
		result := New(OSFileSystem{}).Inspect(context.Background(), Config{GradleUserHomeRoots: []string{root}})
		if !hasInventoryDiagnostic(result.Diagnostics, "INVENTORY_DIRECTORY_INVALID") {
			t.Fatalf("expected invalid Gradle directory diagnostic: %#v", result.Diagnostics)
		}
	})

	t.Run("unsafe plugin version and incomplete wrapper", func(t *testing.T) {
		root := t.TempDir()
		writeInventoryFixture(t, filepath.Join(root, "wrapper", "dists", "gradle-8.11-bin", "hash", "download.part"), "partial")
		writeInventoryFixture(t, filepath.Join(root, "caches", "modules-2", "files-2.1", "com.android.tools.build", "gradle", "unsafe version", "hash", "plugin.jar"), "plugin")
		result := New(OSFileSystem{}).Inspect(context.Background(), Config{GradleUserHomeRoots: []string{root}})
		if len(result.Resources) != 0 || !hasInventoryDiagnostic(result.Diagnostics, "INVENTORY_GRADLE_VERSION_DIRECTORY_INVALID") {
			t.Fatalf("incomplete or unsafe Gradle entries must not become resources: %#v", result)
		}
	})

	t.Run("symlinked fixed cache", func(t *testing.T) {
		root := t.TempDir()
		external := t.TempDir()
		modulesParent := filepath.Join(root, "caches", "modules-2")
		if err := os.MkdirAll(modulesParent, 0o755); err != nil {
			t.Fatalf("create cache parent: %v", err)
		}
		if err := os.Symlink(external, filepath.Join(modulesParent, "files-2.1")); err != nil {
			t.Fatalf("create cache symlink: %v", err)
		}
		result := New(OSFileSystem{}).Inspect(context.Background(), Config{GradleUserHomeRoots: []string{root}})
		if len(result.Resources) != 0 || !hasInventoryDiagnostic(result.Diagnostics, "INVENTORY_METADATA_SYMLINK_SKIPPED") {
			t.Fatalf("Gradle cache symlink must not be followed: %#v", result)
		}
	})

	t.Run("resource budget", func(t *testing.T) {
		fixture := createInventoryFixture(t)
		result := New(OSFileSystem{}).Inspect(context.Background(), Config{
			GradleUserHomeRoots: []string{fixture.gradleRoot},
			Limits:              Limits{MaxResources: 1},
		})
		if len(result.Resources) != 1 || !hasInventoryDiagnostic(result.Diagnostics, "INVENTORY_MAX_RESOURCES_REACHED") {
			t.Fatalf("expected bounded Gradle inventory: %#v", result)
		}
	})
}

type inventoryFixture struct {
	root        string
	androidRoot string
	flutterRoot string
	fvmRoot     string
	gradleRoot  string
	jdkRoot     string
}

func createInventoryFixture(t *testing.T) inventoryFixture {
	t.Helper()
	root := t.TempDir()
	fixture := inventoryFixture{
		root:        root,
		androidRoot: filepath.Join(root, "android-sdk"),
		flutterRoot: filepath.Join(root, "flutter-sdk"),
		fvmRoot:     filepath.Join(root, "fvm-versions"),
		gradleRoot:  filepath.Join(root, "gradle-home"),
		jdkRoot:     filepath.Join(root, "jdks"),
	}
	writeInventoryFixture(t, filepath.Join(fixture.androidRoot, "platforms", "android-35", "source.properties"), "Pkg.Revision=1\nAndroidVersion.ApiLevel=35\n")
	writeInventoryFixture(t, filepath.Join(fixture.androidRoot, "build-tools", "35.0.0", "source.properties"), "Pkg.Revision=35.0.0\n")
	writeInventoryFixture(t, filepath.Join(fixture.androidRoot, "ndk", "27.0.12077973", "source.properties"), "Pkg.Revision=27.0.12077973\n")
	writeInventoryFixture(t, filepath.Join(fixture.androidRoot, "cmake", "3.22.1", "source.properties"), "Pkg.Revision=3.22.1\n")

	createFlutterSDK(t, fixture.flutterRoot)
	writeInventoryFixture(t, filepath.Join(fixture.flutterRoot, "bin", "cache", "flutter.version.json"), `{
  "frameworkVersion": "3.35.0",
  "channel": "stable",
  "dartSdkVersion": "3.9.0 (build synthetic)"
}
`)
	fvmSDK := filepath.Join(fixture.fvmRoot, "3.19.6")
	createFlutterSDK(t, fvmSDK)
	writeInventoryFixture(t, filepath.Join(fvmSDK, "version"), "3.19.6\n")
	writeInventoryFixture(t, filepath.Join(fvmSDK, "bin", "cache", "dart-sdk", "version"), "3.3.4\n")

	gradleDistribution := filepath.Join(fixture.gradleRoot, "wrapper", "dists", "gradle-8.12-bin", "fixture-hash")
	writeInventoryFixture(t, filepath.Join(gradleDistribution, "gradle-8.12", "bin", "gradle"), "synthetic-gradle")
	writeInventoryFixture(t, filepath.Join(gradleDistribution, "gradle-8.12-bin.zip.ok"), "")
	writeInventoryFixture(t, filepath.Join(fixture.gradleRoot, "caches", "modules-2", "files-2.1", "com.android.tools.build", "gradle", "8.7.3", "hash", "gradle-8.7.3.jar"), "synthetic-agp")
	writeInventoryFixture(t, filepath.Join(fixture.gradleRoot, "caches", "modules-2", "files-2.1", "org.jetbrains.kotlin", "kotlin-gradle-plugin", "2.1.0", "hash", "kotlin-gradle-plugin-2.1.0.jar"), "synthetic-kotlin")

	jdkHome := filepath.Join(fixture.jdkRoot, "temurin-17.jdk", "Contents", "Home")
	writeInventoryFixture(t, filepath.Join(jdkHome, "release"), "JAVA_VERSION=\"17.0.12\"\nIMPLEMENTOR=\"Synthetic\"\n")
	writeInventoryFixture(t, filepath.Join(jdkHome, "bin", "java"), "synthetic-java")
	return fixture
}

func createFlutterSDK(t *testing.T, root string) {
	t.Helper()
	writeInventoryFixture(t, filepath.Join(root, "bin", "flutter"), "#!/bin/sh\n")
	writeInventoryFixture(t, filepath.Join(root, "packages", "flutter", "README"), "synthetic Flutter SDK\n")
}

func writeInventoryFixture(t *testing.T, filePath, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("create fixture directory: %v", err)
	}
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}

func findResource(resources []domain.InstalledResource, ecosystem, component, version string) *domain.InstalledResource {
	for index := range resources {
		resource := &resources[index]
		if resource.Ecosystem != ecosystem || resource.Component != component {
			continue
		}
		if version == "" && resource.Version == nil {
			return resource
		}
		if resource.Version != nil && *resource.Version == version {
			return resource
		}
	}
	return nil
}

func hasMetadata(metadata []domain.MetadataEntry, key, value string) bool {
	for _, entry := range metadata {
		if entry.Key == key && entry.Value == value {
			return true
		}
	}
	return false
}

func hasInventoryDiagnostic(diagnostics []domain.Diagnostic, code string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}

func logicalRegularSize(t *testing.T, root string) int64 {
	t.Helper()
	var size int64
	if err := filepath.WalkDir(root, func(_ string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&fs.ModeSymlink != 0 || entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			size += info.Size()
		}
		return nil
	}); err != nil {
		t.Fatalf("measure logical fixture size: %v", err)
	}
	return size
}

type inventoryFileSnapshot struct {
	hash       [sha256.Size]byte
	modifiedAt time.Time
}

func snapshotInventoryFiles(t *testing.T, root string) map[string]inventoryFileSnapshot {
	t.Helper()
	snapshot := make(map[string]inventoryFileSnapshot)
	if err := filepath.WalkDir(root, func(currentPath string, entry fs.DirEntry, walkErr error) error {
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
		snapshot[currentPath] = inventoryFileSnapshot{
			hash:       sha256.Sum256(content),
			modifiedAt: info.ModTime(),
		}
		return nil
	}); err != nil {
		t.Fatalf("snapshot inventory fixtures: %v", err)
	}
	return snapshot
}

type faultInventoryFileSystem struct {
	OSFileSystem
	failOpen    string
	failWalk    string
	failReadDir string
}

func (fileSystem faultInventoryFileSystem) Open(name string) (fs.File, error) {
	if name == fileSystem.failOpen {
		return nil, fs.ErrPermission
	}
	return fileSystem.OSFileSystem.Open(name)
}

func (fileSystem faultInventoryFileSystem) WalkDir(root string, fn fs.WalkDirFunc) error {
	if root == fileSystem.failWalk {
		return fs.ErrPermission
	}
	return fileSystem.OSFileSystem.WalkDir(root, fn)
}

func (fileSystem faultInventoryFileSystem) ReadDir(name string) ([]fs.DirEntry, error) {
	if name == fileSystem.failReadDir {
		return nil, fs.ErrPermission
	}
	return fileSystem.OSFileSystem.ReadDir(name)
}
