package assembler

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Each pending case is a strict expected failure and leaves this set when it passes.
var pending = map[string]bool{
	"admission-reasons": true, "budget-droppable-order": true,
	"budget-margin-protected": true, "budget-margin-rounding": true,
	"budget-margin": true, "budget-omit-after-variants": true,
	"budget-protected-variants": true, "budget-route-order": true,
	"budget-route-tiers": true, "budget-slot-cap-before-pressure": true,
	"budget-slot-caps": true, "budget-slot-floor-refused": true,
	"budget-slot-floor-under-cap": true, "budget-slot-floor": true,
	"budget-token-caps": true, "budget-variant-choice": true,
	"capability-policy-kind": true, "conflict-fact": true,
	"conflict-instruction": true, "conflict-refused": true,
	"conflict-request-context": true, "conflict-required-slot-first": true,
	"conflict-surfaced-shed": true, "dedupe-evidence-required": true,
	"dedupe-exact": true, "dedupe-exemptions": true,
	"dedupe-producer-reported": true, "diversity-cap": true,
	"diversity-evidence-required": true, "diversity-exemptions": true,
	"evidence-cap-omitted": true, "evidence-precompute-summary": true,
	"evidence-request-context": true, "evidence-retrieve-narrower": true,
	"fixture-three-slot": true, "messages-budget": true,
	"messages-render": true, "ordering-astral-ids": true,
	"placement-protected-unplaced": true, "placement-required-slot-first": true,
	"placement-unplaced-slot": true, "protected-over-budget": true,
	"protected-over-cap": true, "protected-over-slot-cap": true,
	"render-attribute-escaping": true, "required-instructions-missing": true,
	"required-slot-missing": true, "supersede-evidence-required": true,
	"supersede-exemptions": true, "supersede-observations": true,
	"threshold-beyond-2-53": true, "tokenizer-estimate-utf8": true,
}

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
				t.Fatal("pending case now passes; remove it from pending")
			}
			wantPayload, readErr := os.ReadFile(filepath.Join(filepath.Dir(path), "expected.payload.txt"))
			if readErr == nil && !bytes.Equal(result.Payload, wantPayload) {
				t.Fatal("payload bytes differ")
			}
			if readErr != nil && !os.IsNotExist(readErr) {
				t.Fatal(readErr)
			}
			if os.IsNotExist(readErr) && result.Payload != nil {
				t.Fatal("refusal produced a payload")
			}
			wantTrace, err := os.ReadFile(filepath.Join(filepath.Dir(path), "expected.trace.json"))
			if err != nil {
				t.Fatal(err)
			}
			var want map[string]any
			if err := json.Unmarshal(wantTrace, &want); err != nil {
				t.Fatal(err)
			}
			if result.Trace == nil {
				t.Fatal("assembly produced no trace")
			}
			compiled, err := traceSchema()
			if err != nil {
				t.Fatal(err)
			}
			if err := compiled.Validate(result.Trace); err != nil {
				t.Fatalf("trace fails schema: %v", err)
			}
			delete(want, "trace_id")
			delete(want, "timings")
			delete(result.Trace, "trace_id")
			delete(result.Trace, "timings")
			if !reflect.DeepEqual(result.Trace, want) {
				t.Fatal("trace differs")
			}
		})
	}
	for id := range pending {
		if !seen[id] {
			t.Errorf("pending id %q has no vendored case", id)
		}
	}
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
