package inventory

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"path/filepath"
	"strings"

	"dev-environment-auditor/internal/domain"
)

var errStopMeasurement = errors.New("stop resource measurement")

type sizeMeasurement struct {
	size            *int64
	entriesVisited  int
	symlinksIgnored int
	specialsIgnored int
	complete        bool
}

func (inventory *Inventory) addMeasuredResource(
	ctx context.Context,
	ecosystem string,
	component string,
	version *string,
	resourcePath string,
	metadata []domain.MetadataEntry,
	warnings []string,
	limits Limits,
	builder *resourceBuilder,
	result *Result,
) {
	inventory.addMeasuredResourceWithVersionPolicy(
		ctx, ecosystem, component, version, resourcePath, metadata, warnings,
		true, limits, builder, result,
	)
}

func (inventory *Inventory) addMeasuredUnversionedResource(
	ctx context.Context,
	ecosystem string,
	component string,
	resourcePath string,
	metadata []domain.MetadataEntry,
	warnings []string,
	limits Limits,
	builder *resourceBuilder,
	result *Result,
) {
	inventory.addMeasuredResourceWithVersionPolicy(
		ctx, ecosystem, component, nil, resourcePath, metadata, warnings,
		false, limits, builder, result,
	)
}

func (inventory *Inventory) addMeasuredResourceWithVersionPolicy(
	ctx context.Context,
	ecosystem string,
	component string,
	version *string,
	resourcePath string,
	metadata []domain.MetadataEntry,
	warnings []string,
	warnMissingVersion bool,
	limits Limits,
	builder *resourceBuilder,
	result *Result,
) {
	if builder.len() >= limits.MaxResources {
		result.Diagnostics = append(result.Diagnostics, newDiagnostic(
			"INVENTORY_MAX_RESOURCES_REACHED",
			domain.SeverityWarning,
			fmt.Sprintf("maximum inventory resource budget %d reached", limits.MaxResources),
			resourcePath,
		))
		return
	}
	measurement := inventory.measureLogicalSize(ctx, resourcePath, limits, result)
	result.Stats.EntriesMeasured += measurement.entriesVisited
	metadata = append(metadata,
		metadataEntry("size_metric", "logical_regular_file_bytes"),
		metadataEntry("size_status", sizeStatus(measurement.complete)),
	)
	if measurement.symlinksIgnored > 0 {
		metadata = append(metadata, metadataEntry("size_symlinks_ignored", integerString(measurement.symlinksIgnored)))
		warnings = append(warnings, "symbolic links were not followed during size measurement")
	}
	if measurement.specialsIgnored > 0 {
		metadata = append(metadata, metadataEntry("size_special_files_ignored", integerString(measurement.specialsIgnored)))
		warnings = append(warnings, "non-regular filesystem entries were excluded from logical size")
	}
	if !measurement.complete {
		warnings = append(warnings, "logical size is unavailable because measurement was incomplete")
	}
	if version == nil && warnMissingVersion {
		warnings = append(warnings, "version could not be determined statically")
	}
	builder.add(ecosystem, component, version, resourcePath, measurement.size, metadata, warnings)
}

func (inventory *Inventory) measureLogicalSize(
	ctx context.Context,
	root string,
	limits Limits,
	result *Result,
) sizeMeasurement {
	measurement := sizeMeasurement{complete: true}
	var total int64
	err := inventory.fileSystem.WalkDir(root, func(currentPath string, entry fs.DirEntry, walkErr error) error {
		if ctx.Err() != nil {
			measurement.complete = false
			return errStopMeasurement
		}
		if walkErr != nil {
			measurement.complete = false
			result.Diagnostics = append(result.Diagnostics, fileErrorDiagnostic(
				"INVENTORY_SIZE_ENTRY_UNAVAILABLE",
				"cannot inspect entry during size measurement",
				currentPath,
				walkErr,
			))
			if entry != nil && entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		measurement.entriesVisited++
		if measurement.entriesVisited > limits.MaxEntriesPerResource {
			measurement.complete = false
			result.Diagnostics = append(result.Diagnostics, newDiagnostic(
				"INVENTORY_SIZE_MAX_ENTRIES_REACHED",
				domain.SeverityWarning,
				fmt.Sprintf("maximum size entry budget %d reached", limits.MaxEntriesPerResource),
				root,
			))
			return errStopMeasurement
		}
		relative, relativeErr := filepath.Rel(root, currentPath)
		if relativeErr != nil {
			measurement.complete = false
			result.Diagnostics = append(result.Diagnostics, newDiagnostic(
				"INVENTORY_SIZE_RELATIVE_PATH_FAILED",
				domain.SeverityWarning,
				"cannot determine entry depth during size measurement",
				currentPath,
			))
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if pathDepth(relative) > limits.MaxDepth {
			measurement.complete = false
			result.Diagnostics = append(result.Diagnostics, newDiagnostic(
				"INVENTORY_SIZE_MAX_DEPTH_REACHED",
				domain.SeverityWarning,
				fmt.Sprintf("maximum size depth %d reached", limits.MaxDepth),
				currentPath,
			))
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			measurement.symlinksIgnored++
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			measurement.complete = false
			result.Diagnostics = append(result.Diagnostics, fileErrorDiagnostic(
				"INVENTORY_SIZE_ENTRY_UNAVAILABLE",
				"cannot read entry metadata during size measurement",
				currentPath,
				infoErr,
			))
			return nil
		}
		if !info.Mode().IsRegular() {
			measurement.specialsIgnored++
			return nil
		}
		if info.Size() < 0 || total > math.MaxInt64-info.Size() {
			measurement.complete = false
			result.Diagnostics = append(result.Diagnostics, newDiagnostic(
				"INVENTORY_SIZE_OVERFLOW",
				domain.SeverityWarning,
				"logical size overflowed int64",
				root,
			))
			return errStopMeasurement
		}
		total += info.Size()
		return nil
	})
	if ctx.Err() != nil {
		measurement.complete = false
		result.Diagnostics = append(result.Diagnostics, newDiagnostic(
			"INVENTORY_CANCELLED",
			domain.SeverityError,
			ctx.Err().Error(),
			root,
		))
	} else if err != nil && !errors.Is(err, errStopMeasurement) {
		measurement.complete = false
		result.Diagnostics = append(result.Diagnostics, fileErrorDiagnostic(
			"INVENTORY_SIZE_WALK_FAILED",
			"cannot complete logical size measurement",
			root,
			err,
		))
	}
	if measurement.complete {
		measurement.size = &total
	}
	return measurement
}

func pathDepth(relative string) int {
	if relative == "." {
		return 0
	}
	return strings.Count(filepath.ToSlash(relative), "/") + 1
}

func sizeStatus(complete bool) string {
	if complete {
		return "complete"
	}
	return "incomplete"
}
