package assembler

import "sort"

// stageExclusions applies one pipeline stage's decisions in deterministic row order.
func stageExclusions(items []*basicItem, dropped map[*basicItem]string, reason, relation string) ([]*basicItem, []any) {
	kept, excluded := make([]*basicItem, 0, len(items)), []any{}
	for _, item := range items {
		winner, lost := dropped[item]
		if !lost {
			kept = append(kept, item)
			continue
		}
		row := map[string]any{"item_id": item.id, "reason": reason, "stage": "assembler", "slot": item.slot}
		if relation != "" {
			row[relation] = winner
		}
		excluded = append(excluded, row)
	}
	sort.Slice(excluded, func(i, j int) bool {
		return utf16Less(asString(asObject(excluded[i])["item_id"]), asString(asObject(excluded[j])["item_id"]))
	})
	return kept, excluded
}
