package inventory

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"

	"dev-environment-auditor/internal/domain"
)

func (inventory *Inventory) inspectJDKRoot(
	ctx context.Context,
	root string,
	limits Limits,
	builder *resourceBuilder,
	result *Result,
) {
	before := builder.len()
	if home := inventory.findJDKHome(root, result); home != "" {
		inventory.addJDK(ctx, home, "explicit_jdk_root", limits, builder, result)
		return
	}

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
		home := inventory.findJDKHome(candidatePath, result)
		if home == "" {
			continue
		}
		inventory.addJDK(ctx, home, "jdk_container", limits, builder, result)
	}
	if builder.len() == before {
		result.Diagnostics = append(result.Diagnostics, newDiagnostic(
			"INVENTORY_JDK_NONE_FOUND",
			domain.SeverityInfo,
			"no JDK release metadata was found at the root or its direct children",
			root,
		))
	}
}

func (inventory *Inventory) findJDKHome(root string, result *Result) string {
	candidates := []string{
		root,
		filepath.Join(root, "Contents", "Home"),
	}
	for _, candidate := range candidates {
		releasePath := filepath.Join(candidate, "release")
		if !isWithin(root, releasePath) {
			continue
		}
		if diagnostic := inventory.rejectSymlinkPath(root, releasePath); diagnostic != nil {
			if diagnostic.Code != "INVENTORY_METADATA_NOT_FOUND" {
				result.Diagnostics = append(result.Diagnostics, *diagnostic)
			}
			continue
		}
		info, err := inventory.fileSystem.Lstat(releasePath)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				result.Diagnostics = append(result.Diagnostics, fileErrorDiagnostic(
					"INVENTORY_JDK_RELEASE_UNAVAILABLE",
					"cannot inspect JDK release metadata",
					releasePath,
					err,
				))
			}
			continue
		}
		if info.Mode().IsRegular() {
			return candidate
		}
	}
	return ""
}

func (inventory *Inventory) addJDK(
	ctx context.Context,
	home string,
	source string,
	limits Limits,
	builder *resourceBuilder,
	result *Result,
) {
	releasePath := filepath.Join(home, "release")
	properties := parseReleaseProperties(inventory.readMetadataFile(home, releasePath, limits, result))
	version := strings.TrimSpace(properties["JAVA_VERSION"])
	metadata := []domain.MetadataEntry{metadataEntry("inventory_source", source)}
	var versionPointer *string
	if isSafeVersion(version) {
		versionPointer = &version
		metadata = append(metadata, metadataEntry("version_source", "release:JAVA_VERSION"))
	} else {
		result.Diagnostics = append(result.Diagnostics, newDiagnostic(
			"INVENTORY_JDK_VERSION_INVALID",
			domain.SeverityWarning,
			"JDK release metadata does not contain a safe static JAVA_VERSION",
			releasePath,
		))
	}
	inventory.addMeasuredResource(
		ctx,
		"java",
		"jdk",
		versionPointer,
		home,
		metadata,
		nil,
		limits,
		builder,
		result,
	)
}

func parseReleaseProperties(content []byte) map[string]string {
	properties := make(map[string]string)
	for _, rawLine := range strings.Split(string(content), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		separator := strings.Index(line, "=")
		if separator <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:separator])
		value := strings.TrimSpace(line[separator+1:])
		if len(value) >= 2 &&
			((value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1 : len(value)-1]
		}
		if key != "" {
			properties[key] = strings.TrimSpace(value)
		}
	}
	return properties
}
