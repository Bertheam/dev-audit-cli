package inventory

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"regexp"

	"dev-environment-auditor/internal/domain"
)

var gradleDistributionDirectory = regexp.MustCompile(`^gradle-(.+)-(bin|all)$`)

type gradlePluginCache struct {
	path      []string
	ecosystem string
	component string
}

var gradlePluginCaches = []gradlePluginCache{
	{
		path:      []string{"caches", "modules-2", "files-2.1", "com.android.tools.build", "gradle"},
		ecosystem: "android",
		component: "android_gradle_plugin",
	},
	{
		path:      []string{"caches", "modules-2", "files-2.1", "org.jetbrains.kotlin", "kotlin-gradle-plugin"},
		ecosystem: "kotlin",
		component: "kotlin_gradle_plugin",
	},
}

func (inventory *Inventory) inspectGradleUserHome(
	ctx context.Context,
	root string,
	limits Limits,
	builder *resourceBuilder,
	result *Result,
) {
	before := builder.len()
	inventory.inspectGradleDistributions(ctx, root, limits, builder, result)
	for _, cache := range gradlePluginCaches {
		if ctx.Err() != nil {
			break
		}
		if builder.len() >= limits.MaxResources {
			appendMaxResourcesDiagnostic(root, limits, result)
			break
		}
		inventory.inspectGradlePluginCache(ctx, root, cache, limits, builder, result)
	}
	if builder.len() == before {
		result.Diagnostics = append(result.Diagnostics, newDiagnostic(
			"INVENTORY_GRADLE_NO_RESOURCES_FOUND",
			domain.SeverityInfo,
			"no complete Wrapper distribution or supported plugin cache was found",
			root,
		))
	}
}

func (inventory *Inventory) inspectGradleDistributions(
	ctx context.Context,
	root string,
	limits Limits,
	builder *resourceBuilder,
	result *Result,
) {
	distributionsRoot := filepath.Join(root, "wrapper", "dists")
	if !inventory.optionalDirectory(root, distributionsRoot, result) {
		return
	}
	for _, distributionEntry := range inventory.readDirectory(distributionsRoot, result) {
		if ctx.Err() != nil {
			return
		}
		distributionPath := filepath.Join(distributionsRoot, distributionEntry.Name())
		if budgetReached(distributionPath, limits, builder, result) {
			return
		}
		if _, ok := inventory.inspectCandidateDirectory(distributionPath, limits, result); !ok {
			continue
		}
		match := gradleDistributionDirectory.FindStringSubmatch(distributionEntry.Name())
		if match == nil || !isSafeVersion(match[1]) {
			continue
		}
		version := match[1]
		distributionType := match[2]
		for _, hashEntry := range inventory.readDirectory(distributionPath, result) {
			hashPath := filepath.Join(distributionPath, hashEntry.Name())
			if budgetReached(hashPath, limits, builder, result) {
				return
			}
			if _, ok := inventory.inspectCandidateDirectory(hashPath, limits, result); !ok {
				continue
			}
			installationPath := filepath.Join(hashPath, "gradle-"+version)
			if !inventory.optionalDirectory(hashPath, installationPath, result) {
				continue
			}
			inventory.addMeasuredResource(
				ctx,
				"gradle",
				"gradle",
				&version,
				hashPath,
				[]domain.MetadataEntry{
					metadataEntry("inventory_source", "gradle_wrapper_cache"),
					metadataEntry("distribution_type", distributionType),
					metadataEntry("installation_path", installationPath),
				},
				nil,
				limits,
				builder,
				result,
			)
		}
	}
}

func (inventory *Inventory) inspectGradlePluginCache(
	ctx context.Context,
	root string,
	cache gradlePluginCache,
	limits Limits,
	builder *resourceBuilder,
	result *Result,
) {
	cacheRoot := filepath.Join(append([]string{root}, cache.path...)...)
	if !inventory.optionalDirectory(root, cacheRoot, result) {
		return
	}
	for _, versionEntry := range inventory.readDirectory(cacheRoot, result) {
		versionPath := filepath.Join(cacheRoot, versionEntry.Name())
		if ctx.Err() != nil || budgetReached(versionPath, limits, builder, result) {
			return
		}
		if _, ok := inventory.inspectCandidateDirectory(versionPath, limits, result); !ok {
			continue
		}
		version := versionEntry.Name()
		if !isSafeVersion(version) {
			result.Diagnostics = append(result.Diagnostics, newDiagnostic(
				"INVENTORY_GRADLE_VERSION_DIRECTORY_INVALID",
				domain.SeverityWarning,
				"Gradle plugin cache directory is not a safe static version",
				versionPath,
			))
			continue
		}
		inventory.addMeasuredResource(
			ctx,
			cache.ecosystem,
			cache.component,
			&version,
			versionPath,
			[]domain.MetadataEntry{metadataEntry("inventory_source", "gradle_module_cache")},
			nil,
			limits,
			builder,
			result,
		)
	}
}

func (inventory *Inventory) optionalDirectory(base, path string, result *Result) bool {
	if !isWithin(base, path) {
		result.Diagnostics = append(result.Diagnostics, newDiagnostic(
			"INVENTORY_DIRECTORY_OUTSIDE_ROOT",
			domain.SeverityError,
			"inventory directory is outside its authorized root",
			path,
		))
		return false
	}
	if diagnostic := inventory.rejectSymlinkPath(base, path); diagnostic != nil {
		if diagnostic.Code != "INVENTORY_METADATA_NOT_FOUND" {
			result.Diagnostics = append(result.Diagnostics, *diagnostic)
		}
		return false
	}
	info, err := inventory.fileSystem.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false
	}
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, fileErrorDiagnostic(
			"INVENTORY_DIRECTORY_UNAVAILABLE",
			"cannot inspect inventory directory",
			path,
			err,
		))
		return false
	}
	if !info.IsDir() {
		result.Diagnostics = append(result.Diagnostics, newDiagnostic(
			"INVENTORY_DIRECTORY_INVALID",
			domain.SeverityWarning,
			"inventory path is not a directory",
			path,
		))
		return false
	}
	return true
}

func budgetReached(path string, limits Limits, builder *resourceBuilder, result *Result) bool {
	if builder.len() >= limits.MaxResources {
		appendMaxResourcesDiagnostic(path, limits, result)
		return true
	}
	if result.Stats.CandidatesInspected >= limits.MaxCandidates {
		appendMaxCandidatesDiagnostic(path, limits, result)
		return true
	}
	return false
}
