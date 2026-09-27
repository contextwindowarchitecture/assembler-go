package assembler

import "testing"

func TestUTF16Ordering(t *testing.T) {
	if !utf16Less("😀", "ｚ") {
		t.Fatal("astral code unit must sort before U+FF5A")
	}
	if !utf16Less("a", "aa") {
		t.Fatal("a prefix must sort first")
	}
}

func TestECMAScriptBlank(t *testing.T) {
	for _, value := range []string{"", "\ufeff", " \u2028\u00a0"} {
		if !blank(value) {
			t.Fatalf("%q should be blank", value)
		}
	}
	for _, value := range []string{"\u001c", "text", "\ufeffx"} {
		if blank(value) {
			t.Fatalf("%q should not be blank", value)
		}
	}
}
