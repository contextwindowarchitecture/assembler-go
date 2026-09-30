package assembler

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestFixtureWhitespaceTokenizer(t *testing.T) {
	for _, test := range []struct {
		text string
		want int
	}{
		{"", 0},
		{"one two", 2},
		{"one\ufefftwo", 2},
		{"one\u001ctwo", 1},
		{" \u2028word\u00a0", 1},
	} {
		if got := fixtureWhitespace(test.text); got != test.want {
			t.Errorf("fixtureWhitespace(%q) = %d, want %d", test.text, got, test.want)
		}
	}
}

func TestEstimateUTF8Tokenizer(t *testing.T) {
	for _, test := range []struct {
		text string
		want int
	}{
		{"", 0}, {"a", 1}, {"abcd", 1}, {"abcde", 2}, {"😀", 1}, {"😀a", 2},
	} {
		if got := estimateUTF8(test.text); got != test.want {
			t.Errorf("estimateUTF8(%q) = %d, want %d", test.text, got, test.want)
		}
	}
}

// TestPublishedTokenizerIDStopsBeforeAssembly follows R-16 and the README's Tokenizers and
// renderers: an application's tokenizer under the ID of a published tokenizer stops the call
// before assembly, with no payload and no trace, whatever tokenizer the snapshot names. No case
// can supply a tokenizer, so this test is the only one that covers it.
func TestPublishedTokenizerIDStopsBeforeAssembly(t *testing.T) {
	raw, err := os.ReadFile("vendor/cwa/conformance/cases/fixture-three-slot/snapshot.json")
	if err != nil {
		t.Fatal(err)
	}
	const field = `"tokenizer": "fixture-whitespace/v1"`
	if bytes.Count(raw, []byte(field)) != 1 {
		t.Fatalf("snapshot does not name its tokenizer once as %s", field)
	}
	naming := func(id string) []byte {
		return bytes.Replace(raw, []byte(field), []byte(`"tokenizer": "`+id+`"`), 1)
	}
	counted := 0
	words := func(text string) int {
		counted++
		return len(strings.Fields(text))
	}

	// The application's own tokenizer under its own ID assembles, so the stops below come from
	// the published ID alone.
	result, err := Assemble(naming("app-words/v1"), Options{Tokenizers: map[string]Tokenizer{"app-words/v1": words}})
	if err != nil || result.Payload == nil || counted == 0 {
		t.Fatalf("own tokenizer under its own ID: err %v, payload %v, %d counts", err, result.Payload != nil, counted)
	}

	for _, published := range []string{"fixture-whitespace/v1", "estimate-utf8/v1"} {
		for _, named := range []string{"fixture-whitespace/v1", "estimate-utf8/v1", "app-words/v1", "unprovided/v1"} {
			t.Run(published+" named "+named, func(t *testing.T) {
				counted = 0
				options := Options{Tokenizers: map[string]Tokenizer{published: words, "app-words/v1": words}}
				result, err := Assemble(naming(named), options)
				if err == nil {
					t.Fatal("accepted an application tokenizer under a published ID")
				}
				var rejected *SnapshotRejectedError
				var unsupported *UnsupportedComponentError
				if errors.As(err, &rejected) || errors.As(err, &unsupported) {
					t.Fatalf("want the published ID refused, got %T: %v", err, err)
				}
				if !strings.Contains(err.Error(), published) {
					t.Errorf("error does not name %s: %v", published, err)
				}
				if result.Payload != nil || result.Trace != nil {
					t.Error("stopped call produced a payload or trace")
				}
				if counted != 0 {
					t.Errorf("an application tokenizer counted %d texts before the stop", counted)
				}
			})
		}
	}
}
