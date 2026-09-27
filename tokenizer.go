package assembler

// fixtureWhitespace counts maximal non-whitespace runs using ECMAScript's set.
func fixtureWhitespace(text string) int {
	count, inWord := 0, false
	for _, r := range text {
		if ecmaWhitespace(r) {
			inWord = false
		} else if !inWord {
			count++
			inWord = true
		}
	}
	return count
}

// estimateUTF8 charges one token per four UTF-8 bytes, rounded up.
func estimateUTF8(text string) int { return (len(text) + 3) / 4 }
