package assembler

import "sort"

// capSourceDiversity limits ordinary items per authenticated producer and source.
func capSourceDiversity(items []*basicItem, policy map[string]any) ([]*basicItem, []any) {
	slotRules := objectValue(policy["slots"])
	groups := map[sourceKey][]*basicItem{}
	for _, item := range items {
		if _, ok := objectValue(slotRules[item.slot])["max_per_source"].(float64); ok {
			key := sourceKey{item.slot, item.producerID, asString(item.data["source"])}
			groups[key] = append(groups[key], item)
		}
	}
	dropped := map[*basicItem]string{}
	for key, group := range groups {
		rule := objectValue(slotRules[key.slot])
		cap := rule["max_per_source"].(float64)
		exempt, ordinary := 0, []*basicItem{}
		for _, item := range group {
			if item.tier == "protected" || item.groupID != "" {
				exempt++
			} else {
				ordinary = append(ordinary, item)
			}
		}
		sort.Slice(ordinary, func(i, j int) bool { return rankHigher(ordinary[i], ordinary[j], rule) })
		for rank, item := range ordinary {
			if float64(exempt+rank) >= cap {
				dropped[item] = ""
			}
		}
	}
	return stageExclusions(items, dropped, "source_diversity_cap", "")
}
