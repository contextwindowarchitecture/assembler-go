package assembler

import (
	"errors"
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
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			result, err := Assemble(raw, Options{})
			var rejected *SnapshotRejectedError
			if !errors.As(err, &rejected) {
				t.Fatalf("want snapshot rejection, got %v", err)
			}
			if result.Payload != nil || result.Trace != nil {
				t.Fatal("rejection produced payload or trace")
			}
		})
	}
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
