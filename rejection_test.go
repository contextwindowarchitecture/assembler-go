package assembler

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestRejectionCases(t *testing.T) {
	paths, err := filepath.Glob("vendor/cwa/conformance/rejections/*/snapshot.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no vendored rejection cases")
	}
	for _, path := range paths {
		t.Run(filepath.Base(filepath.Dir(path)), func(t *testing.T) {
			switch got := runRejection(filepath.Dir(path), Assemble); got.outcome {
			case "rejected":
			case "skipped":
				t.Skip(got.detail)
			default:
				t.Fatal(got.detail)
			}
		})
	}
}

// runRejection runs the rejection case in directory: its snapshot is rejected before assembly,
// with no payload and no trace (README, Reporting results).
func runRejection(directory string, assemble assembleFunc) caseOutcome {
	raw, err := os.ReadFile(filepath.Join(directory, "snapshot.json"))
	if err != nil {
		return caseOutcome{"failed", err.Error()}
	}
	result, err := assemble(raw, Options{})
	var unsupported *UnsupportedComponentError
	if errors.As(err, &unsupported) {
		return unsupportedOutcome(raw, err, "renderer")
	}
	var rejected *SnapshotRejectedError
	if !errors.As(err, &rejected) {
		return caseOutcome{"failed", fmt.Sprintf("want snapshot rejection, got %v", err)}
	}
	if result.Payload != nil || result.Trace != nil {
		return caseOutcome{"failed", "rejection produced payload or trace"}
	}
	return caseOutcome{outcome: "rejected"}
}

func TestValidSnapshotIsNotRejected(t *testing.T) {
	raw, err := os.ReadFile("vendor/cwa/conformance/cases/fixture-three-slot/snapshot.json")
	if err != nil {
		t.Fatal(err)
	}
	_, err = Assemble(raw, Options{})
	var rejected *SnapshotRejectedError
	if errors.As(err, &rejected) {
		t.Fatalf("valid snapshot rejected: %v", err)
	}
}

// TestUnsupportedComponentRejections follows the README's Reporting results. A rejection case is
// skipped only when the port does not provide its renderer and that renderer is optional, as it
// is when a case breaks an optional renderer's check; no snapshot check needs a tokenizer. Every
// published rejection uses required components, so a port that reports one of them unsupported
// has failed the case.
func TestUnsupportedComponentRejections(t *testing.T) {
	paths, err := filepath.Glob("vendor/cwa/conformance/rejections/*/snapshot.json")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no vendored rejection cases: %v", err)
	}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var named map[string]json.RawMessage
		if err := json.Unmarshal(raw, &named); err != nil {
			t.Fatal(err)
		}
		for _, component := range []string{"tokenizer", "renderer"} {
			var id string
			if err := json.Unmarshal(named[component], &id); err != nil {
				t.Fatalf("%s names no %s: %v", path, component, err)
			}
			lacking := &UnsupportedComponentError{Component: component, ID: id}
			without := func([]byte, Options) (Result, error) { return Result{}, lacking }
			if got := runRejection(filepath.Dir(path), without); got.outcome != "failed" {
				t.Errorf("%s without its %s %s: %s, want failed", filepath.Base(filepath.Dir(path)), component, id, got.outcome)
			}
		}
	}
	source := "vendor/cwa/conformance/rejections/profile-unrealizable"
	directory := namingComponent(t, source, "renderer", "optional-renderer/v1")
	if got := runRejection(directory, Assemble); got.outcome != "skipped" {
		t.Errorf("a rejection breaking an optional renderer's check: %s, want skipped: %s", got.outcome, got.detail)
	}
	directory = namingComponent(t, source, "tokenizer", "optional-tokenizer/v1")
	without := func([]byte, Options) (Result, error) {
		return Result{}, &UnsupportedComponentError{Component: "tokenizer", ID: "optional-tokenizer/v1"}
	}
	if got := runRejection(directory, without); got.outcome != "failed" {
		t.Errorf("a rejection without its optional tokenizer: %s, want failed", got.outcome)
	}
}
