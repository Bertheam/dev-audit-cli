package inventory

import (
	"context"
	"encoding/xml"
	"io"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"dev-environment-auditor/internal/domain"
)

var appleSimulatorDeviceID = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func (inventory *Inventory) inspectXcodeRoot(
	ctx context.Context,
	root string,
	limits Limits,
	builder *resourceBuilder,
	result *Result,
) {
	developerRoot := filepath.Join(root, "Contents", "Developer")
	if !inventory.optionalDirectory(root, filepath.Join(developerRoot, "Platforms"), result) ||
		!inventory.optionalDirectory(root, filepath.Join(developerRoot, "Toolchains"), result) {
		result.Diagnostics = append(result.Diagnostics, newDiagnostic(
			"INVENTORY_XCODE_ROOT_INVALID",
			domain.SeverityWarning,
			"Xcode application root does not contain the expected Platforms and Toolchains directories",
			root,
		))
		return
	}
	version := inventory.applePlistString(
		root,
		filepath.Join(root, "Contents", "version.plist"),
		"CFBundleShortVersionString",
		limits,
		result,
	)
	if version != "" && !isSafeAppleDisplayValue(version) {
		result.Diagnostics = append(result.Diagnostics, newDiagnostic(
			"INVENTORY_XCODE_VERSION_INVALID",
			domain.SeverityWarning,
			"Xcode version metadata is not a safe static value",
			filepath.Join(root, "Contents", "version.plist"),
		))
		version = ""
	}
	inventory.addMeasuredResource(
		ctx,
		"apple",
		"xcode",
		optionalString(version),
		root,
		[]domain.MetadataEntry{
			metadataEntry("inventory_source", "xcode_application"),
			metadataEntry("location_kind", "filesystem"),
		},
		[]string{"Xcode installations are inventory-only and never automatic cleanup candidates"},
		limits,
		builder,
		result,
	)
}

func (inventory *Inventory) inspectAppleDeveloperRoot(
	ctx context.Context,
	root string,
	limits Limits,
	builder *resourceBuilder,
	result *Result,
) {
	before := builder.len()
	inventory.inspectAppleChildren(ctx, root, filepath.Join(root, "Xcode", "DerivedData"), "xcode_derived_data", "xcode_derived_data", []domain.MetadataEntry{
		metadataEntry("management", "filesystem"),
		metadataEntry("cleanup_strategy", "targeted_directory_removal"),
	}, []string{"generated Xcode build and index data; cleanup requires explicit plan selection and confirmation"}, limits, builder, result)
	inventory.inspectAppleChildren(ctx, root, filepath.Join(root, "Xcode", "iOS DeviceSupport"), "xcode_device_support", "xcode_device_support", nil,
		[]string{"device support compatibility cannot be inferred from filesystem presence alone"}, limits, builder, result)
	inventory.inspectSimulatorDevices(ctx, root, limits, builder, result)
	inventory.inspectSimulatorRuntimes(ctx, root, filepath.Join(root, "CoreSimulator", "Profiles", "Runtimes"), limits, builder, result)
	inventory.inspectSimulatorVolumeRuntimes(ctx, root, limits, builder, result)
	inventory.inspectAppleChildren(ctx, root, filepath.Join(root, "Packages"), "xcode_component_download", "xcode_component_download", []domain.MetadataEntry{
		metadataEntry("sensitivity", "sensitive_mutable_download"),
		metadataEntry("activity_state", "unknown_may_be_in_progress"),
	}, []string{"component download state is unknown; never modify while Xcode may be downloading or installing"}, limits, builder, result)

	commandLineTools := filepath.Join(root, "CommandLineTools")
	if inventory.optionalDirectory(root, commandLineTools, result) && !budgetReached(commandLineTools, limits, builder, result) {
		if _, ok := inventory.inspectCandidateDirectory(commandLineTools, limits, result); ok {
			inventory.addMeasuredUnversionedResource(ctx, "apple", "xcode_command_line_tools", commandLineTools,
				[]domain.MetadataEntry{metadataEntry("inventory_source", "xcode_command_line_tools")},
				[]string{"Command Line Tools are inventory-only and never automatic cleanup candidates"},
				limits, builder, result)
		}
	}
	if builder.len() == before {
		result.Diagnostics = append(result.Diagnostics, newDiagnostic(
			"INVENTORY_APPLE_NO_RESOURCES_FOUND",
			domain.SeverityInfo,
			"no supported Xcode or CoreSimulator resource was found",
			root,
		))
	}
}

func (inventory *Inventory) inspectAppleChildren(
	ctx context.Context,
	root string,
	container string,
	component string,
	source string,
	extraMetadata []domain.MetadataEntry,
	warnings []string,
	limits Limits,
	builder *resourceBuilder,
	result *Result,
) {
	if !inventory.optionalDirectory(root, container, result) {
		return
	}
	for _, entry := range inventory.readDirectory(container, result) {
		candidate := filepath.Join(container, entry.Name())
		if ctx.Err() != nil || budgetReached(candidate, limits, builder, result) {
			return
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			result.Diagnostics = append(result.Diagnostics, newDiagnostic(
				"INVENTORY_SYMLINK_CANDIDATE_SKIPPED", domain.SeverityInfo,
				"symbolic link candidate was not followed", candidate,
			))
			continue
		}
		if !entry.IsDir() && !entry.Type().IsRegular() {
			continue
		}
		result.Stats.CandidatesInspected++
		metadata := append([]domain.MetadataEntry{metadataEntry("inventory_source", source)}, extraMetadata...)
		name := safeAppleName(entry.Name())
		if component == "xcode_device_support" {
			inventory.addMeasuredResource(ctx, "apple", component, optionalString(name), candidate, metadata, warnings, limits, builder, result)
			continue
		}
		if name != "" {
			metadata = append(metadata, metadataEntry("resource_name", name))
		}
		inventory.addMeasuredUnversionedResource(ctx, "apple", component, candidate, metadata, warnings, limits, builder, result)
	}
}

func (inventory *Inventory) inspectSimulatorDevices(
	ctx context.Context,
	root string,
	limits Limits,
	builder *resourceBuilder,
	result *Result,
) {
	devicesRoot := filepath.Join(root, "CoreSimulator", "Devices")
	if !inventory.optionalDirectory(root, devicesRoot, result) {
		return
	}
	for _, entry := range inventory.readDirectory(devicesRoot, result) {
		path := filepath.Join(devicesRoot, entry.Name())
		if ctx.Err() != nil || budgetReached(path, limits, builder, result) {
			return
		}
		if !appleSimulatorDeviceID.MatchString(entry.Name()) {
			continue
		}
		if _, ok := inventory.inspectCandidateDirectory(path, limits, result); !ok {
			continue
		}
		plist := inventory.applePlistStrings(
			path,
			filepath.Join(path, "device.plist"),
			[]string{"name", "runtime"},
			limits,
			result,
		)
		name := plist["name"]
		runtime := plist["runtime"]
		metadata := []domain.MetadataEntry{
			metadataEntry("inventory_source", "core_simulator_device"),
			metadataEntry("device_id", strings.ToUpper(entry.Name())),
			metadataEntry("sensitivity", "sensitive_mutable_user_data"),
			metadataEntry("activity_state", "unknown"),
			metadataEntry("management", "xcode_device_hub"),
		}
		if isSafeAppleDisplayValue(name) {
			metadata = append(metadata, metadataEntry("device_name", name))
		}
		version := simulatorRuntimeDisplayName(runtime)
		inventory.addMeasuredResource(ctx, "apple", "apple_simulator_device", optionalString(version), path, metadata,
			[]string{"simulator data may contain mutable user or test data", "boot and activity state were not queried"},
			limits, builder, result)
	}
}

func (inventory *Inventory) inspectSimulatorRuntimes(
	ctx context.Context,
	root string,
	runtimesRoot string,
	limits Limits,
	builder *resourceBuilder,
	result *Result,
) {
	if !inventory.optionalDirectory(root, runtimesRoot, result) {
		return
	}
	for _, entry := range inventory.readDirectory(runtimesRoot, result) {
		path := filepath.Join(runtimesRoot, entry.Name())
		if ctx.Err() != nil || budgetReached(path, limits, builder, result) {
			return
		}
		if !strings.HasSuffix(strings.ToLower(entry.Name()), ".simruntime") {
			continue
		}
		if _, ok := inventory.inspectCandidateDirectory(path, limits, result); !ok {
			continue
		}
		version := safeAppleName(strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())))
		builder.add(
			"apple",
			"apple_simulator_runtime",
			optionalString(version),
			path,
			nil,
			[]domain.MetadataEntry{
				metadataEntry("inventory_source", "core_simulator_runtime"),
				metadataEntry("management", "xcode_components"),
				metadataEntry("size_metric", "not_measured"),
				metadataEntry("size_status", "skipped_high_cardinality_runtime_tree"),
			},
			[]string{
				"runtime availability and active use were not inferred; manage runtimes through Xcode Components",
				"runtime tree was not traversed because mounted simulator images contain very large file counts",
			},
		)
	}
}

func (inventory *Inventory) inspectSimulatorVolumeRuntimes(
	ctx context.Context,
	root string,
	limits Limits,
	builder *resourceBuilder,
	result *Result,
) {
	volumesRoot := filepath.Join(root, "CoreSimulator", "Volumes")
	if !inventory.optionalDirectory(root, volumesRoot, result) {
		return
	}
	for _, entry := range inventory.readDirectory(volumesRoot, result) {
		volume := filepath.Join(volumesRoot, entry.Name())
		if ctx.Err() != nil || budgetReached(volume, limits, builder, result) {
			return
		}
		if _, ok := inventory.inspectCandidateDirectory(volume, limits, result); !ok {
			continue
		}
		runtimes := filepath.Join(volume, "Library", "Developer", "CoreSimulator", "Profiles", "Runtimes")
		inventory.inspectSimulatorRuntimes(ctx, root, runtimes, limits, builder, result)
	}
}

func (inventory *Inventory) applePlistString(
	basePath string,
	filePath string,
	key string,
	limits Limits,
	result *Result,
) string {
	return inventory.applePlistStrings(basePath, filePath, []string{key}, limits, result)[key]
}

func (inventory *Inventory) applePlistStrings(
	basePath string,
	filePath string,
	keys []string,
	limits Limits,
	result *Result,
) map[string]string {
	values := make(map[string]string)
	content := inventory.readMetadataFile(basePath, filePath, limits, result)
	if len(content) == 0 {
		return values
	}
	for key, value := range parseXMLPlistStrings(content, keys) {
		values[key] = strings.TrimSpace(value)
	}
	return values
}

func parseXMLPlistStrings(content []byte, expectedKeys []string) map[string]string {
	values := make(map[string]string)
	allowlist := make(map[string]struct{}, len(expectedKeys))
	for _, key := range expectedKeys {
		allowlist[key] = struct{}{}
	}
	decoder := xml.NewDecoder(strings.NewReader(string(content)))
	wantedKey := ""
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return values
		}
		if err != nil {
			return map[string]string{}
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "key":
			var key string
			if decoder.DecodeElement(&key, &start) != nil {
				return map[string]string{}
			}
			if _, allowed := allowlist[key]; allowed {
				wantedKey = key
			} else {
				wantedKey = ""
			}
		case "string":
			var value string
			if decoder.DecodeElement(&value, &start) != nil {
				return map[string]string{}
			}
			if wantedKey != "" {
				values[wantedKey] = value
			}
			wantedKey = ""
		default:
			if wantedKey != "" {
				wantedKey = ""
			}
		}
	}
}

func safeAppleName(value string) string {
	value = strings.TrimSpace(value)
	if !isSafeAppleDisplayValue(value) {
		return ""
	}
	return value
}

func isSafeAppleDisplayValue(value string) bool {
	if value == "" || len(value) > 120 {
		return false
	}
	for _, character := range value {
		if unicode.IsLetter(character) || unicode.IsDigit(character) ||
			strings.ContainsRune(" ._+()-", character) {
			continue
		}
		return false
	}
	return true
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func simulatorRuntimeDisplayName(identifier string) string {
	const prefix = "com.apple.CoreSimulator.SimRuntime."
	if !strings.HasPrefix(identifier, prefix) {
		return ""
	}
	value := strings.TrimPrefix(identifier, prefix)
	parts := strings.Split(value, "-")
	if len(parts) < 2 {
		return ""
	}
	platform := parts[0]
	version := strings.Join(parts[1:], ".")
	return safeAppleName(platform + " " + version)
}
