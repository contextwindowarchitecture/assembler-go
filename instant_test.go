package assembler

import "testing"

func TestInstantFullPrecision(t *testing.T) {
	for _, test := range []struct {
		left, right string
		want        int
	}{
		{"2026-09-22T12:00:00.0000000001Z", "2026-09-22T12:00:00Z", 1},
		{"2026-09-22T12:00:00.0000000001Z", "2026-09-22T12:00:00.0000000002Z", -1},
		{"2026-09-22T11:58:00Z", "2026-09-22T13:58:00+02:00", 0},
		{"2026-09-22T12:00:00.0Z", "2026-09-22T12:00:00Z", 0},
	} {
		got, err := compareInstants(test.left, test.right)
		if err != nil {
			t.Fatal(err)
		}
		if got != test.want {
			t.Errorf("compare %q to %q = %d, want %d", test.left, test.right, got, test.want)
		}
	}
}
