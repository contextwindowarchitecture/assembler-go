package assembler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContractLock(t *testing.T) {
	data, err := os.ReadFile("vendor/cwa.lock.json")
	if err != nil {
		t.Fatal(err)
	}
	var lock struct {
		Files map[string]string `json:"files"`
	}
	if err := json.Unmarshal(data, &lock); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	err = filepath.WalkDir("vendor/cwa", func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		name, err := filepath.Rel("vendor/cwa", path)
		if err != nil {
			return err
		}
		name = filepath.ToSlash(name)
		want, ok := lock.Files[name]
		if !ok {
			t.Errorf("unlocked file %s", name)
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		hash := sha256.Sum256(body)
		if hex.EncodeToString(hash[:]) != want {
			t.Errorf("contract hash mismatch: %s", name)
		}
		seen[name] = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for name := range lock.Files {
		if !seen[name] {
			t.Errorf("locked file missing: %s", name)
		}
	}
}

func TestGeneratedContractCurrent(t *testing.T) {
	for _, source := range []string{"contract/reasons.json", "contract/slot-defaults.json"} {
		compareGenerated(t, source, filepath.Base(source))
	}
	matches, err := filepath.Glob("vendor/cwa/schema/*.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range matches {
		compareGenerated(t, strings.TrimPrefix(source, "vendor/cwa/"), "schema/"+filepath.Base(source))
	}
}

func compareGenerated(t *testing.T, source, destination string) {
	t.Helper()
	want, err := os.ReadFile(filepath.Join("vendor/cwa", source))
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join("internal/generated", destination))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("generated copy of %s is stale", source)
	}
}

func TestLicense(t *testing.T) {
	want, err := os.ReadFile("vendor/cwa/LICENSE")
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("LICENSE")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Error("LICENSE differs from vendored contract")
	}
	notice, err := os.ReadFile("NOTICE")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(notice), "Apache License, Version 2.0") {
		t.Error("NOTICE does not name Apache License, Version 2.0")
	}
}
