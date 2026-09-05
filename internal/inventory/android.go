package inventory

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"

	"dev-environment-auditor/internal/domain"
)

type androidPackageKind struct {
	directory string
	component string
}

var androidPackageKinds = []androidPackageKind{
	{directory: "platforms", component: "android_sdk_platform"},
	{directory: "build-tools", component: "android_build_tools"},
	{directory: "ndk", component: "ndk"},
	{directory: "cmake", component: "cmake"},
}

func (inventory *Inventory) inspectAndroidRoot(
	ctx context.Context,
	root string,
	limits Limits,
	builder *resourceBuilder,
	result *Result,
) {
	before := builder.len()
	for _, packageKind := range androidPackageKinds {
		if ctx.Err() != nil {
			break
		}
		if builder.len() >= limits.MaxResources {
			appendMaxResourcesDiagnostic(root, limits, result)
			break
		}
		container := filepath.Join(root, packageKind.directory)
		info, err := inventory.fileSystem.Lstat(container)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			result.Diagnostics = append(result.Diagnostics, fileErrorDiagnostic(
				"INVENTORY_ANDROID_DIRECTORY_UNAVAILABLE",
				"cannot inspect Android SDK package directory",
				container,
				err,
			))
			continue
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			result.Diagnostics = append(result.Diagnostics, newDiagnostic(
				"INVENTORY_SYMLINK_CANDIDATE_SKIPPED",
				domain.SeverityInfo,
				"symbolic link package directory was not followed",
				container,
			))
			continue
		}
		if !info.IsDir() {
			result.Diagnostics = append(result.Diagnostics, newDiagnostic(
				"INVENTORY_ANDROID_DIRECTORY_INVALID",
				domain.SeverityWarning,
				"Android SDK package path is not a directory",
				container,
			))
			continue
		}
		for _, entry := range inventory.readDirectory(container, result) {
			if ctx.Err() != nil {
				break
			}
			candidatePath := filepath.Join(container, entry.Name())
			if builder.len() >= limits.MaxResources {
				appendMaxResourcesDiagnostic(candidatePath, limits, result)
				break
			}
			if result.Stats.CandidatesInspected >= limits.MaxCandidates {
				appendMaxCandidatesDiagnostic(candidatePath, limits, result)
				break
			}
			if _, ok := inventory.inspectCandidateDirectory(candidatePath, limits, result); !ok {
				continue
			}
			inventory.addAndroidPackage(ctx, root, candidatePath, packageKind.component, limits, builder, result)
		}
	}
	if ctx.Err() == nil && builder.len() < limits.MaxResources {
		inventory.inspectAndroidSystemImages(ctx, root, limits, builder, result)
	}

	legacyNDK := filepath.Join(root, "ndk-bundle")
	if ctx.Err() == nil && builder.len() < limits.MaxResources {
		if _, err := inventory.fileSystem.Lstat(legacyNDK); err == nil {
			if _, ok := inventory.inspectCandidateDirectory(legacyNDK, limits, result); ok {
				inventory.addAndroidPackage(ctx, root, legacyNDK, "ndk", limits, builder, result)
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			result.Diagnostics = append(result.Diagnostics, fileErrorDiagnostic(
				"INVENTORY_CANDIDATE_UNAVAILABLE",
				"cannot inspect legacy Android NDK candidate",
				legacyNDK,
				err,
			))
		}
	} else if ctx.Err() == nil {
		if _, err := inventory.fileSystem.Lstat(legacyNDK); err == nil {
			appendMaxResourcesDiagnostic(legacyNDK, limits, result)
		}
	}
	if builder.len() == before {
		result.Diagnostics = append(result.Diagnostics, newDiagnostic(
			"INVENTORY_ANDROID_NO_PACKAGES_FOUND",
			domain.SeverityInfo,
			"no supported Android SDK packages were found",
			root,
		))
	}
}

func (inventory *Inventory) inspectAndroidSystemImages(
	ctx context.Context,
	sdkRoot string,
	limits Limits,
	builder *resourceBuilder,
	result *Result,
) {
	container := filepath.Join(sdkRoot, "system-images")
	info, err := inventory.fileSystem.Lstat(container)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, fileErrorDiagnostic(
			"INVENTORY_ANDROID_DIRECTORY_UNAVAILABLE",
			"cannot inspect Android system image directory",
			container,
			err,
		))
		return
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		result.Diagnostics = append(result.Diagnostics, newDiagnostic(
			"INVENTORY_SYMLINK_CANDIDATE_SKIPPED",
			domain.SeverityInfo,
			"symbolic link system image directory was not followed",
			container,
		))
		return
	}
	if !info.IsDir() {
		result.Diagnostics = append(result.Diagnostics, newDiagnostic(
			"INVENTORY_ANDROID_DIRECTORY_INVALID",
			domain.SeverityWarning,
			"Android system image path is not a directory",
			container,
		))
		return
	}

	for _, platformEntry := range inventory.readDirectory(container, result) {
		if ctx.Err() != nil || builder.len() >= limits.MaxResources || result.Stats.CandidatesInspected >= limits.MaxCandidates {
			appendAndroidInventoryBudgetDiagnostic(filepath.Join(container, platformEntry.Name()), limits, builder, result)
			return
		}
		platformPath := filepath.Join(container, platformEntry.Name())
		if _, ok := inventory.inspectCandidateDirectory(platformPath, limits, result); !ok {
			continue
		}
		apiLevel := strings.TrimPrefix(platformEntry.Name(), "android-")
		if !androidImageAPI.MatchString(apiLevel) {
			continue
		}
		for _, tagEntry := range inventory.readDirectory(platformPath, result) {
			tagPath := filepath.Join(platformPath, tagEntry.Name())
			if ctx.Err() != nil || builder.len() >= limits.MaxResources || result.Stats.CandidatesInspected >= limits.MaxCandidates {
				appendAndroidInventoryBudgetDiagnostic(tagPath, limits, builder, result)
				return
			}
			if _, ok := inventory.inspectCandidateDirectory(tagPath, limits, result); !ok || !isSafeVersion(tagEntry.Name()) {
				continue
			}
			for _, abiEntry := range inventory.readDirectory(tagPath, result) {
				imagePath := filepath.Join(tagPath, abiEntry.Name())
				if ctx.Err() != nil || builder.len() >= limits.MaxResources || result.Stats.CandidatesInspected >= limits.MaxCandidates {
					appendAndroidInventoryBudgetDiagnostic(imagePath, limits, builder, result)
					return
				}
				if _, ok := inventory.inspectCandidateDirectory(imagePath, limits, result); !ok || !isSafeVersion(abiEntry.Name()) {
					continue
				}
				version := strings.Join([]string{"android-" + apiLevel, tagEntry.Name(), abiEntry.Name()}, "/")
				inventory.addMeasuredResource(
					ctx,
					"android",
					"android_system_image",
					&version,
					imagePath,
					[]domain.MetadataEntry{
						metadataEntry("inventory_source", "android_sdk_root"),
						metadataEntry("sdk_root", sdkRoot),
						metadataEntry("api_level", apiLevel),
						metadataEntry("tag", tagEntry.Name()),
						metadataEntry("abi", abiEntry.Name()),
						metadataEntry("package_path", strings.Join([]string{"system-images", "android-" + apiLevel, tagEntry.Name(), abiEntry.Name()}, ";")),
						metadataEntry("management", "sdkmanager"),
					},
					[]string{"no project reference is inferred for Android system images yet"},
					limits,
					builder,
					result,
				)
			}
		}
	}
}

func appendAndroidInventoryBudgetDiagnostic(path string, limits Limits, builder *resourceBuilder, result *Result) {
	if builder.len() >= limits.MaxResources {
		appendMaxResourcesDiagnostic(path, limits, result)
	} else if result.Stats.CandidatesInspected >= limits.MaxCandidates {
		appendMaxCandidatesDiagnostic(path, limits, result)
	}
}

func (inventory *Inventory) inspectAndroidAVDRoot(
	ctx context.Context,
	root string,
	limits Limits,
	builder *resourceBuilder,
	result *Result,
) {
	before := builder.len()
	for _, entry := range inventory.readDirectory(root, result) {
		if ctx.Err() != nil {
			break
		}
		if builder.len() >= limits.MaxResources {
			appendMaxResourcesDiagnostic(filepath.Join(root, entry.Name()), limits, result)
			break
		}
		if !strings.HasSuffix(strings.ToLower(entry.Name()), ".avd") {
			continue
		}
		avdPath := filepath.Join(root, entry.Name())
		if _, ok := inventory.inspectCandidateDirectory(avdPath, limits, result); !ok {
			continue
		}
		configPath := filepath.Join(avdPath, "config.ini")
		properties := parseProperties(inventory.readMetadataFile(avdPath, configPath, limits, result))
		metadata := []domain.MetadataEntry{
			metadataEntry("inventory_source", "android_avd_root"),
			metadataEntry("avd_root", root),
			metadataEntry("sensitivity", "sensitive_mutable_user_data"),
			metadataEntry("management", "avdmanager"),
		}
		if name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())); isSafeVersion(name) {
			metadata = append(metadata, metadataEntry("avd_name", name))
		}
		for _, key := range []string{"AvdId", "target", "tag.id", "abi.type", "hw.device.name"} {
			if value := strings.TrimSpace(properties[key]); isSafeVersion(value) {
				metadata = append(metadata, metadataEntry(strings.ToLower(strings.ReplaceAll(key, ".", "_")), value))
			}
		}
		version := androidAVDVersion(properties)
		var versionPointer *string
		if version != "" {
			versionPointer = &version
		}
		inventory.addMeasuredResource(
			ctx,
			"android",
			"android_avd",
			versionPointer,
			avdPath,
			metadata,
			[]string{
				"AVD files can contain mutable user data and are sensitive",
				"AVD activity and project usage cannot be determined statically; no cleanup action is implied",
			},
			limits,
			builder,
			result,
		)
	}
	if builder.len() == before {
		result.Diagnostics = append(result.Diagnostics, newDiagnostic(
			"INVENTORY_ANDROID_NO_AVDS_FOUND",
			domain.SeverityInfo,
			"no Android Virtual Device directory was found",
			root,
		))
	}
}

func androidAVDVersion(properties map[string]string) string {
	if target := strings.TrimSpace(properties["target"]); isSafeVersion(target) {
		return target
	}
	api := ""
	if value := strings.TrimSpace(properties["image.sysdir.1"]); value != "" {
		for _, component := range strings.FieldsFunc(filepath.ToSlash(value), func(character rune) bool {
			return character == '/' || character == ';'
		}) {
			if strings.HasPrefix(component, "android-") && androidImageAPI.MatchString(strings.TrimPrefix(component, "android-")) {
				api = component
				break
			}
		}
	}
	return api
}

func (inventory *Inventory) addAndroidPackage(
	ctx context.Context,
	sdkRoot string,
	packagePath string,
	component string,
	limits Limits,
	builder *resourceBuilder,
	result *Result,
) {
	propertiesPath := filepath.Join(packagePath, "source.properties")
	properties := parseProperties(inventory.readMetadataFile(packagePath, propertiesPath, limits, result))
	version := ""
	versionSource := ""
	if component == "android_sdk_platform" {
		if value := strings.TrimSpace(properties["AndroidVersion.ApiLevel"]); androidAPILevel.MatchString(value) {
			version = value
			versionSource = "source.properties:AndroidVersion.ApiLevel"
		} else if fallback := strings.TrimPrefix(filepath.Base(packagePath), "android-"); androidAPILevel.MatchString(fallback) {
			version = fallback
			versionSource = "directory_name"
		}
	} else {
		if value := strings.TrimSpace(properties["Pkg.Revision"]); isSafeVersion(value) {
			version = value
			versionSource = "source.properties:Pkg.Revision"
		} else if fallback := filepath.Base(packagePath); isSafeVersion(fallback) && fallback != "ndk-bundle" {
			version = fallback
			versionSource = "directory_name"
		}
	}
	metadata := []domain.MetadataEntry{
		metadataEntry("inventory_source", "android_sdk_root"),
		metadataEntry("sdk_root", sdkRoot),
	}
	if packageIdentifier, ok := androidPackageIdentifier(sdkRoot, packagePath); ok {
		metadata = append(metadata,
			metadataEntry("package_path", packageIdentifier),
			metadataEntry("management", "sdkmanager"),
		)
	}
	var versionPointer *string
	if version != "" {
		versionPointer = &version
		metadata = append(metadata, metadataEntry("version_source", versionSource))
	}
	inventory.addMeasuredResource(
		ctx,
		"android",
		component,
		versionPointer,
		packagePath,
		metadata,
		nil,
		limits,
		builder,
		result,
	)
}

func androidPackageIdentifier(sdkRoot, packagePath string) (string, bool) {
	relative, err := filepath.Rel(sdkRoot, packagePath)
	if err != nil || relative == "." || relative == "ndk-bundle" ||
		relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", false
	}
	parts := strings.Split(filepath.ToSlash(relative), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", false
	}
	return strings.Join(parts, ";"), true
}

func parseProperties(content []byte) map[string]string {
	properties := make(map[string]string)
	for _, rawLine := range strings.Split(string(content), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		separator := strings.IndexAny(line, "=:")
		if separator <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:separator])
		value := strings.TrimSpace(line[separator+1:])
		if key != "" {
			properties[key] = value
		}
	}
	return properties
}
