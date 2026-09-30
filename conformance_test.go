package assembler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// Each pending case is a strict expected failure and leaves this set when it passes.
var pending = map[string]bool{}

func TestConformanceCases(t *testing.T) {
	paths, err := filepath.Glob("vendor/cwa/conformance/cases/*/snapshot.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no vendored cases")
	}
	seen := map[string]bool{}
	for _, path := range paths {
		id := filepath.Base(filepath.Dir(path))
		seen[id] = true
		t.Run(id, func(t *testing.T) {
			got := runCase(filepath.Dir(path), Assemble)
			switch {
			case got.outcome == "skipped":
				t.Skip(got.detail)
			case pending[id] && got.outcome == "passed":
				t.Fatal("pending case now passes; remove it from pending")
			case pending[id]:
			case got.outcome != "passed":
				t.Fatal(got.detail)
			}
		})
	}
	for id := range pending {
		if !seen[id] {
			t.Errorf("pending id %q has no vendored case", id)
		}
	}
}

// caseOutcome is a case's outcome as the README's Reporting results names it: passed or rejected,
// failed, or skipped, with detail saying why for the last two.
type caseOutcome struct{ outcome, detail string }

// assembleFunc is Assemble, or a stand-in a test uses to answer as a port without a component.
type assembleFunc func(raw []byte, options Options) (Result, error)

// runCase runs the case in directory as the README's Running a case describes.
func runCase(directory string, assemble assembleFunc) caseOutcome {
	raw, err := os.ReadFile(filepath.Join(directory, "snapshot.json"))
	if err != nil {
		return caseOutcome{"failed", err.Error()}
	}
	result, err := assemble(raw, Options{})
	if err != nil {
		var unsupported *UnsupportedComponentError
		if errors.As(err, &unsupported) {
			return unsupportedOutcome(raw, err, "tokenizer", "renderer")
		}
		return caseOutcome{"failed", err.Error()}
	}
	if err := compareCase(directory, result); err != nil {
		return caseOutcome{"failed", err.Error()}
	}
	return caseOutcome{outcome: "passed"}
}

// unsupportedOutcome is the outcome of a case the port cannot run for want of a tokenizer or
// renderer (README, Reporting results). Every implementation provides the required ones, so the
// case is skipped only when one of its snapshot's fields names an optional component, outside
// the required set; a case whose fields name only required ones has failed. A rejection case
// passes only its renderer field, since no snapshot check needs a tokenizer.
func unsupportedOutcome(raw []byte, err error, fields ...string) caseOutcome {
	required, readErr := requiredComponents()
	if readErr != nil {
		return caseOutcome{"failed", readErr.Error()}
	}
	// A snapshot that does not parse names no optional component.
	var snapshot map[string]json.RawMessage
	_ = json.Unmarshal(raw, &snapshot)
	var optional []string
	for _, field := range fields {
		var id string
		if json.Unmarshal(snapshot[field], &id) == nil && id != "" && !required[id] {
			optional = append(optional, field+" "+id)
		}
	}
	if len(optional) > 0 {
		return caseOutcome{"skipped", fmt.Sprintf("%v: the case uses the optional %s", err, strings.Join(optional, " and "))}
	}
	return caseOutcome{"failed", "a required component is not provided: " + err.Error()}
}

// requiredComponents reads the tokenizers and renderers every implementation provides from the
// vendored README: the bullets under its Tokenizers and renderers heading, as
// scripts/conformance.py reads them.
func requiredComponents() (map[string]bool, error) {
	const path = "vendor/cwa/conformance/README.md"
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	_, section, found := strings.Cut(string(body), "\n## Tokenizers and renderers\n")
	if !found {
		return nil, fmt.Errorf("%s has no Tokenizers and renderers section; re-vendor the contract", path)
	}
	section, _, _ = strings.Cut(section, "\n## ")
	required := map[string]bool{}
	for _, match := range requiredBullet.FindAllStringSubmatch(section, -1) {
		required[match[1]] = true
	}
	if len(required) == 0 {
		return nil, fmt.Errorf("%s lists no tokenizers or renderers under Tokenizers and renderers", path)
	}
	return required, nil
}

var requiredBullet = regexp.MustCompile("(?m)^- `([^`]+)`")

func compareCase(directory string, result Result) error {
	wantPayload, err := os.ReadFile(filepath.Join(directory, "expected.payload.txt"))
	if err == nil && !bytes.Equal(result.Payload, wantPayload) {
		return fmt.Errorf("payload bytes differ")
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if os.IsNotExist(err) && result.Payload != nil {
		return fmt.Errorf("refusal produced a payload")
	}
	if result.Trace == nil {
		return fmt.Errorf("assembly produced no trace")
	}
	compiled, err := traceSchema()
	if err != nil {
		return err
	}
	if err := compiled.Validate(result.Trace); err != nil {
		return fmt.Errorf("trace fails schema: %w", err)
	}
	wantBody, err := os.ReadFile(filepath.Join(directory, "expected.trace.json"))
	if err != nil {
		return err
	}
	actualBody, err := json.Marshal(result.Trace)
	if err != nil {
		return err
	}
	var want, actual map[string]any
	if err := json.Unmarshal(wantBody, &want); err != nil {
		return err
	}
	if err := json.Unmarshal(actualBody, &actual); err != nil {
		return err
	}
	// trace_id, timings and recovery.detail may differ (README, Running a case; R-23).
	for _, trace := range []map[string]any{want, actual} {
		delete(trace, "trace_id")
		delete(trace, "timings")
		if recovery, ok := trace["recovery"].(map[string]any); ok {
			delete(recovery, "detail")
		}
	}
	if !reflect.DeepEqual(actual, want) {
		return fmt.Errorf("trace differs")
	}
	return nil
}

func TestImplementationIdentity(t *testing.T) {
	goMod, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(goMod), "module "+Implementation.Name+"\n") {
		t.Fatal("report name differs from the module path")
	}
	if Implementation.Version == "" || Implementation.Language != "Go" {
		t.Fatal("incomplete implementation identity")
	}
}

func TestReportCurrent(t *testing.T) {
	if testing.Short() {
		t.Skip("report regeneration")
	}
	adapter := filepath.Join(t.TempDir(), "adapter")
	build := exec.Command("go", "build", "-o", adapter, "./cmd/adapter")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build adapter: %v: %s", err, output)
	}
	outputPath := filepath.Join(t.TempDir(), "report.json")
	cmd := exec.Command("python3", "scripts/conformance.py", "--command", adapter,
		"--name", Implementation.Name, "--version", Implementation.Version,
		"--language", Implementation.Language, "--out", outputPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		// Expected while ordinary cases remain pending; the report is still written.
		if len(pending) == 0 {
			t.Fatalf("conformance run: %v: %s", err, output)
		}
	}
	want, err := os.ReadFile("conformance-report.json")
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("conformance-report.json is stale")
	}
}

// TestCompareIgnoresRecoveryDetail follows the README's Running a case: recovery.detail is free
// text for people, removed with trace_id and timings before the trace comparison (R-23), while
// recovery.action is still compared.
func TestCompareIgnoresRecoveryDetail(t *testing.T) {
	directory := "vendor/cwa/conformance/cases/conflict-request-context"
	raw, err := os.ReadFile(filepath.Join(directory, "snapshot.json"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := Assemble(raw, Options{})
	if err != nil {
		t.Fatal(err)
	}
	recovery, ok := result.Trace["recovery"].(map[string]any)
	if !ok {
		t.Fatal("the case records no recovery")
	}
	recovery["detail"] = "Ask the user which instruction applies."
	if err := compareCase(directory, result); err != nil {
		t.Errorf("recovery.detail was compared: %v", err)
	}
	recovery["action"] = "retrieve_narrower"
	if err := compareCase(directory, result); err == nil {
		t.Error("recovery.action was not compared")
	}
}

// TestUnsupportedComponentCases follows the README's Reporting results. Every published case
// uses only required tokenizers and renderers, so a port that reports the case's tokenizer or
// renderer unsupported has failed it, never skipped it. A case that names an optional component
// the port does not provide is skipped.
func TestUnsupportedComponentCases(t *testing.T) {
	paths, err := filepath.Glob("vendor/cwa/conformance/cases/*/snapshot.json")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no vendored cases: %v", err)
	}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var named struct{ Tokenizer, Renderer string }
		if err := json.Unmarshal(raw, &named); err != nil {
			t.Fatal(err)
		}
		for _, lacking := range []UnsupportedComponentError{{"tokenizer", named.Tokenizer}, {"renderer", named.Renderer}} {
			without := func([]byte, Options) (Result, error) { return Result{}, &lacking }
			if got := runCase(filepath.Dir(path), without); got.outcome != "failed" {
				t.Errorf("%s without its %s %s: %s, want failed", filepath.Base(filepath.Dir(path)), lacking.Component, lacking.ID, got.outcome)
			}
		}
	}
	for _, field := range []string{"tokenizer", "renderer"} {
		directory := namingComponent(t, "vendor/cwa/conformance/cases/fixture-three-slot", field, "optional-"+field+"/v1")
		if got := runCase(directory, Assemble); got.outcome != "skipped" {
			t.Errorf("a case naming an optional %s: %s, want skipped: %s", field, got.outcome, got.detail)
		}
	}
}

// namingComponent copies the snapshot of the vendored case in source to a new directory, with
// its field naming id instead, and returns the directory.
func namingComponent(t *testing.T, source, field, id string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(source, "snapshot.json"))
	if err != nil {
		t.Fatal(err)
	}
	var named map[string]json.RawMessage
	if err := json.Unmarshal(raw, &named); err != nil {
		t.Fatal(err)
	}
	old := append([]byte(`"`+field+`": `), named[field]...)
	if bytes.Count(raw, old) != 1 {
		t.Fatalf("%s does not name its %s once as %s", source, field, old)
	}
	directory := t.TempDir()
	edited := bytes.Replace(raw, old, []byte(`"`+field+`": "`+id+`"`), 1)
	if err := os.WriteFile(filepath.Join(directory, "snapshot.json"), edited, 0o644); err != nil {
		t.Fatal(err)
	}
	return directory
}

// TestRequiredComponents pins the required set read from the vendored README to the four
// components this port provides. A re-vendor that requires another fails here until the port
// provides it, and a README the reader no longer understands fails here rather than letting
// every unsupported case be skipped.
func TestRequiredComponents(t *testing.T) {
	required, err := requiredComponents()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"fixture-whitespace/v1": true, "estimate-utf8/v1": true, "fixture-xml/v1": true, "cwa-messages/v1": true}
	if !reflect.DeepEqual(required, want) {
		t.Fatalf("the README requires %v, want %v", required, want)
	}
}
