// Package inventory records locally installed Flutter, Android, Apple and JDK
// resources from explicit, typed roots without invoking package managers.
package inventory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"dev-environment-auditor/internal/domain"
)

const (
	DefaultMaxCandidates         = 4_096
	DefaultMaxResources          = 1_024
	DefaultMaxEntriesPerResource = 500_000
	DefaultMaxDepth              = 64
	DefaultMaxMetadataFileBytes  = 64 << 10
)

type Limits struct {
	MaxCandidates         int
	MaxResources          int
	MaxEntriesPerResource int
	MaxDepth              int
	MaxMetadataFileBytes  int64
}

type Config struct {
	AndroidSDKRoots     []string
	AndroidAVDRoots     []string
	FlutterSDKRoots     []string
	FVMCacheRoots       []string
	GradleUserHomeRoots []string
	JDKRoots            []string
	XcodeRoots          []string
	AppleDeveloperRoots []string
	Limits              Limits
}

type Stats struct {
	RootsScanned        int
	CandidatesInspected int
	ResourcesFound      int
	EntriesMeasured     int
	MetadataFilesRead   int
	MetadataBytesRead   int64
}

type Result struct {
	Resources   []domain.InstalledResource
	Diagnostics []domain.Diagnostic
	Stats       Stats
}

type FileSystem interface {
	Lstat(name string) (fs.FileInfo, error)
	Open(name string) (fs.File, error)
	ReadDir(name string) ([]fs.DirEntry, error)
	WalkDir(root string, fn fs.WalkDirFunc) error
}

type Inventory struct {
	fileSystem FileSystem
}

func New(fileSystem FileSystem) *Inventory {
	return &Inventory{fileSystem: fileSystem}
}

func (inventory *Inventory) Inspect(ctx context.Context, config Config) Result {
	result := Result{Resources: []domain.InstalledResource{}, Diagnostics: []domain.Diagnostic{}}
	if inventory == nil || inventory.fileSystem == nil {
		result.Diagnostics = append(result.Diagnostics, newDiagnostic(
			"INVENTORY_FILESYSTEM_UNAVAILABLE",
			domain.SeverityError,
			"filesystem adapter is required",
			"",
		))
		return result
	}
	if totalConfiguredRoots(config) == 0 {
		result.Diagnostics = append(result.Diagnostics, newDiagnostic(
			"INVENTORY_NO_ROOTS",
			domain.SeverityError,
			"at least one explicit inventory root is required",
			"",
		))
		return result
	}

	limits := normalizeLimits(config.Limits)
	builder := newResourceBuilder()
	groups := []struct {
		kind  rootKind
		paths []string
	}{
		{kind: rootAndroidSDK, paths: config.AndroidSDKRoots},
		{kind: rootAndroidAVD, paths: config.AndroidAVDRoots},
		{kind: rootFlutterSDK, paths: config.FlutterSDKRoots},
		{kind: rootFVMCache, paths: config.FVMCacheRoots},
		{kind: rootGradleUserHome, paths: config.GradleUserHomeRoots},
		{kind: rootJDK, paths: config.JDKRoots},
		{kind: rootXcode, paths: config.XcodeRoots},
		{kind: rootAppleDeveloper, paths: config.AppleDeveloperRoots},
	}

	for _, group := range groups {
		roots, diagnostics := inventory.validateRoots(group.kind, group.paths)
		result.Diagnostics = append(result.Diagnostics, diagnostics...)
		for _, root := range roots {
			if ctx.Err() != nil {
				result.Diagnostics = append(result.Diagnostics, newDiagnostic(
					"INVENTORY_CANCELLED",
					domain.SeverityError,
					ctx.Err().Error(),
					root,
				))
				break
			}
			if builder.len() >= limits.MaxResources {
				result.Diagnostics = append(result.Diagnostics, newDiagnostic(
					"INVENTORY_MAX_RESOURCES_REACHED",
					domain.SeverityWarning,
					fmt.Sprintf("maximum inventory resource budget %d reached", limits.MaxResources),
					root,
				))
				break
			}
			result.Stats.RootsScanned++
			switch group.kind {
			case rootAndroidSDK:
				inventory.inspectAndroidRoot(ctx, root, limits, builder, &result)
			case rootAndroidAVD:
				inventory.inspectAndroidAVDRoot(ctx, root, limits, builder, &result)
			case rootFlutterSDK:
				inventory.inspectFlutterSDK(ctx, root, "explicit_flutter_root", limits, builder, &result)
			case rootFVMCache:
				inventory.inspectFVMCache(ctx, root, limits, builder, &result)
			case rootGradleUserHome:
				inventory.inspectGradleUserHome(ctx, root, limits, builder, &result)
			case rootJDK:
				inventory.inspectJDKRoot(ctx, root, limits, builder, &result)
			case rootXcode:
				inventory.inspectXcodeRoot(ctx, root, limits, builder, &result)
			case rootAppleDeveloper:
				inventory.inspectAppleDeveloperRoot(ctx, root, limits, builder, &result)
			}
		}
	}

	result.Resources = builder.resources()
	result.Stats.ResourcesFound = len(result.Resources)
	sortDiagnostics(result.Diagnostics)
	return result
}

type rootKind string

const (
	rootAndroidSDK     rootKind = "android_sdk"
	rootAndroidAVD     rootKind = "android_avd"
	rootFlutterSDK     rootKind = "flutter_sdk"
	rootFVMCache       rootKind = "fvm_cache"
	rootGradleUserHome rootKind = "gradle_user_home"
	rootJDK            rootKind = "jdk"
	rootXcode          rootKind = "xcode"
	rootAppleDeveloper rootKind = "apple_developer"
)

func totalConfiguredRoots(config Config) int {
	return len(config.AndroidSDKRoots) + len(config.AndroidAVDRoots) + len(config.FlutterSDKRoots) +
		len(config.FVMCacheRoots) + len(config.GradleUserHomeRoots) + len(config.JDKRoots) +
		len(config.XcodeRoots) + len(config.AppleDeveloperRoots)
}

func normalizeLimits(limits Limits) Limits {
	if limits.MaxCandidates <= 0 {
		limits.MaxCandidates = DefaultMaxCandidates
	}
	if limits.MaxResources <= 0 {
		limits.MaxResources = DefaultMaxResources
	}
	if limits.MaxEntriesPerResource <= 0 {
		limits.MaxEntriesPerResource = DefaultMaxEntriesPerResource
	}
	if limits.MaxDepth <= 0 {
		limits.MaxDepth = DefaultMaxDepth
	}
	if limits.MaxMetadataFileBytes <= 0 {
		limits.MaxMetadataFileBytes = DefaultMaxMetadataFileBytes
	}
	return limits
}

func (inventory *Inventory) validateRoots(kind rootKind, paths []string) ([]string, []domain.Diagnostic) {
	roots := make([]string, 0, len(paths))
	diagnostics := make([]domain.Diagnostic, 0)
	seen := make(map[string]struct{})
	for _, rawPath := range paths {
		if strings.TrimSpace(rawPath) == "" {
			diagnostics = append(diagnostics, newDiagnostic(
				"INVENTORY_INVALID_ROOT",
				domain.SeverityError,
				fmt.Sprintf("%s inventory root cannot be empty", kind),
				"",
			))
			continue
		}
		root, err := filepath.Abs(rawPath)
		if err != nil {
			diagnostics = append(diagnostics, newDiagnostic(
				"INVENTORY_INVALID_ROOT",
				domain.SeverityError,
				fmt.Sprintf("cannot canonicalize %s inventory root", kind),
				rawPath,
			))
			continue
		}
		root = filepath.Clean(root)
		if _, exists := seen[root]; exists {
			diagnostics = append(diagnostics, newDiagnostic(
				"INVENTORY_DUPLICATE_ROOT",
				domain.SeverityInfo,
				fmt.Sprintf("duplicate %s inventory root was ignored", kind),
				root,
			))
			continue
		}
		seen[root] = struct{}{}
		info, err := inventory.fileSystem.Lstat(root)
		if err != nil {
			diagnostic := fileErrorDiagnostic(
				"INVENTORY_ROOT_UNAVAILABLE",
				fmt.Sprintf("cannot access %s inventory root", kind),
				root,
				err,
			)
			diagnostic.Severity = domain.SeverityError
			diagnostics = append(diagnostics, diagnostic)
			continue
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			diagnostics = append(diagnostics, newDiagnostic(
				"INVENTORY_SYMLINK_ROOT_REJECTED",
				domain.SeverityError,
				fmt.Sprintf("%s inventory root is a symbolic link", kind),
				root,
			))
			continue
		}
		if !info.IsDir() {
			diagnostics = append(diagnostics, newDiagnostic(
				"INVENTORY_ROOT_NOT_DIRECTORY",
				domain.SeverityError,
				fmt.Sprintf("%s inventory root must be a directory", kind),
				root,
			))
			continue
		}
		roots = append(roots, root)
	}
	sort.Strings(roots)
	return roots, diagnostics
}

func (inventory *Inventory) readMetadataFile(
	basePath string,
	filePath string,
	limits Limits,
	result *Result,
) []byte {
	if !isWithin(basePath, filePath) {
		result.Diagnostics = append(result.Diagnostics, newDiagnostic(
			"INVENTORY_METADATA_OUTSIDE_RESOURCE",
			domain.SeverityError,
			"metadata path is outside its resource",
			filePath,
		))
		return nil
	}
	if diagnostic := inventory.rejectSymlinkPath(basePath, filePath); diagnostic != nil {
		if diagnostic.Code != "INVENTORY_METADATA_NOT_FOUND" {
			result.Diagnostics = append(result.Diagnostics, *diagnostic)
		}
		return nil
	}
	info, err := inventory.fileSystem.Lstat(filePath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, fileErrorDiagnostic(
			"INVENTORY_METADATA_UNAVAILABLE",
			"cannot inspect inventory metadata",
			filePath,
			err,
		))
		return nil
	}
	if !info.Mode().IsRegular() {
		result.Diagnostics = append(result.Diagnostics, newDiagnostic(
			"INVENTORY_METADATA_NOT_REGULAR",
			domain.SeverityWarning,
			"inventory metadata is not a regular file",
			filePath,
		))
		return nil
	}
	if info.Size() > limits.MaxMetadataFileBytes {
		result.Diagnostics = append(result.Diagnostics, newDiagnostic(
			"INVENTORY_METADATA_TOO_LARGE",
			domain.SeverityWarning,
			fmt.Sprintf("inventory metadata exceeds maximum size %d bytes", limits.MaxMetadataFileBytes),
			filePath,
		))
		return nil
	}
	file, err := inventory.fileSystem.Open(filePath)
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, fileErrorDiagnostic(
			"INVENTORY_METADATA_UNAVAILABLE",
			"cannot open inventory metadata",
			filePath,
			err,
		))
		return nil
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, limits.MaxMetadataFileBytes+1))
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, fileErrorDiagnostic(
			"INVENTORY_METADATA_READ_FAILED",
			"cannot read inventory metadata",
			filePath,
			err,
		))
		return nil
	}
	if int64(len(content)) > limits.MaxMetadataFileBytes {
		result.Diagnostics = append(result.Diagnostics, newDiagnostic(
			"INVENTORY_METADATA_TOO_LARGE",
			domain.SeverityWarning,
			fmt.Sprintf("inventory metadata exceeds maximum size %d bytes", limits.MaxMetadataFileBytes),
			filePath,
		))
		return nil
	}
	result.Stats.MetadataFilesRead++
	result.Stats.MetadataBytesRead += int64(len(content))
	return content
}

func (inventory *Inventory) rejectSymlinkPath(basePath, targetPath string) *domain.Diagnostic {
	relative, err := filepath.Rel(basePath, targetPath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		diagnostic := newDiagnostic(
			"INVENTORY_METADATA_OUTSIDE_RESOURCE",
			domain.SeverityError,
			"metadata path is outside its resource",
			targetPath,
		)
		return &diagnostic
	}
	current := basePath
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "." || component == "" {
			continue
		}
		current = filepath.Join(current, component)
		info, err := inventory.fileSystem.Lstat(current)
		if errors.Is(err, fs.ErrNotExist) {
			diagnostic := newDiagnostic(
				"INVENTORY_METADATA_NOT_FOUND",
				domain.SeverityInfo,
				"optional inventory metadata was not found",
				current,
			)
			return &diagnostic
		}
		if err != nil {
			diagnostic := fileErrorDiagnostic(
				"INVENTORY_METADATA_UNAVAILABLE",
				"cannot inspect inventory metadata path",
				current,
				err,
			)
			return &diagnostic
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			diagnostic := newDiagnostic(
				"INVENTORY_METADATA_SYMLINK_SKIPPED",
				domain.SeverityInfo,
				"symbolic link in metadata path was not followed",
				current,
			)
			return &diagnostic
		}
	}
	return nil
}

func (inventory *Inventory) inspectCandidateDirectory(
	path string,
	limits Limits,
	result *Result,
) (fs.FileInfo, bool) {
	if result.Stats.CandidatesInspected >= limits.MaxCandidates {
		result.Diagnostics = append(result.Diagnostics, newDiagnostic(
			"INVENTORY_MAX_CANDIDATES_REACHED",
			domain.SeverityWarning,
			fmt.Sprintf("maximum inventory candidate budget %d reached", limits.MaxCandidates),
			path,
		))
		return nil, false
	}
	result.Stats.CandidatesInspected++
	info, err := inventory.fileSystem.Lstat(path)
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, fileErrorDiagnostic(
			"INVENTORY_CANDIDATE_UNAVAILABLE",
			"cannot inspect inventory candidate",
			path,
			err,
		))
		return nil, false
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		result.Diagnostics = append(result.Diagnostics, newDiagnostic(
			"INVENTORY_SYMLINK_CANDIDATE_SKIPPED",
			domain.SeverityInfo,
			"symbolic link candidate was not followed",
			path,
		))
		return nil, false
	}
	if !info.IsDir() {
		return nil, false
	}
	return info, true
}

func appendMaxCandidatesDiagnostic(path string, limits Limits, result *Result) {
	result.Diagnostics = append(result.Diagnostics, newDiagnostic(
		"INVENTORY_MAX_CANDIDATES_REACHED",
		domain.SeverityWarning,
		fmt.Sprintf("maximum inventory candidate budget %d reached", limits.MaxCandidates),
		path,
	))
}

func appendMaxResourcesDiagnostic(path string, limits Limits, result *Result) {
	result.Diagnostics = append(result.Diagnostics, newDiagnostic(
		"INVENTORY_MAX_RESOURCES_REACHED",
		domain.SeverityWarning,
		fmt.Sprintf("maximum inventory resource budget %d reached", limits.MaxResources),
		path,
	))
}

func (inventory *Inventory) readDirectory(path string, result *Result) []fs.DirEntry {
	entries, err := inventory.fileSystem.ReadDir(path)
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, fileErrorDiagnostic(
			"INVENTORY_DIRECTORY_UNAVAILABLE",
			"cannot list inventory directory",
			path,
			err,
		))
		return nil
	}
	sort.Slice(entries, func(left, right int) bool {
		return entries[left].Name() < entries[right].Name()
	})
	return entries
}

func isWithin(parent, candidate string) bool {
	relative, err := filepath.Rel(parent, candidate)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

type resourceBuilder struct {
	items map[string]*domain.InstalledResource
}

func newResourceBuilder() *resourceBuilder {
	return &resourceBuilder{items: make(map[string]*domain.InstalledResource)}
}

func (builder *resourceBuilder) len() int {
	return len(builder.items)
}

func (builder *resourceBuilder) add(
	ecosystem string,
	component string,
	version *string,
	path string,
	size *int64,
	metadata []domain.MetadataEntry,
	warnings []string,
) {
	path = filepath.Clean(path)
	key := strings.Join([]string{ecosystem, component, path}, "\x00")
	resource, exists := builder.items[key]
	if !exists {
		resource = &domain.InstalledResource{
			ID:              stableResourceID(key),
			Ecosystem:       ecosystem,
			Component:       component,
			Version:         copyStringPointer(version),
			Path:            path,
			SizeBytes:       copyInt64Pointer(size),
			ReferenceStatus: domain.ReferenceUnknown,
			Metadata:        []domain.MetadataEntry{},
			Warnings:        []string{},
		}
		builder.items[key] = resource
	}
	if resource.Version == nil && version != nil {
		resource.Version = copyStringPointer(version)
	}
	if resource.SizeBytes == nil && size != nil {
		resource.SizeBytes = copyInt64Pointer(size)
	}
	for _, entry := range metadata {
		if !containsMetadata(resource.Metadata, entry) {
			resource.Metadata = append(resource.Metadata, entry)
		}
	}
	for _, warning := range warnings {
		if warning != "" && !containsString(resource.Warnings, warning) {
			resource.Warnings = append(resource.Warnings, warning)
		}
	}
}

func (builder *resourceBuilder) resources() []domain.InstalledResource {
	resources := make([]domain.InstalledResource, 0, len(builder.items))
	for _, resource := range builder.items {
		sort.Slice(resource.Metadata, func(left, right int) bool {
			leftKey := resource.Metadata[left].Key + "\x00" + resource.Metadata[left].Value
			rightKey := resource.Metadata[right].Key + "\x00" + resource.Metadata[right].Value
			return leftKey < rightKey
		})
		sort.Strings(resource.Warnings)
		resources = append(resources, *resource)
	}
	sort.Slice(resources, func(left, right int) bool {
		leftKey := strings.Join([]string{
			resources[left].Ecosystem,
			resources[left].Component,
			pointerValue(resources[left].Version),
			resources[left].Path,
		}, "\x00")
		rightKey := strings.Join([]string{
			resources[right].Ecosystem,
			resources[right].Component,
			pointerValue(resources[right].Version),
			resources[right].Path,
		}, "\x00")
		return leftKey < rightKey
	})
	return resources
}

func stableResourceID(key string) string {
	digest := sha256.Sum256([]byte(key))
	return "resource-" + hex.EncodeToString(digest[:8])
}

func containsMetadata(entries []domain.MetadataEntry, expected domain.MetadataEntry) bool {
	for _, entry := range entries {
		if entry == expected {
			return true
		}
	}
	return false
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func copyStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func copyInt64Pointer(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func pointerValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func newDiagnostic(
	code string,
	severity domain.DiagnosticSeverity,
	message string,
	diagnosticPath string,
) domain.Diagnostic {
	diagnostic := domain.Diagnostic{
		Code:     code,
		Severity: severity,
		Scope:    "inventory",
		Message:  message,
	}
	if diagnosticPath != "" {
		pathCopy := diagnosticPath
		diagnostic.Path = &pathCopy
	}
	return diagnostic
}

func fileErrorDiagnostic(code, message, diagnosticPath string, err error) domain.Diagnostic {
	if errors.Is(err, fs.ErrNotExist) {
		message += ": path does not exist"
	} else if errors.Is(err, fs.ErrPermission) {
		message += ": permission denied"
	} else {
		message += ": " + err.Error()
	}
	return newDiagnostic(code, domain.SeverityWarning, message, diagnosticPath)
}

func sortDiagnostics(diagnostics []domain.Diagnostic) {
	sort.Slice(diagnostics, func(left, right int) bool {
		leftPath := ""
		if diagnostics[left].Path != nil {
			leftPath = *diagnostics[left].Path
		}
		rightPath := ""
		if diagnostics[right].Path != nil {
			rightPath = *diagnostics[right].Path
		}
		leftKey := strings.Join([]string{diagnostics[left].Scope, diagnostics[left].Code, leftPath, diagnostics[left].Message}, "\x00")
		rightKey := strings.Join([]string{diagnostics[right].Scope, diagnostics[right].Code, rightPath, diagnostics[right].Message}, "\x00")
		return leftKey < rightKey
	})
}

func metadataEntry(key, value string) domain.MetadataEntry {
	return domain.MetadataEntry{Key: key, Value: value}
}

func integerString(value int) string {
	return strconv.Itoa(value)
}
