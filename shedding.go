package assembler

import "sort"

// shedDroppable tests the whole rendered payload after each omission.
func shedDroppable(snapshot map[string]any, items []*basicItem, rendererID string, tokenizer Tokenizer, floor *floorGuard) ([]*basicItem, []any, error) {
	policy := asObject(snapshot["route_policy"])
	_, _, count, err := renderBasic(snapshot, items, rendererID, tokenizer)
	if err != nil {
		return nil, nil, err
	}
	if fitsBudget(count, asObject(snapshot["budget"])) {
		return items, []any{}, nil
	}
	candidates := sheddingOrder(items, policy, "droppable")
	current := append([]*basicItem(nil), items...)
	excluded := []any{}
	for _, candidate := range candidates {
		if floor.frozen[candidate.slot] {
			continue
		}
		next := omitItem(append([]*basicItem(nil), current...), candidate)
		allowed, err := floor.allow(next, candidate)
		if err != nil {
			return nil, nil, err
		}
		if !allowed {
			continue
		}
		current = next
		excluded = append(excluded, budgetExclusion(candidate))
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

func reduceCompressible(snapshot map[string]any, items []*basicItem, rendererID string, tokenizer Tokenizer, floor *floorGuard) ([]*basicItem, []any, error) {
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
		compress: func(item *basicItem) (bool, error) {
			return chooseVariant(pass.snapshot, pass.items, item, pass.rendererID, pass.tokenizer)
		}, fits: pass.fits, floor: floor,
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
