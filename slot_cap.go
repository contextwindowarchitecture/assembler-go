package assembler

import "sort"

type slotCapPass struct {
	snapshot   map[string]any
	items      []*basicItem
	excluded   []any
	policy     map[string]any
	slot       string
	cap        float64
	rendererID string
	tokenizer  Tokenizer
}

// enforceSlotCaps reduces each capped slot before whole-payload budget pressure.
func enforceSlotCaps(snapshot map[string]any, items []*basicItem, rendererID string, tokenizer Tokenizer) ([]*basicItem, []any, error) {
	policy := asObject(snapshot["route_policy"])
	rules := objectValue(policy["slots"])
	slots := []string{}
	for slot, raw := range rules {
		if _, capped := objectValue(raw)["max_tokens"].(float64); capped {
			slots = append(slots, slot)
		}
	}
	sort.Slice(slots, func(i, j int) bool {
		left, _ := objectValue(rules[slots[i]])["priority"].(float64)
		right, _ := objectValue(rules[slots[j]])["priority"].(float64)
		if left != right {
			return left < right
		}
		return utf16Less(slots[i], slots[j])
	})
	current, excluded := append([]*basicItem(nil), items...), []any{}
	for _, slot := range slots {
		pass := &slotCapPass{snapshot: snapshot, items: current, excluded: excluded,
			policy: policy, slot: slot, cap: objectValue(rules[slot])["max_tokens"].(float64),
			rendererID: rendererID, tokenizer: tokenizer}
		if err := pass.apply(); err != nil {
			return nil, nil, err
		}
		current, excluded = pass.items, pass.excluded
	}
	return current, excluded, nil
}

func (pass *slotCapPass) apply() error {
	within, err := pass.within()
	if err != nil || within {
		return err
	}
	walk := reductionWalk{
		items: &pass.items, excluded: &pass.excluded,
		compress: func(item *basicItem) error {
			_, err := chooseSlotVariant(pass.snapshot, pass.items, item, pass.slot, pass.rendererID, pass.cap, pass.tokenizer)
			return err
		}, fits: pass.within,
	}
	walk.candidates = sheddingOrder(pass.items, pass.policy, "droppable")
	if within, err = walk.visit("omit", pass.slot, anyItem); err != nil || within {
		return err
	}
	walk.candidates = sheddingOrder(pass.items, pass.policy, "compressible")
	listed := map[string]bool{}
	for _, raw := range arrayValue(pass.policy["fitting_order"]) {
		step := asObject(raw)
		if step["slot"] != pass.slot {
			continue
		}
		action := asString(step["action"])
		listed[action] = true
		if within, err = walk.visit(action, pass.slot, anyItem); err != nil || within {
			return err
		}
	}
	for _, action := range []string{"compress", "omit"} {
		if listed[action] {
			continue
		}
		if within, err = walk.visit(action, pass.slot, anyItem); err != nil || within {
			return err
		}
	}
	return featureGap("slot cap fitting")
}

func (pass *slotCapPass) within() (bool, error) {
	size, err := slotSize(pass.snapshot, pass.items, pass.slot, pass.rendererID, pass.tokenizer)
	return float64(size) <= pass.cap, err
}
