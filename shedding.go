package assembler

import "sort"

// shedDroppable tests the whole rendered payload after each omission.
func shedDroppable(snapshot map[string]any, items []*basicItem, rendererID string, tokenizer Tokenizer) ([]*basicItem, []any, error) {
	policy := asObject(snapshot["route_policy"])
	slotRules := objectValue(policy["slots"])
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

// fittingPass keeps route order and per-item state together during budget pressure.
type fittingPass struct {
	snapshot          map[string]any
	items, candidates []*basicItem
	excluded          []any
	rendererID        string
	tokenizer         Tokenizer
	budget            map[string]any
	listed            map[string]bool
}

func reduceCompressible(snapshot map[string]any, items []*basicItem, rendererID string, tokenizer Tokenizer) ([]*basicItem, []any, error) {
	policy := asObject(snapshot["route_policy"])
	pass := &fittingPass{
		snapshot: snapshot, items: append([]*basicItem(nil), items...),
		candidates: sheddingOrder(items, policy, "compressible"), excluded: []any{},
		rendererID: rendererID, tokenizer: tokenizer,
		budget: asObject(snapshot["budget"]), listed: map[string]bool{},
	}
	fits, err := pass.fits()
	if err != nil || fits {
		return pass.items, pass.excluded, err
	}
	walk := reductionWalk{
		items: &pass.items, excluded: &pass.excluded, candidates: pass.candidates,
		compress: func(item *basicItem) error {
			_, err := chooseVariant(pass.snapshot, pass.items, item, pass.rendererID, pass.tokenizer)
			return err
		}, fits: pass.fits,
	}
	for _, raw := range arrayValue(policy["fitting_order"]) {
		step := asObject(raw)
		slot, action := asString(step["slot"]), asString(step["action"])
		pass.listed[slot+"/"+action] = true
		fits, err = walk.visit(action, slot, func(*basicItem) bool { return true })
		if err != nil || fits {
			return pass.items, pass.excluded, err
		}
	}
	for _, action := range []string{"compress", "omit"} {
		fits, err = walk.visit(action, "", func(item *basicItem) bool {
			return !pass.listed[item.slot+"/"+action]
		})
		if err != nil || fits {
			return pass.items, pass.excluded, err
		}
	}
	return pass.items, pass.excluded, nil
}

func (pass *fittingPass) fits() (bool, error) {
	_, _, count, err := renderBasic(pass.snapshot, pass.items, pass.rendererID, pass.tokenizer)
	if err != nil {
		return false, err
	}
	return fitsBudget(count, pass.budget), nil
}

func containsItem(items []*basicItem, target *basicItem) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func sheddingOrder(items []*basicItem, policy map[string]any, tier string) []*basicItem {
	slotRules := objectValue(policy["slots"])
	candidates := []*basicItem{}
	for _, item := range items {
		if item.tier == tier || (tier == "" && item.tier != "protected") {
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
