package assembler

import "sort"

// shedDroppable tests the whole rendered payload after each omission.
func shedDroppable(snapshot map[string]any, items []*basicItem, rendererID string, tokenizer Tokenizer) ([]*basicItem, []any, error) {
	policy := asObject(snapshot["route_policy"])
	slotRules := objectValue(policy["slots"])
	for slot, rawRule := range slotRules {
		if cap, ok := objectValue(rawRule)["max_tokens"].(float64); ok {
			size, err := slotSize(snapshot, items, slot, rendererID, tokenizer)
			if err != nil {
				return nil, nil, err
			}
			if float64(size) > cap {
				return nil, nil, featureGap("slot token cap")
			}
		}
	}
	_, _, count, err := renderBasic(snapshot, items, rendererID, tokenizer)
	if err != nil {
		return nil, nil, err
	}
	if fitsBudget(count, asObject(snapshot["budget"])) {
		return items, []any{}, nil
	}
	for _, rawRule := range slotRules {
		if objectValue(rawRule)["min_tokens"] != nil {
			return nil, nil, featureGap("slot floor fitting")
		}
	}
	candidates := []*basicItem{}
	for _, item := range items {
		if item.tier == "droppable" {
			candidates = append(candidates, item)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		leftRule, rightRule := objectValue(slotRules[left.slot]), objectValue(slotRules[right.slot])
		leftPriority, _ := leftRule["priority"].(float64)
		rightPriority, _ := rightRule["priority"].(float64)
		if leftPriority != rightPriority {
			return leftPriority < rightPriority
		}
		if left.slot != right.slot {
			return utf16Less(left.slot, right.slot)
		}
		return rankHigher(right, left, leftRule)
	})
	current := append([]*basicItem(nil), items...)
	excluded := []any{}
	for _, candidate := range candidates {
		for i, item := range current {
			if item == candidate {
				current = append(current[:i], current[i+1:]...)
				break
			}
		}
		excluded = append(excluded, map[string]any{
			"item_id": candidate.id, "reason": "over_budget", "stage": "assembler", "slot": candidate.slot,
		})
		_, _, count, err := renderBasic(snapshot, current, rendererID, tokenizer)
		if err != nil {
			return nil, nil, err
		}
		if fitsBudget(count, asObject(snapshot["budget"])) {
			break
		}
	}
	return current, excluded, nil
}
