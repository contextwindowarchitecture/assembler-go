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
			runConformanceCase(t, path, id)
		})
	}
	for id := range pending {
		if !seen[id] {
			t.Errorf("pending id %q has no vendored case", id)
		}
	}
}

func runConformanceCase(t *testing.T, path, id string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Assemble(raw, Options{})
	if err != nil {
		var unsupported *UnsupportedComponentError
		if errors.As(err, &unsupported) {
			t.Skip(err)
		}
		if pending[id] {
			return
		}
		t.Fatal(err)
	}
	if pending[id] {
		if err := compareCase(filepath.Dir(path), result); err != nil {
			return
		}
		t.Fatal("pending case now passes; remove it from pending")
	}
	if err := compareCase(filepath.Dir(path), result); err != nil {
		t.Fatal(err)
	}
}

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
