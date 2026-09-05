package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"dev-environment-auditor/internal/application"
	"dev-environment-auditor/internal/autodetect"
	"dev-environment-auditor/internal/classification"
	"dev-environment-auditor/internal/domain"
	"dev-environment-auditor/internal/planning"
	"dev-environment-auditor/internal/report"
)

var version = "0.2.0-dev"

const (
	defaultScanTimeout   = 30 * time.Second
	maxInputReportBytes  = 64 << 20
	exitSuccess          = 0
	exitPartial          = 1
	exitUsage            = 2
	exitOperationalError = 3
)

type commandDependencies struct {
	scan         func(context.Context, application.Config) domain.ScanDocument
	detect       func(context.Context, autodetect.Config) autodetect.Result
	readFile     func(string) ([]byte, error)
	writeFile    func(string, []byte) error
	writeNewFile func(string, []byte) error
}

type stringListFlag []string

func (values *stringListFlag) String() string {
	return strings.Join(*values, ",")
}

func (values *stringListFlag) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("value cannot be empty")
	}
	*values = append(*values, value)
	return nil
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(arguments []string, stdout, stderr io.Writer) int {
	scanner := application.NewScanner(application.SystemClock{}, version)
	detector := autodetect.New()
	return runWithDependencies(arguments, stdout, stderr, commandDependencies{
		scan:         scanner.Scan,
		detect:       detector.Detect,
		readFile:     readReportFile,
		writeFile:    writePrivateFile,
		writeNewFile: writeNewPrivateFile,
	})
}

func runWithDependencies(
	arguments []string,
	stdout io.Writer,
	stderr io.Writer,
	dependencies commandDependencies,
) int {
	if len(arguments) == 0 {
		printRootUsage(stderr)
		return exitUsage
	}
	switch arguments[0] {
	case "version":
		if len(arguments) != 1 {
			fmt.Fprintln(stderr, "version does not accept arguments")
			return exitUsage
		}
		fmt.Fprintln(stdout, version)
		return exitSuccess
	case "scan":
		return runScan(arguments[1:], stdout, stderr, dependencies)
	case "explain":
		return runExplain(arguments[1:], stdout, stderr, dependencies)
	case "plan":
		return runPlan(arguments[1:], stdout, stderr, dependencies)
	case "help", "-h", "--help":
		printRootUsage(stdout)
		return exitSuccess
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n", arguments[0])
		printRootUsage(stderr)
		return exitUsage
	}
}

func runPlan(
	arguments []string,
	stdout io.Writer,
	stderr io.Writer,
	dependencies commandDependencies,
) int {
	flags := flag.NewFlagSet("plan", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		printPlanUsage(stderr)
		flags.PrintDefaults()
	}
	var reportPath string
	var outputPath string
	var format string
	var resourceIDs stringListFlag
	flags.StringVar(&reportPath, "report", "", "JSON v1 scan report to read")
	flags.Var(&resourceIDs, "resource", "resource ID to include manually; repeatable")
	flags.StringVar(&format, "format", "terminal", "output format: terminal or json")
	flags.StringVar(&outputPath, "output", "", "new output file; never overwritten; '-' means stdout")
	if err := flags.Parse(arguments); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitSuccess
		}
		return exitUsage
	}
	if strings.TrimSpace(reportPath) == "" || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "plan requires --report FILE and accepts flags only")
		return exitUsage
	}
	format = strings.ToLower(strings.TrimSpace(format))
	if format != "terminal" && format != "json" {
		fmt.Fprintln(stderr, "--format must be terminal or json")
		return exitUsage
	}
	if outputPath != "" && outputPath != "-" && sameCleanPath(reportPath, outputPath) {
		fmt.Fprintln(stderr, "--output must not overwrite the input report")
		return exitUsage
	}

	content, err := dependencies.readFile(reportPath)
	if err != nil {
		fmt.Fprintf(stderr, "cannot read scan report: %v\n", err)
		return exitOperationalError
	}
	document, err := report.DecodeJSON(content)
	if err != nil {
		fmt.Fprintf(stderr, "invalid scan report: %v\n", err)
		return exitOperationalError
	}
	digest := sha256.Sum256(content)
	plan, err := planning.Build(document, planning.Config{
		ToolVersion:        version,
		SourceReportSHA256: hex.EncodeToString(digest[:]),
		ResourceIDs:        []string(resourceIDs),
	})
	if err != nil {
		fmt.Fprintf(stderr, "cannot build cleanup plan: %v\n", err)
		return exitOperationalError
	}
	var payload []byte
	if format == "json" {
		payload, err = report.RenderPlanJSON(plan)
	} else {
		payload, err = report.RenderPlanTerminal(plan)
	}
	if err != nil {
		fmt.Fprintf(stderr, "cannot render cleanup plan: %v\n", err)
		return exitOperationalError
	}
	if outputPath != "" && outputPath != "-" && dependencies.writeNewFile == nil {
		fmt.Fprintln(stderr, "cannot write immutable cleanup plan: new-file writer is unavailable")
		return exitOperationalError
	}
	if err := writePayload(stdout, outputPath, payload, dependencies.writeNewFile); err != nil {
		fmt.Fprintf(stderr, "cannot write immutable cleanup plan: %v\n", err)
		return exitOperationalError
	}
	if plan.Selection.Mode == domain.SelectionExplicitResourceIDs && len(plan.Excluded) > 0 {
		return exitPartial
	}
	return exitSuccess
}

func runScan(
	arguments []string,
	stdout io.Writer,
	stderr io.Writer,
	dependencies commandDependencies,
) int {
	flags := flag.NewFlagSet("scan", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		printScanUsage(stderr)
		flags.PrintDefaults()
	}
	var projectRoots stringListFlag
	var exclusions stringListFlag
	var androidRoots stringListFlag
	var androidAVDRoots stringListFlag
	var flutterRoots stringListFlag
	var fvmRoots stringListFlag
	var gradleRoots stringListFlag
	var jdkRoots stringListFlag
	var format string
	var outputPath string
	var timeout time.Duration
	var autoDetect bool
	var deepSearch bool
	var dockerInventory bool
	var oldAfterDays int
	flags.Var(&projectRoots, "root", "project search root; repeatable; auto-detected when omitted")
	flags.Var(&exclusions, "exclude", "relative path or directory-name exclusion; repeatable")
	flags.Var(&androidRoots, "android-sdk-root", "Android SDK inventory root; repeatable")
	flags.Var(&androidAVDRoots, "android-avd-root", "Android AVD inventory root; repeatable")
	flags.Var(&flutterRoots, "flutter-sdk-root", "direct Flutter SDK root; repeatable")
	flags.Var(&fvmRoots, "fvm-cache-root", "FVM cache root whose children are SDKs; repeatable")
	flags.Var(&gradleRoots, "gradle-user-home", "Gradle user-home inventory root; repeatable")
	flags.Var(&jdkRoots, "jdk-root", "JDK home or macOS JDK container root; repeatable")
	flags.StringVar(&format, "format", "terminal", "output format: terminal or json")
	flags.StringVar(&outputPath, "output", "", "explicit output file; '-' means stdout")
	flags.DurationVar(&timeout, "timeout", defaultScanTimeout, "maximum total scan duration")
	flags.BoolVar(&autoDetect, "auto-detect", true, "detect omitted project and inventory roots without executing tools")
	flags.BoolVar(&deepSearch, "deep-search", true, "run a bounded search of conventional development locations")
	flags.BoolVar(&dockerInventory, "docker-inventory", true, "observe local Docker images, containers, builders and BuildKit cache when available")
	flags.IntVar(&oldAfterDays, "old-after-days", classification.DefaultOldAfterDays, "classify trusted last-use observations as old after this many days")
	if err := flags.Parse(arguments); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitSuccess
		}
		return exitUsage
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "scan accepts flags only")
		return exitUsage
	}
	if timeout <= 0 {
		fmt.Fprintln(stderr, "--timeout must be greater than zero")
		return exitUsage
	}
	if oldAfterDays <= 0 {
		fmt.Fprintln(stderr, "--old-after-days must be greater than zero")
		return exitUsage
	}
	format = strings.ToLower(strings.TrimSpace(format))
	if format != "terminal" && format != "json" {
		fmt.Fprintln(stderr, "--format must be terminal or json")
		return exitUsage
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	preScanDiagnostics := []domain.Diagnostic{}
	heuristicInventoryFamilies := []string{}
	projectDiscoveryHeuristic := false
	if autoDetect && dependencies.detect != nil {
		needFlutterFamily := len(flutterRoots)+len(fvmRoots) == 0
		detected := dependencies.detect(ctx, autodetect.Config{
			NeedProjectRoots: len(projectRoots) == 0,
			NeedAndroid:      len(androidRoots) == 0,
			NeedAndroidAVD:   len(androidAVDRoots) == 0,
			NeedFlutter:      needFlutterFamily,
			NeedFVM:          needFlutterFamily,
			NeedGradle:       len(gradleRoots) == 0,
			NeedJDK:          len(jdkRoots) == 0,
			DeepSearch:       deepSearch,
		})
		preScanDiagnostics = append(preScanDiagnostics, detected.Diagnostics...)
		if len(projectRoots) == 0 {
			projectRoots = append(projectRoots, detected.Roots.ProjectRoots...)
			projectDiscoveryHeuristic = detected.ProjectDiscoveryHeuristic
		}
		if len(androidRoots) == 0 {
			androidRoots = append(androidRoots, detected.Roots.AndroidSDKRoots...)
		}
		if len(androidAVDRoots) == 0 {
			androidAVDRoots = append(androidAVDRoots, detected.Roots.AndroidAVDRoots...)
		}
		if needFlutterFamily {
			flutterRoots = append(flutterRoots, detected.Roots.FlutterSDKRoots...)
			fvmRoots = append(fvmRoots, detected.Roots.FVMCacheRoots...)
		}
		if len(gradleRoots) == 0 {
			gradleRoots = append(gradleRoots, detected.Roots.GradleUserHomeRoots...)
		}
		if len(jdkRoots) == 0 {
			jdkRoots = append(jdkRoots, detected.Roots.JDKRoots...)
		}
		for _, family := range detected.HeuristicInventoryFamilies {
			heuristicInventoryFamilies = append(heuristicInventoryFamilies, string(family))
		}
	}
	if len(projectRoots) == 0 {
		if autoDetect {
			fmt.Fprintln(stderr, "no project root was provided or detected; pass --root PATH")
		} else {
			fmt.Fprintln(stderr, "scan requires at least one --root when --auto-detect=false")
		}
		return exitUsage
	}

	document := dependencies.scan(ctx, application.Config{
		ProjectRoots:               []string(projectRoots),
		Exclusions:                 []string(exclusions),
		AndroidSDKRoots:            []string(androidRoots),
		AndroidAVDRoots:            []string(androidAVDRoots),
		FlutterSDKRoots:            []string(flutterRoots),
		FVMCacheRoots:              []string(fvmRoots),
		GradleUserHomeRoots:        []string(gradleRoots),
		JDKRoots:                   []string(jdkRoots),
		DockerInventory:            dockerInventory,
		OldAfterDays:               oldAfterDays,
		PreScanDiagnostics:         preScanDiagnostics,
		ProjectDiscoveryHeuristic:  projectDiscoveryHeuristic,
		HeuristicInventoryFamilies: heuristicInventoryFamilies,
	})

	var payload []byte
	var err error
	if format == "json" {
		payload, err = report.RenderJSON(document)
	} else {
		payload, err = report.RenderTerminal(document)
	}
	if err != nil {
		fmt.Fprintf(stderr, "cannot render scan report: %v\n", err)
		return exitOperationalError
	}
	if err := writePayload(stdout, outputPath, payload, dependencies.writeFile); err != nil {
		fmt.Fprintf(stderr, "cannot write scan report: %v\n", err)
		return exitOperationalError
	}
	if hasErrorDiagnostic(document.Diagnostics) {
		return exitPartial
	}
	return exitSuccess
}

func runExplain(
	arguments []string,
	stdout io.Writer,
	stderr io.Writer,
	dependencies commandDependencies,
) int {
	flags := flag.NewFlagSet("explain", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		printExplainUsage(stderr)
		flags.PrintDefaults()
	}
	var reportPath string
	var outputPath string
	flags.StringVar(&reportPath, "report", "", "JSON v1 scan report to read")
	flags.StringVar(&outputPath, "output", "", "explicit output file; '-' means stdout")
	if err := flags.Parse(arguments); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitSuccess
		}
		return exitUsage
	}
	if strings.TrimSpace(reportPath) == "" || flags.NArg() != 1 {
		fmt.Fprintln(stderr, "explain requires --report FILE and exactly one project, requirement, or resource ID")
		return exitUsage
	}
	if outputPath != "" && outputPath != "-" && sameCleanPath(reportPath, outputPath) {
		fmt.Fprintln(stderr, "--output must not overwrite the input report")
		return exitUsage
	}

	content, err := dependencies.readFile(reportPath)
	if err != nil {
		fmt.Fprintf(stderr, "cannot read scan report: %v\n", err)
		return exitOperationalError
	}
	document, err := report.DecodeJSON(content)
	if err != nil {
		fmt.Fprintf(stderr, "invalid scan report: %v\n", err)
		return exitOperationalError
	}
	payload, found, err := report.Explain(document, flags.Arg(0))
	if err != nil {
		fmt.Fprintf(stderr, "cannot explain report item: %v\n", err)
		return exitOperationalError
	}
	if !found {
		fmt.Fprintf(stderr, "report item %q was not found\n", flags.Arg(0))
		return exitPartial
	}
	if err := writePayload(stdout, outputPath, payload, dependencies.writeFile); err != nil {
		fmt.Fprintf(stderr, "cannot write explanation: %v\n", err)
		return exitOperationalError
	}
	return exitSuccess
}

func writePayload(
	stdout io.Writer,
	outputPath string,
	payload []byte,
	writeFile func(string, []byte) error,
) error {
	if outputPath != "" && outputPath != "-" {
		return writeFile(outputPath, payload)
	}
	_, err := stdout.Write(payload)
	return err
}

func readReportFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maxInputReportBytes+1))
	if err != nil {
		return nil, err
	}
	if len(content) > maxInputReportBytes {
		return nil, fmt.Errorf("report exceeds %d bytes", maxInputReportBytes)
	}
	return content, nil
}

func writePrivateFile(path string, content []byte) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("output path cannot be empty")
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("refusing to write through an output symlink")
		}
		if !info.Mode().IsRegular() {
			return errors.New("output path is not a regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func writeNewPrivateFile(path string, content []byte) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("output path cannot be empty")
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return errors.New("output already exists; immutable plans are never overwritten")
		}
		return err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func sameCleanPath(left, right string) bool {
	leftInfo, leftStatErr := os.Stat(left)
	rightInfo, rightStatErr := os.Stat(right)
	if leftStatErr == nil && rightStatErr == nil && os.SameFile(leftInfo, rightInfo) {
		return true
	}
	leftPath, leftErr := filepath.Abs(left)
	rightPath, rightErr := filepath.Abs(right)
	return leftErr == nil && rightErr == nil && filepath.Clean(leftPath) == filepath.Clean(rightPath)
}

func hasErrorDiagnostic(diagnostics []domain.Diagnostic) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == domain.SeverityError {
			return true
		}
	}
	return false
}

func printRootUsage(output io.Writer) {
	fmt.Fprintln(output, "Usage:")
	fmt.Fprintln(output, "  dev-audit scan [--root PATH] [options]")
	fmt.Fprintln(output, "  dev-audit explain --report FILE ID [--output FILE]")
	fmt.Fprintln(output, "  dev-audit plan --report FILE [--resource ID ...] [options]")
	fmt.Fprintln(output, "  dev-audit version")
}

func printScanUsage(output io.Writer) {
	fmt.Fprintln(output, "Usage: dev-audit scan [--root PATH ...] [options]")
	fmt.Fprintln(output, "Omitted roots are detected locally; use --auto-detect=false for explicit-only mode.")
}

func printExplainUsage(output io.Writer) {
	fmt.Fprintln(output, "Usage: dev-audit explain --report FILE ID [--output FILE]")
}

func printPlanUsage(output io.Writer) {
	fmt.Fprintln(output, "Usage: dev-audit plan --report FILE [--resource ID ...] [--format terminal|json] [--output NEW_FILE]")
	fmt.Fprintln(output, "Without --resource, only conservative default-safe candidates are selected. No command is executed.")
}
