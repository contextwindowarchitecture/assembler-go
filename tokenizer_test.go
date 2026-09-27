package assembler

import "testing"

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
