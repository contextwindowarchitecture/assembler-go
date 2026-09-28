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
	candidates := sheddingOrder(items, policy, "droppable")
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

// reduceCompressible follows route steps before the default compress/omit steps.
func reduceCompressible(snapshot map[string]any, items []*basicItem, rendererID string, tokenizer Tokenizer) ([]*basicItem, []any, error) {
	budget := asObject(snapshot["budget"])
	_, _, count, err := renderBasic(snapshot, items, rendererID, tokenizer)
	if err != nil {
		return nil, nil, err
	}
	if fitsBudget(count, budget) {
		return items, []any{}, nil
	}
	policy := asObject(snapshot["route_policy"])
	candidates := sheddingOrder(items, policy, "compressible")
	current := append([]*basicItem(nil), items...)
	excluded := []any{}
	listed := map[string]bool{}
	for _, raw := range arrayValue(policy["fitting_order"]) {
		step := asObject(raw)
		slot, action := asString(step["slot"]), asString(step["action"])
		listed[slot+"/"+action] = true
		if action == "compress" {
			if hasVariants(current, slot) {
				return nil, nil, featureGap("variant compression")
			}
			continue
		}
		for _, item := range candidates {
			if item.slot != slot {
				continue
			}
			current, excluded, count, err = omitCompressible(snapshot, current, excluded, item, rendererID, tokenizer)
			if err != nil {
				return nil, nil, err
			}
			if fitsBudget(count, budget) {
				return current, excluded, nil
			}
		}
	}
	for _, item := range current {
		if item.tier == "compressible" && !listed[item.slot+"/compress"] && len(asArray(item.data["variants"])) > 0 {
			return nil, nil, featureGap("variant compression")
		}
	}
	for _, item := range candidates {
		if listed[item.slot+"/omit"] {
			continue
		}
		current, excluded, count, err = omitCompressible(snapshot, current, excluded, item, rendererID, tokenizer)
		if err != nil {
			return nil, nil, err
		}
		if fitsBudget(count, budget) {
			break
		}
	}
	return current, excluded, nil
}

func hasVariants(items []*basicItem, slot string) bool {
	for _, item := range items {
		if item.slot == slot && len(asArray(item.data["variants"])) > 0 {
			return true
		}
	}
	return false
}

func omitCompressible(snapshot map[string]any, items []*basicItem, excluded []any, target *basicItem, rendererID string, tokenizer Tokenizer) ([]*basicItem, []any, int, error) {
	for i, item := range items {
		if item != target {
			continue
		}
		items = append(items[:i], items[i+1:]...)
		break
	}
	excluded = append(excluded, map[string]any{
		"item_id": target.id, "reason": "over_budget", "stage": "assembler", "slot": target.slot,
	})
	_, _, count, err := renderBasic(snapshot, items, rendererID, tokenizer)
	return items, excluded, count, err
}

func sheddingOrder(items []*basicItem, policy map[string]any, tier string) []*basicItem {
	slotRules := objectValue(policy["slots"])
	candidates := []*basicItem{}
	for _, item := range items {
		if item.tier == tier {
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
	return candidates
}
