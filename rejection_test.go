package assembler

import (
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
