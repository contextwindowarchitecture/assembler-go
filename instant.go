package assembler

import (
	"cmp"
	"strings"
	"time"
)

// compareInstants retains every fractional digit after normalizing the offset.
func compareInstants(left, right string) (int, error) {
	a, af, err := instantParts(left)
	if err != nil {
		return 0, err
	}
	b, bf, err := instantParts(right)
	if err != nil {
		return 0, err
	}
	if a != b {
		return cmp.Compare(a, b), nil
	}
	length := max(len(af), len(bf))
	af += strings.Repeat("0", length-len(af))
	bf += strings.Repeat("0", length-len(bf))
	return strings.Compare(af, bf), nil
}

func instantParts(value string) (int64, string, error) {
	normalized := []byte(value)
	if normalized[10] == 't' {
		normalized[10] = 'T'
	}
	if normalized[len(normalized)-1] == 'z' {
		normalized[len(normalized)-1] = 'Z'
	}
	parsed, err := time.Parse(time.RFC3339Nano, string(normalized))
	if err != nil {
		return 0, "", err
	}
	fraction := ""
	if dot := strings.IndexByte(value, '.'); dot >= 0 {
		end := dot + 1
		for end < len(value) && value[end] >= '0' && value[end] <= '9' {
			end++
		}
		fraction = value[dot+1 : end]
	}
	return parsed.Unix(), fraction, nil
}
