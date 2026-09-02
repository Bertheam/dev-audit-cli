package inventory

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"

	"dev-environment-auditor/internal/domain"
)

type flutterVersionMetadata struct {
	FrameworkVersion string `json:"frameworkVersion"`
	FlutterVersion   string `json:"flutterVersion"`
	Channel          string `json:"channel"`
	DartSDKVersion   string `json:"dartSdkVersion"`
}

func (inventory *Inventory) inspectFVMCache(
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
		candidatePath := filepath.Join(root, entry.Name())
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
		inventory.inspectFlutterSDK(ctx, candidatePath, "fvm_cache", limits, builder, result)
	}
	if builder.len() == before {
		result.Diagnostics = append(result.Diagnostics, newDiagnostic(
			"INVENTORY_FVM_NO_SDKS_FOUND",
			domain.SeverityInfo,
			"no Flutter SDK was found among the direct FVM cache children",
			root,
		))
	}
}

func (inventory *Inventory) inspectFlutterSDK(
	ctx context.Context,
	root string,
	source string,
	limits Limits,
	builder *resourceBuilder,
	result *Result,
) {
	if !inventory.hasFlutterSDKMarkers(root, result) {
		result.Diagnostics = append(result.Diagnostics, newDiagnostic(
			"INVENTORY_FLUTTER_SDK_MARKERS_MISSING",
			domain.SeverityWarning,
			"candidate does not contain the required static Flutter SDK markers",
			root,
		))
		return
	}
	version, channel, dartVersion, versionSource := inventory.flutterVersion(root, source, limits, result)
	metadata := []domain.MetadataEntry{metadataEntry("inventory_source", source)}
	if versionSource != "" {
		metadata = append(metadata, metadataEntry("version_source", versionSource))
	}
	if channel != "" {
		metadata = append(metadata, metadataEntry("channel", channel))
	}
	if dartVersion != "" {
		metadata = append(metadata,
			metadataEntry("dart_sdk_version", dartVersion),
			metadataEntry("dart_version_source", "Flutter SDK metadata"),
		)
	}
	var versionPointer *string
	if version != "" {
		versionPointer = &version
	}
	inventory.addMeasuredResource(
		ctx,
		"flutter",
		"flutter_sdk",
		versionPointer,
		root,
		metadata,
		nil,
		limits,
		builder,
		result,
	)
}

func (inventory *Inventory) hasFlutterSDKMarkers(root string, result *Result) bool {
	markers := []struct {
		path      string
		directory bool
	}{
		{path: filepath.Join(root, "bin", "flutter")},
		{path: filepath.Join(root, "packages", "flutter"), directory: true},
	}
	for _, marker := range markers {
		if diagnostic := inventory.rejectSymlinkPath(root, marker.path); diagnostic != nil {
			if diagnostic.Code != "INVENTORY_METADATA_NOT_FOUND" {
				result.Diagnostics = append(result.Diagnostics, *diagnostic)
			}
			return false
		}
		info, err := inventory.fileSystem.Lstat(marker.path)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				result.Diagnostics = append(result.Diagnostics, fileErrorDiagnostic(
					"INVENTORY_FLUTTER_MARKER_UNAVAILABLE",
					"cannot inspect Flutter SDK marker",
					marker.path,
					err,
				))
			}
			return false
		}
		if marker.directory != info.IsDir() {
			return false
		}
		if !marker.directory && !info.Mode().IsRegular() {
			return false
		}
	}
	return true
}

func (inventory *Inventory) flutterVersion(
	root string,
	source string,
	limits Limits,
	result *Result,
) (string, string, string, string) {
	jsonPath := filepath.Join(root, "bin", "cache", "flutter.version.json")
	if content := inventory.readMetadataFile(root, jsonPath, limits, result); content != nil {
		var metadata flutterVersionMetadata
		if err := json.Unmarshal(content, &metadata); err != nil {
			result.Diagnostics = append(result.Diagnostics, newDiagnostic(
				"INVENTORY_FLUTTER_VERSION_JSON_MALFORMED",
				domain.SeverityWarning,
				"Flutter version metadata is not valid JSON",
				jsonPath,
			))
		} else {
			version := strings.TrimSpace(metadata.FrameworkVersion)
			if version == "" {
				version = strings.TrimSpace(metadata.FlutterVersion)
			}
			channel := strings.TrimSpace(metadata.Channel)
			if !isSafeVersion(channel) {
				channel = ""
			}
			dartVersion := firstSafeVersionToken(metadata.DartSDKVersion)
			if isKnownFlutterVersion(version) {
				return version, channel, dartVersion, "bin/cache/flutter.version.json:frameworkVersion"
			}
			result.Diagnostics = append(result.Diagnostics, newDiagnostic(
				"INVENTORY_FLUTTER_VERSION_INVALID",
				domain.SeverityWarning,
				"Flutter version metadata does not contain a safe static version",
				jsonPath,
			))
		}
	}

	legacyPath := filepath.Join(root, "version")
	if content := inventory.readMetadataFile(root, legacyPath, limits, result); content != nil {
		version := strings.TrimSpace(string(content))
		if isKnownFlutterVersion(version) {
			return version, "", inventory.legacyDartVersion(root, limits, result), "version"
		}
		result.Diagnostics = append(result.Diagnostics, newDiagnostic(
			"INVENTORY_FLUTTER_VERSION_INVALID",
			domain.SeverityWarning,
			"legacy Flutter version file does not contain a safe static version",
			legacyPath,
		))
	}

	if source == "fvm_cache" {
		fallback := filepath.Base(root)
		if isKnownFlutterVersion(fallback) {
			return fallback, "", inventory.legacyDartVersion(root, limits, result), "fvm_directory_name"
		}
	}
	return "", "", inventory.legacyDartVersion(root, limits, result), ""
}

func isKnownFlutterVersion(version string) bool {
	return isSafeVersion(version) && !strings.Contains(strings.ToLower(version), "unknown")
}

func (inventory *Inventory) legacyDartVersion(root string, limits Limits, result *Result) string {
	versionPath := filepath.Join(root, "bin", "cache", "dart-sdk", "version")
	content := inventory.readMetadataFile(root, versionPath, limits, result)
	if content == nil {
		return ""
	}
	return firstSafeVersionToken(string(content))
}

func firstSafeVersionToken(value string) string {
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) == 0 || !isSafeVersion(fields[0]) {
		return ""
	}
	return fields[0]
}
