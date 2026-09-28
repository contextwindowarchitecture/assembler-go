package assembler

type floorGuard struct {
	snapshot   map[string]any
	rendererID string
	tokenizer  Tokenizer
	frozen     map[string]bool
}

func newFloorGuard(snapshot map[string]any, rendererID string, tokenizer Tokenizer) *floorGuard {
	return &floorGuard{snapshot: snapshot, rendererID: rendererID, tokenizer: tokenizer, frozen: map[string]bool{}}
}

// allow freezes a slot when the proposed reduction would cross its floor.
func (guard *floorGuard) allow(items []*basicItem, item *basicItem) (bool, error) {
	rules := objectValue(asObject(guard.snapshot["route_policy"])["slots"])
	floor, set := objectValue(rules[item.slot])["min_tokens"].(float64)
	if !set {
		return true, nil
	}
	size, err := slotSize(guard.snapshot, items, item.slot, guard.rendererID, guard.tokenizer)
	if err != nil {
		return false, err
	}
	if float64(size) < floor {
		guard.frozen[item.slot] = true
		return false, nil
	}
	return true, nil
}
