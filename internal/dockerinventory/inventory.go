// Package dockerinventory observes local Docker daemon storage through a small
// allowlist of read-only CLI commands. It never reads container commands,
// labels, environment variables, mounts, or file contents.
package dockerinventory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"dev-environment-auditor/internal/domain"
)

const (
	DefaultCommandTimeout = 5 * time.Second
	DefaultMaxOutputBytes = 8 << 20
	DefaultMaxResources   = 2_000
)

type Config struct {
	CommandTimeout time.Duration
	MaxOutputBytes int64
	MaxResources   int
}

type Stats struct {
	CommandsRun    int
	ResourcesFound int
	OutputBytes    int64
}

type Result struct {
	Resources   []domain.InstalledResource
	Diagnostics []domain.Diagnostic
	Stats       Stats
}

type Inspector struct {
	runner Runner
}

func New(runner Runner) *Inspector {
	return &Inspector{runner: runner}
}

func (inspector *Inspector) Inspect(ctx context.Context, config Config) Result {
	result := Result{Resources: []domain.InstalledResource{}, Diagnostics: []domain.Diagnostic{}}
	if inspector == nil || inspector.runner == nil {
		result.Diagnostics = append(result.Diagnostics, diagnostic(
			"DOCKER_INVENTORY_RUNNER_UNAVAILABLE",
			domain.SeverityError,
			"Docker inventory requires a command runner",
		))
		return result
	}
	config = normalizeConfig(config)
	dockerPath, err := inspector.runner.LookPath("docker")
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, diagnostic(
			"DOCKER_CLIENT_NOT_FOUND",
			domain.SeverityInfo,
			"Docker CLI was not found; local Docker inventory was skipped",
		))
		return result
	}

	versionResult := inspector.run(ctx, dockerPath, []string{"version", "--format", "{{json .}}"}, config, &result)
	clientVersion, serverVersion, versionOK := parseDockerVersion(versionResult.Stdout)
	if clientVersion != "" {
		result.Diagnostics = append(result.Diagnostics, diagnostic(
			"DOCKER_CLIENT_FOUND",
			domain.SeverityInfo,
			fmt.Sprintf("Docker CLI %s was found", clientVersion),
		))
	}
	if ctx.Err() != nil {
		result.Diagnostics = append(result.Diagnostics, diagnostic(
			"DOCKER_INVENTORY_CANCELLED",
			domain.SeverityError,
			ctx.Err().Error(),
		))
		return finalize(result)
	}
	if versionResult.OutputLimited {
		result.Diagnostics = append(result.Diagnostics, outputLimitDiagnostic("docker version", config.MaxOutputBytes))
		return finalize(result)
	}
	if versionResult.Err != nil || !versionOK || serverVersion == "" {
		result.Diagnostics = append(result.Diagnostics, diagnostic(
			"DOCKER_DAEMON_UNAVAILABLE",
			domain.SeverityInfo,
			"Docker daemon is unavailable; daemon resources were not inventoried",
		))
		return finalize(result)
	}
	result.Diagnostics = append(result.Diagnostics, diagnostic(
		"DOCKER_DAEMON_CONNECTED",
		domain.SeverityInfo,
		fmt.Sprintf("Docker daemon %s answered read-only inventory requests", serverVersion),
	))

	commands := []struct {
		label     string
		arguments []string
		parse     func([]byte, int) ([]domain.InstalledResource, []domain.Diagnostic)
	}{
		{
			label: "docker image ls",
			arguments: []string{
				"image", "ls", "--all", "--digests", "--no-trunc", "--format",
				"{{json .ID}}\t{{json .Repository}}\t{{json .Tag}}\t{{json .Digest}}\t{{json .CreatedAt}}\t{{json .Size}}",
			},
			parse: parseImages,
		},
		{
			label: "docker container ls",
			arguments: []string{
				"container", "ls", "--all", "--no-trunc", "--size", "--format",
				"{{json .ID}}\t{{json .Image}}\t{{json .Names}}\t{{json .State}}\t{{json .Status}}\t{{json .CreatedAt}}\t{{json .Size}}",
			},
			parse: parseContainers,
		},
		{
			label: "docker buildx ls",
			arguments: []string{
				"buildx", "ls", "--no-trunc", "--format",
				"{{json .Name}}\t{{json .DriverEndpoint}}\t{{json .Status}}\t{{json .Buildkit}}\t{{json .Platforms}}\t{{json .LastActivity}}\t{{json .Builder.Name}}",
			},
			parse: parseBuilders,
		},
		{
			label: "docker buildx du",
			arguments: []string{
				"buildx", "du", "--format=json",
			},
			parse: parseBuildCache,
		},
	}

	remaining := config.MaxResources
	for _, command := range commands {
		if ctx.Err() != nil {
			result.Diagnostics = append(result.Diagnostics, diagnostic(
				"DOCKER_INVENTORY_CANCELLED",
				domain.SeverityError,
				ctx.Err().Error(),
			))
			break
		}
		if remaining <= 0 {
			result.Diagnostics = append(result.Diagnostics, diagnostic(
				"DOCKER_INVENTORY_MAX_RESOURCES_REACHED",
				domain.SeverityWarning,
				fmt.Sprintf("maximum Docker resource budget %d reached", config.MaxResources),
			))
			break
		}
		commandResult := inspector.run(ctx, dockerPath, command.arguments, config, &result)
		if commandResult.TimedOut {
			result.Diagnostics = append(result.Diagnostics, diagnostic(
				"DOCKER_INVENTORY_COMMAND_TIMEOUT",
				domain.SeverityWarning,
				command.label+" exceeded its execution timeout",
			))
			continue
		}
		if commandResult.OutputLimited {
			result.Diagnostics = append(result.Diagnostics, outputLimitDiagnostic(command.label, config.MaxOutputBytes))
			continue
		}
		if commandResult.Err != nil {
			result.Diagnostics = append(result.Diagnostics, diagnostic(
				"DOCKER_INVENTORY_COMMAND_FAILED",
				domain.SeverityWarning,
				command.label+" failed; this part of Docker inventory is incomplete",
			))
			continue
		}
		resources, diagnostics := command.parse(commandResult.Stdout, remaining)
		result.Resources = append(result.Resources, resources...)
		result.Diagnostics = append(result.Diagnostics, diagnostics...)
		remaining -= len(resources)
	}
	return finalize(result)
}

func (inspector *Inspector) run(
	ctx context.Context,
	name string,
	arguments []string,
	config Config,
	result *Result,
) CommandResult {
	commandContext, cancel := context.WithTimeout(ctx, config.CommandTimeout)
	defer cancel()
	commandResult := inspector.runner.Run(commandContext, name, append([]string(nil), arguments...), config.MaxOutputBytes)
	if errors.Is(commandContext.Err(), context.DeadlineExceeded) {
		commandResult.TimedOut = true
	}
	result.Stats.CommandsRun++
	result.Stats.OutputBytes += int64(len(commandResult.Stdout) + len(commandResult.Stderr))
	return commandResult
}

type dockerVersion struct {
	Client *struct {
		Version string `json:"Version"`
	} `json:"Client"`
	Server *struct {
		Version string `json:"Version"`
	} `json:"Server"`
}

func parseDockerVersion(content []byte) (string, string, bool) {
	var value dockerVersion
	if err := json.Unmarshal(content, &value); err != nil {
		return "", "", false
	}
	client := ""
	server := ""
	if value.Client != nil {
		client = safeShortValue(value.Client.Version)
	}
	if value.Server != nil {
		server = safeShortValue(value.Server.Version)
	}
	return client, server, client != ""
}

type imageObservation struct {
	id         string
	references map[string]struct{}
	digests    map[string]struct{}
	createdAt  string
	size       *int64
}

func parseImages(content []byte, limit int) ([]domain.InstalledResource, []domain.Diagnostic) {
	observations := make(map[string]*imageObservation)
	diagnostics := []domain.Diagnostic{}
	for _, line := range nonEmptyLines(content) {
		fields, ok := parseJSONFields(line, 6)
		if !ok || !isDockerObjectID(fields[0]) {
			diagnostics = appendOnce(diagnostics, diagnostic(
				"DOCKER_IMAGE_OUTPUT_MALFORMED",
				domain.SeverityWarning,
				"Docker image output contained a malformed row",
			))
			continue
		}
		observation := observations[fields[0]]
		if observation == nil {
			observation = &imageObservation{id: fields[0], references: make(map[string]struct{}), digests: make(map[string]struct{})}
			observations[fields[0]] = observation
		}
		if reference := dockerImageReference(fields[1], fields[2]); reference != "" {
			observation.references[reference] = struct{}{}
		}
		if fields[3] != "" && fields[3] != "<none>" && safeReportValue(fields[3]) {
			observation.digests[fields[3]] = struct{}{}
		}
		if observation.createdAt == "" && safeReportValue(fields[4]) {
			observation.createdAt = fields[4]
		}
		if observation.size == nil {
			observation.size = parseDockerSize(fields[5])
		}
	}

	ids := sortedMapKeys(observations)
	resources := make([]domain.InstalledResource, 0, minInt(len(ids), limit))
	for _, id := range ids {
		if len(resources) >= limit {
			diagnostics = appendOnce(diagnostics, maxResourcesDiagnostic(limit))
			break
		}
		observation := observations[id]
		metadata := []domain.MetadataEntry{
			entry("location_kind", "docker_object"),
			entry("size_metric", "docker_image_virtual_size"),
			entry("management", "docker image"),
		}
		for _, reference := range sortedMapKeys(observation.references) {
			metadata = append(metadata, entry("reference", reference))
		}
		for _, digest := range sortedMapKeys(observation.digests) {
			metadata = append(metadata, entry("digest", digest))
		}
		if observation.createdAt != "" {
			metadata = append(metadata, entry("created_at", observation.createdAt))
		}
		var version *string
		if references := sortedMapKeys(observation.references); len(references) > 0 {
			value := references[0]
			version = &value
		}
		resources = append(resources, resource(
			"docker_image",
			observation.id,
			version,
			observation.size,
			metadata,
			[]string{"Docker image size can share layers with other images and must not be summed as uniquely reclaimable space"},
		))
	}
	return resources, diagnostics
}

func parseContainers(content []byte, limit int) ([]domain.InstalledResource, []domain.Diagnostic) {
	resources := []domain.InstalledResource{}
	diagnostics := []domain.Diagnostic{}
	for _, line := range nonEmptyLines(content) {
		if len(resources) >= limit {
			diagnostics = appendOnce(diagnostics, maxResourcesDiagnostic(limit))
			break
		}
		fields, ok := parseJSONFields(line, 7)
		if !ok || !isDockerObjectID(fields[0]) {
			diagnostics = appendOnce(diagnostics, diagnostic(
				"DOCKER_CONTAINER_OUTPUT_MALFORMED",
				domain.SeverityWarning,
				"Docker container output contained a malformed row",
			))
			continue
		}
		metadata := []domain.MetadataEntry{
			entry("location_kind", "docker_object"),
			entry("size_metric", "docker_container_writable_layer"),
			entry("sensitivity", "sensitive_mutable_data"),
			entry("management", "docker container"),
		}
		for index, key := range []string{"image", "name", "state", "status", "created_at"} {
			value := fields[index+1]
			if value != "" && safeReportValue(value) {
				metadata = append(metadata, entry(key, value))
			}
		}
		resources = append(resources, resource(
			"docker_container",
			fields[0],
			nil,
			parseContainerWritableSize(fields[6]),
			metadata,
			[]string{
				"container writable layers may contain mutable data and are sensitive",
				"container state and size do not imply that removal is safe",
			},
		))
	}
	return resources, diagnostics
}

func parseBuilders(content []byte, limit int) ([]domain.InstalledResource, []domain.Diagnostic) {
	diagnostics := []domain.Diagnostic{}
	type builderRow struct {
		name           string
		driverEndpoint string
		status         string
		buildkit       string
		platforms      string
		lastActivity   string
		parent         string
	}
	rows := []builderRow{}
	for _, line := range nonEmptyLines(content) {
		fields, ok := parseJSONFields(line, 7)
		if !ok {
			diagnostics = appendOnce(diagnostics, diagnostic(
				"DOCKER_BUILDER_OUTPUT_MALFORMED",
				domain.SeverityWarning,
				"Docker builder output contained a malformed row",
			))
			continue
		}
		rows = append(rows, builderRow{
			name:           strings.TrimSuffix(fields[0], "*"),
			driverEndpoint: fields[1],
			status:         fields[2],
			buildkit:       fields[3],
			platforms:      fields[4],
			lastActivity:   fields[5],
			parent:         strings.TrimSuffix(fields[6], "*"),
		})
	}
	type builderObservation struct {
		metadata []domain.MetadataEntry
		buildkit string
	}
	observations := make(map[string]*builderObservation)
	for _, row := range rows {
		if !isSafeDockerName(row.name) || !isDockerDriver(row.driverEndpoint) {
			continue
		}
		observations[row.name] = &builderObservation{
			metadata: []domain.MetadataEntry{
				entry("location_kind", "docker_object"),
				entry("driver", row.driverEndpoint),
				entry("management", "docker buildx"),
			},
		}
		if row.lastActivity != "" && safeReportValue(row.lastActivity) {
			observations[row.name].metadata = append(observations[row.name].metadata, entry("last_activity", row.lastActivity))
		}
	}
	for _, row := range rows {
		parent := row.parent
		if parent == "" {
			parent = row.name
		}
		observation := observations[parent]
		if observation == nil || isDockerDriver(row.driverEndpoint) {
			continue
		}
		for _, item := range []domain.MetadataEntry{
			entry("node_status", row.status),
			entry("node_buildkit_version", row.buildkit),
			entry("node_platforms", row.platforms),
		} {
			if item.Value != "" && safeReportValue(item.Value) {
				observation.metadata = append(observation.metadata, item)
			}
		}
		if observation.buildkit == "" {
			observation.buildkit = safeShortValue(row.buildkit)
		}
	}

	names := sortedMapKeys(observations)
	resources := make([]domain.InstalledResource, 0, minInt(len(names), limit))
	for _, name := range names {
		if len(resources) >= limit {
			diagnostics = appendOnce(diagnostics, maxResourcesDiagnostic(limit))
			break
		}
		observation := observations[name]
		var version *string
		if value := observation.buildkit; value != "" {
			version = &value
		}
		resources = append(resources, resource("docker_builder", name, version, nil, observation.metadata, nil))
	}
	return resources, diagnostics
}

type buildCacheRow struct {
	ID          string `json:"ID"`
	CreatedAt   string `json:"CreatedAt"`
	LastUsedAt  string `json:"LastUsedAt"`
	Mutable     bool   `json:"Mutable"`
	Reclaimable bool   `json:"Reclaimable"`
	Shared      bool   `json:"Shared"`
	Size        string `json:"Size"`
	Type        string `json:"Type"`
	UsageCount  int    `json:"UsageCount"`
}

func parseBuildCache(content []byte, limit int) ([]domain.InstalledResource, []domain.Diagnostic) {
	resources := []domain.InstalledResource{}
	diagnostics := []domain.Diagnostic{}
	for _, line := range nonEmptyLines(content) {
		if len(resources) >= limit {
			diagnostics = appendOnce(diagnostics, maxResourcesDiagnostic(limit))
			break
		}
		var row buildCacheRow
		if err := json.Unmarshal([]byte(line), &row); err != nil || !isDockerObjectID(row.ID) {
			diagnostics = appendOnce(diagnostics, diagnostic(
				"DOCKER_BUILD_CACHE_OUTPUT_MALFORMED",
				domain.SeverityWarning,
				"Docker BuildKit cache output contained a malformed row",
			))
			continue
		}
		metadata := []domain.MetadataEntry{
			entry("location_kind", "docker_object"),
			entry("size_metric", "docker_buildkit_record_size"),
			entry("reclaimable_by_docker", strconv.FormatBool(row.Reclaimable)),
			entry("shared", strconv.FormatBool(row.Shared)),
			entry("mutable", strconv.FormatBool(row.Mutable)),
			entry("usage_count", strconv.Itoa(row.UsageCount)),
			entry("management", "docker buildx"),
		}
		for key, value := range map[string]string{
			"created_at":         row.CreatedAt,
			"last_used_observed": row.LastUsedAt,
			"cache_type":         row.Type,
		} {
			if value != "" && safeReportValue(value) {
				metadata = append(metadata, entry(key, value))
			}
		}
		warnings := []string{"Docker reclaimable status is an observation and does not authorize cleanup"}
		if row.Shared {
			warnings = append(warnings, "cache storage is shared with another Docker resource")
		}
		if row.Mutable {
			warnings = append(warnings, "cache record is mutable and may change during the scan")
		}
		resources = append(resources, resource(
			"docker_build_cache",
			row.ID,
			nil,
			parseBuildCacheSize(row.Size),
			metadata,
			warnings,
		))
	}
	return resources, diagnostics
}

func parseJSONFields(line string, count int) ([]string, bool) {
	rawFields := strings.Split(line, "\t")
	if len(rawFields) != count {
		return nil, false
	}
	fields := make([]string, count)
	for index, raw := range rawFields {
		if err := json.Unmarshal([]byte(raw), &fields[index]); err != nil {
			return nil, false
		}
	}
	return fields, true
}

func parseContainerWritableSize(value string) *int64 {
	if before, _, found := strings.Cut(value, "("); found {
		value = strings.TrimSpace(before)
	}
	return parseDockerSize(value)
}

func parseExactBytes(value string) *int64 {
	value = strings.TrimSpace(value)
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 0 {
		return nil
	}
	return &parsed
}

func parseBuildCacheSize(value string) *int64 {
	if exact := parseExactBytes(value); exact != nil {
		return exact
	}
	return parseDockerSize(value)
}

func parseDockerSize(value string) *int64 {
	value = strings.TrimSpace(strings.ReplaceAll(value, " ", ""))
	if value == "" {
		return nil
	}
	index := 0
	for index < len(value) && ((value[index] >= '0' && value[index] <= '9') || value[index] == '.') {
		index++
	}
	if index == 0 {
		return nil
	}
	number, err := strconv.ParseFloat(value[:index], 64)
	if err != nil || number < 0 {
		return nil
	}
	unit := strings.ToUpper(value[index:])
	multipliers := map[string]float64{
		"B": 1, "KB": 1e3, "MB": 1e6, "GB": 1e9, "TB": 1e12,
		"KIB": 1 << 10, "MIB": 1 << 20, "GIB": 1 << 30, "TIB": 1 << 40,
	}
	multiplier, exists := multipliers[unit]
	if !exists || number*multiplier > math.MaxInt64 {
		return nil
	}
	result := int64(math.Round(number * multiplier))
	return &result
}

func dockerImageReference(repository, tag string) string {
	if repository == "" || repository == "<none>" || !safeReportValue(repository) {
		return ""
	}
	if tag == "" || tag == "<none>" {
		return repository
	}
	if !safeReportValue(tag) {
		return ""
	}
	return repository + ":" + tag
}

func resource(
	component string,
	objectID string,
	version *string,
	size *int64,
	metadata []domain.MetadataEntry,
	warnings []string,
) domain.InstalledResource {
	key := "docker\x00" + component + "\x00" + objectID
	digest := sha256.Sum256([]byte(key))
	return domain.InstalledResource{
		ID:              "resource-" + hex.EncodeToString(digest[:8]),
		Ecosystem:       "docker",
		Component:       component,
		Version:         version,
		Path:            "docker://" + strings.TrimPrefix(component, "docker_") + "/" + url.PathEscape(objectID),
		SizeBytes:       size,
		ReferenceStatus: domain.ReferenceUnknown,
		Metadata:        sortedMetadata(metadata),
		Warnings:        sortedUniqueStrings(warnings),
	}
}

func normalizeConfig(config Config) Config {
	if config.CommandTimeout <= 0 {
		config.CommandTimeout = DefaultCommandTimeout
	}
	if config.MaxOutputBytes <= 0 {
		config.MaxOutputBytes = DefaultMaxOutputBytes
	}
	if config.MaxResources <= 0 {
		config.MaxResources = DefaultMaxResources
	}
	return config
}

func nonEmptyLines(content []byte) []string {
	lines := []string{}
	for _, line := range strings.Split(string(content), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func isDockerObjectID(value string) bool {
	value = strings.TrimPrefix(value, "sha256:")
	if len(value) < 12 || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') &&
			(character < 'g' || character > 'z') {
			return false
		}
	}
	return true
}

func isSafeDockerName(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || strings.ContainsRune("._-", character) {
			continue
		}
		return false
	}
	return true
}

func isDockerDriver(value string) bool {
	switch value {
	case "docker", "docker-container", "kubernetes", "remote", "cloud":
		return true
	default:
		return false
	}
}

func safeShortValue(value string) string {
	value = strings.TrimSpace(value)
	if len(value) == 0 || len(value) > 80 || !safeReportValue(value) {
		return ""
	}
	return value
}

func safeReportValue(value string) bool {
	if len(value) > 1_024 {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

func finalize(result Result) Result {
	sort.Slice(result.Resources, func(left, right int) bool {
		leftKey := result.Resources[left].Component + "\x00" + result.Resources[left].Path
		rightKey := result.Resources[right].Component + "\x00" + result.Resources[right].Path
		return leftKey < rightKey
	})
	sort.Slice(result.Diagnostics, func(left, right int) bool {
		leftKey := string(result.Diagnostics[left].Severity) + "\x00" + result.Diagnostics[left].Code + "\x00" + result.Diagnostics[left].Message
		rightKey := string(result.Diagnostics[right].Severity) + "\x00" + result.Diagnostics[right].Code + "\x00" + result.Diagnostics[right].Message
		return leftKey < rightKey
	})
	result.Stats.ResourcesFound = len(result.Resources)
	return result
}

func entry(key, value string) domain.MetadataEntry {
	return domain.MetadataEntry{Key: key, Value: value}
}

func sortedMetadata(values []domain.MetadataEntry) []domain.MetadataEntry {
	result := append([]domain.MetadataEntry(nil), values...)
	sort.Slice(result, func(left, right int) bool {
		return result[left].Key+"\x00"+result[left].Value < result[right].Key+"\x00"+result[right].Value
	})
	return result
}

func sortedUniqueStrings(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	unique := result[:0]
	for _, value := range result {
		if value != "" && (len(unique) == 0 || unique[len(unique)-1] != value) {
			unique = append(unique, value)
		}
	}
	return unique
}

func sortedMapKeys[T any](values map[string]T) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}

func appendOnce(diagnostics []domain.Diagnostic, value domain.Diagnostic) []domain.Diagnostic {
	for _, current := range diagnostics {
		if current.Code == value.Code {
			return diagnostics
		}
	}
	return append(diagnostics, value)
}

func diagnostic(code string, severity domain.DiagnosticSeverity, message string) domain.Diagnostic {
	return domain.Diagnostic{
		Code:     code,
		Severity: severity,
		Scope:    "docker-inventory",
		Message:  message,
	}
}

func outputLimitDiagnostic(command string, limit int64) domain.Diagnostic {
	return diagnostic(
		"DOCKER_INVENTORY_OUTPUT_LIMIT_REACHED",
		domain.SeverityWarning,
		fmt.Sprintf("%s exceeded the %d byte output limit", command, limit),
	)
}

func maxResourcesDiagnostic(limit int) domain.Diagnostic {
	return diagnostic(
		"DOCKER_INVENTORY_MAX_RESOURCES_REACHED",
		domain.SeverityWarning,
		fmt.Sprintf("maximum Docker resource budget %d reached", limit),
	)
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
