package assembler

// rankHigher follows the slot's ordered keys, then the UTF-16 item ID.
func rankHigher(left, right *basicItem, slotRule map[string]any) bool {
	keys := arrayValue(slotRule["order_by"])
	if len(keys) == 0 {
		keys = []any{"-relevance", "-freshness"}
	}
	for _, raw := range keys {
		switch raw {
		case "-relevance":
			a, aok := left.data["relevance"].(float64)
			b, bok := right.data["relevance"].(float64)
			if aok != bok {
				return aok
			}
			if aok && a != b {
				return a > b
			}
		case "-freshness", "freshness":
			cmp, _ := compareInstants(asString(left.data["freshness"]), asString(right.data["freshness"]))
			if cmp != 0 {
				if raw == "-freshness" {
					return cmp > 0
				}
				return cmp < 0
			}
		}
	}
	return utf16Less(left.id, right.id)
}
