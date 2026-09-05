package inventory

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"dev-environment-auditor/internal/domain"
)

func TestInventoryXcodeAndCoreSimulatorResources(t *testing.T) {
	base := t.TempDir()
	xcode := filepath.Join(base, "Xcode.app")
	writeInventoryFixture(t, filepath.Join(xcode, "Contents", "Developer", "Platforms", "iPhoneOS.platform", "marker"), "platform")
	writeInventoryFixture(t, filepath.Join(xcode, "Contents", "Developer", "Toolchains", "XcodeDefault.xctoolchain", "marker"), "toolchain")
	writeInventoryFixture(t, filepath.Join(xcode, "Contents", "version.plist"), plistFixture(map[string]string{
		"CFBundleShortVersionString": "16.4",
	}))

	developer := filepath.Join(base, "Library", "Developer")
	derivedData := filepath.Join(developer, "Xcode", "DerivedData", "MyApp-abc123")
	writeInventoryFixture(t, filepath.Join(derivedData, "Build", "object.o"), "derived")
	deviceSupport := filepath.Join(developer, "Xcode", "iOS DeviceSupport", "18.5 (22F76)")
	writeInventoryFixture(t, filepath.Join(deviceSupport, "Symbols", "symbol"), "symbols")
	deviceID := "00112233-4455-6677-8899-AABBCCDDEEFF"
	device := filepath.Join(developer, "CoreSimulator", "Devices", deviceID)
	writeInventoryFixture(t, filepath.Join(device, "device.plist"), plistFixture(map[string]string{
		"name":    "iPhone 16 Pro",
		"runtime": "com.apple.CoreSimulator.SimRuntime.iOS-18-5",
	}))
	writeInventoryFixture(t, filepath.Join(device, "data", "fixture.db"), "mutable")
	profileRuntime := filepath.Join(developer, "CoreSimulator", "Profiles", "Runtimes", "iOS 18.5.simruntime")
	writeInventoryFixture(t, filepath.Join(profileRuntime, "Contents", "Resources", "runtime"), "profile-runtime")
	volumeRuntime := filepath.Join(developer, "CoreSimulator", "Volumes", "iOS_18_5", "Library", "Developer", "CoreSimulator", "Profiles", "Runtimes", "iOS 18.5.simruntime")
	writeInventoryFixture(t, filepath.Join(volumeRuntime, "Contents", "Resources", "runtime"), "volume-runtime")
	writeInventoryFixture(t, filepath.Join(developer, "Packages", "iOS_18_5.download"), "in-progress")
	writeInventoryFixture(t, filepath.Join(developer, "CommandLineTools", "usr", "bin", "clang"), "clang")

	config := Config{XcodeRoots: []string{xcode}, AppleDeveloperRoots: []string{developer}}
	before := snapshotInventoryFiles(t, base)
	first := New(OSFileSystem{}).Inspect(context.Background(), config)
	second := New(OSFileSystem{}).Inspect(context.Background(), config)
	after := snapshotInventoryFiles(t, base)

	if !reflect.DeepEqual(first, second) {
		t.Fatal("identical Apple inventories must be deterministic")
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("Apple inventory modified fixture content or mtimes")
	}
	if len(first.Diagnostics) != 0 {
		t.Fatalf("unexpected Apple inventory diagnostics: %#v", first.Diagnostics)
	}
	if first.Stats.RootsScanned != 2 || first.Stats.ResourcesFound != 8 {
		t.Fatalf("unexpected Apple inventory stats: %#v; resources=%#v", first.Stats, first.Resources)
	}
	if findResource(first.Resources, "apple", "xcode", "16.4") == nil ||
		findResource(first.Resources, "apple", "xcode_derived_data", "") == nil ||
		findResource(first.Resources, "apple", "xcode_device_support", "18.5 (22F76)") == nil {
		t.Fatalf("expected Xcode resources were not inventoried: %#v", first.Resources)
	}
	simulator := findResource(first.Resources, "apple", "apple_simulator_device", "iOS 18.5")
	if simulator == nil || !hasMetadata(simulator.Metadata, "sensitivity", "sensitive_mutable_user_data") ||
		!hasMetadata(simulator.Metadata, "activity_state", "unknown") || !hasMetadata(simulator.Metadata, "device_name", "iPhone 16 Pro") {
		t.Fatalf("simulator safety metadata is missing: %#v", simulator)
	}
	if countInventoryResources(first.Resources, "apple_simulator_runtime") != 2 {
		t.Fatalf("profile and volume runtimes should both be visible: %#v", first.Resources)
	}
	download := findResource(first.Resources, "apple", "xcode_component_download", "")
	if download == nil || !hasMetadata(download.Metadata, "sensitivity", "sensitive_mutable_download") ||
		!hasMetadata(download.Metadata, "activity_state", "unknown_may_be_in_progress") ||
		!hasMetadata(download.Metadata, "resource_name", "iOS_18_5.download") {
		t.Fatalf("component download must be protected as mutable: %#v", download)
	}
	for _, resource := range first.Resources {
		if resource.ReferenceStatus != domain.ReferenceUnknown {
			t.Fatalf("unexpected Apple resource measurement: %#v", resource)
		}
		if resource.Component == "apple_simulator_runtime" {
			if resource.SizeBytes != nil || !hasMetadata(resource.Metadata, "size_status", "skipped_high_cardinality_runtime_tree") {
				t.Fatalf("runtime tree should be inventoried without an expensive traversal: %#v", resource)
			}
			continue
		}
		if resource.SizeBytes == nil || *resource.SizeBytes != logicalRegularSize(t, resource.Path) {
			t.Fatalf("unexpected Apple resource measurement: %#v", resource)
		}
	}
}

func TestInventoryRejectsMalformedXcodeRootAndIgnoresUnknownDeviceDirectory(t *testing.T) {
	base := t.TempDir()
	xcode := filepath.Join(base, "Broken.app")
	writeInventoryFixture(t, filepath.Join(xcode, "Contents", "marker"), "broken")
	developer := filepath.Join(base, "Developer")
	writeInventoryFixture(t, filepath.Join(developer, "CoreSimulator", "Devices", "not-a-device", "data"), "ignored")

	result := New(OSFileSystem{}).Inspect(context.Background(), Config{
		XcodeRoots: []string{xcode}, AppleDeveloperRoots: []string{developer},
	})
	if len(result.Resources) != 0 || !hasInventoryDiagnostic(result.Diagnostics, "INVENTORY_XCODE_ROOT_INVALID") ||
		!hasInventoryDiagnostic(result.Diagnostics, "INVENTORY_APPLE_NO_RESOURCES_FOUND") {
		t.Fatalf("invalid Apple roots were not handled conservatively: %#v", result)
	}
}

func plistFixture(values map[string]string) string {
	content := `<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict>`
	keys := []string{"CFBundleShortVersionString", "name", "runtime"}
	for _, key := range keys {
		if value, ok := values[key]; ok {
			content += "<key>" + key + "</key><string>" + value + "</string>"
		}
	}
	return content + `</dict></plist>`
}

func countInventoryResources(resources []domain.InstalledResource, component string) int {
	count := 0
	for _, resource := range resources {
		if resource.Component == component {
			count++
		}
	}
	return count
}
