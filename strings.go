package assembler

func blank(value string) bool {
	for _, r := range value {
		if !ecmaWhitespace(r) {
			return false
		}
	}
	return true
}

func ecmaWhitespace(r rune) bool {
	switch {
	case r >= 0x0009 && r <= 0x000d:
		return true
	case r == 0x0020 || r == 0x00a0 || r == 0x1680:
		return true
	case r >= 0x2000 && r <= 0x200a:
		return true
	case r == 0x2028 || r == 0x2029 || r == 0x202f || r == 0x205f:
		return true
	case r == 0x3000 || r == 0xfeff:
		return true
	default:
		return false
	}
}
