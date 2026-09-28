package assembler_test

import (
	"bytes"
	"maps"
	"os"
	"reflect"
	"testing"

	assembler "github.com/contextwindowarchitecture/assembler-go"
)

// TestPublicAssemble exercises the exported API and its deterministic result.
func TestPublicAssemble(t *testing.T) {
	snapshot, err := os.ReadFile("vendor/cwa/conformance/cases/budget-slot-floor/snapshot.json")
	if err != nil {
		t.Fatal(err)
	}
	want, err := assembler.Assemble(snapshot, assembler.Options{})
	if err != nil || len(want.Payload) == 0 {
		t.Fatalf("initial assembly: %v", err)
	}
	for i := 0; i < 64; i++ {
		got, err := assembler.Assemble(snapshot, assembler.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got.Payload, want.Payload) || !reflect.DeepEqual(stableTrace(got.Trace), stableTrace(want.Trace)) {
			t.Fatal("repeated assembly changed payload or stable trace fields")
		}
	}
}

func stableTrace(trace map[string]any) map[string]any {
	copied := maps.Clone(trace)
	delete(copied, "trace_id")
	delete(copied, "timings")
	return copied
}
