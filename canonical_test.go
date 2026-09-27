package assembler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ucarion/jcs"
)

func TestSnapshotDigestCases(t *testing.T) {
	paths, err := filepath.Glob("vendor/cwa/conformance/cases/*/snapshot.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		t.Run(filepath.Base(filepath.Dir(path)), func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var snapshot map[string]any
			if err := json.Unmarshal(raw, &snapshot); err != nil {
				t.Fatal(err)
			}
			got, err := snapshotDigest(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			traceBody, err := os.ReadFile(filepath.Join(filepath.Dir(path), "expected.trace.json"))
			if err != nil {
				t.Fatal(err)
			}
			var trace struct {
				Context struct {
					SnapshotDigest string `json:"snapshot_digest"`
				} `json:"context"`
			}
			if err := json.Unmarshal(traceBody, &trace); err != nil {
				t.Fatal(err)
			}
			if got != trace.Context.SnapshotDigest {
				t.Fatalf("digest: got %q, want %q", got, trace.Context.SnapshotDigest)
			}
		})
	}
}

func TestRegistryDigests(t *testing.T) {
	base := "vendor/cwa/conformance/registry"
	read := func(name string, target any) {
		t.Helper()
		body, err := os.ReadFile(filepath.Join(base, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(body, target); err != nil {
			t.Fatal(err)
		}
	}
	var lock struct {
		Profiles []struct {
			ID     string `json:"id"`
			SHA256 string `json:"sha256"`
		} `json:"profiles"`
		Routes []struct {
			Route   string `json:"route"`
			Version string `json:"version"`
			SHA256  string `json:"sha256"`
		} `json:"route_policies"`
	}
	read("lock.json", &lock)
	var profiles []map[string]any
	read("profiles.json", &profiles)
	for _, profile := range profiles {
		id := asString(profile["id"])
		delete(profile, "evaluation")
		got := canonicalHash(t, profile)
		matched := false
		for _, entry := range lock.Profiles {
			if entry.ID == id {
				matched = true
				if got != entry.SHA256 {
					t.Errorf("profile %s digest: got %s, want %s", id, got, entry.SHA256)
				}
				break
			}
		}
		if !matched {
			t.Errorf("profile %s is absent from registry lock", id)
		}
	}
	var routes []map[string]any
	read("route-policies.json", &routes)
	for _, route := range routes {
		got := canonicalHash(t, route)
		matched := false
		for _, entry := range lock.Routes {
			if entry.Route == route["route"] && entry.Version == route["version"] {
				matched = true
				if got != entry.SHA256 {
					t.Errorf("route %s %s digest: got %s, want %s", entry.Route, entry.Version, got, entry.SHA256)
				}
				break
			}
		}
		if !matched {
			t.Errorf("route %s %s is absent from registry lock", route["route"], route["version"])
		}
	}
}

func canonicalHash(t *testing.T, value any) string {
	t.Helper()
	encoded, err := jcs.Format(value)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte(encoded))
	return hex.EncodeToString(hash[:])
}
