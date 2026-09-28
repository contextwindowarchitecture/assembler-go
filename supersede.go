package assembler

import "sort"

type sourceKey struct {
	slot, producer, source string
}

// supersedeObservations keeps all equally latest observations of a producer source.
func supersedeObservations(items []*basicItem, policy map[string]any) ([]*basicItem, []any, error) {
	slotRules := objectValue(policy["slots"])
	groups := map[sourceKey][]*basicItem{}
	for _, item := range items {
		if objectValue(slotRules[item.slot])["supersede"] != "source" {
			continue
		}
		key := sourceKey{item.slot, item.producerID, asString(item.data["source"])}
		groups[key] = append(groups[key], item)
	}
	dropped := map[*basicItem]string{}
	for key, group := range groups {
		latest := group[0]
		for _, item := range group[1:] {
			cmp, err := compareInstants(asString(item.data["freshness"]), asString(latest.data["freshness"]))
			if err != nil {
				return nil, nil, err
			}
			if cmp > 0 {
				latest = item
			}
		}
		latestRanked := latest
		for _, item := range group {
			cmp, err := compareInstants(asString(item.data["freshness"]), asString(latest.data["freshness"]))
			if err != nil {
				return nil, nil, err
			}
			if cmp == 0 && rankHigher(item, latestRanked, objectValue(slotRules[key.slot])) {
				latestRanked = item
			}
		}
		for _, item := range group {
			cmp, err := compareInstants(asString(item.data["freshness"]), asString(latest.data["freshness"]))
			if err != nil {
				return nil, nil, err
			}
			if cmp < 0 && item.tier != "protected" && item.groupID == "" {
				dropped[item] = latestRanked.id
			}
		}
	}
	kept, excluded := make([]*basicItem, 0, len(items)), []any{}
	for _, item := range items {
		if winner, lost := dropped[item]; lost {
			excluded = append(excluded, map[string]any{
				"item_id": item.id, "reason": "superseded", "stage": "assembler",
				"slot": item.slot, "superseded_by": winner,
			})
		} else {
			kept = append(kept, item)
		}
	}
	sort.Slice(excluded, func(i, j int) bool {
		return utf16Less(asString(asObject(excluded[i])["item_id"]), asString(asObject(excluded[j])["item_id"]))
	})
	return kept, excluded, nil
}
