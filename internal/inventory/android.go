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
