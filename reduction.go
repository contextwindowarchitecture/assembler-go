package assembler

type reductionWalk struct {
	items      *[]*basicItem
	excluded   *[]any
	candidates []*basicItem
	compress   func(*basicItem) (bool, error)
	fits       func() (bool, error)
	floor      *floorGuard
}

func anyItem(*basicItem) bool { return true }

// visit applies eligible candidates in route order until the target fits.
func (walk reductionWalk) visit(action, slot string, eligible func(*basicItem) bool) (bool, error) {
	for _, item := range walk.candidates {
		if (slot != "" && item.slot != slot) || !containsItem(*walk.items, item) || !eligible(item) {
			continue
		}
		if walk.floor != nil && walk.floor.frozen[item.slot] {
			continue
		}
		applied, err := walk.reduce(action, item)
		if err != nil {
			return false, err
		}
		if !applied {
			continue
		}
		ok, err := walk.fits()
		if ok || err != nil {
			return ok, err
		}
	}
	return false, nil
}

func (walk reductionWalk) reduce(action string, item *basicItem) (bool, error) {
	if action == "compress" {
		before := bodyState(item)
		changed, err := walk.compress(item)
		if err != nil || !changed {
			return changed, err
		}
		allowed, err := walk.allowed(*walk.items, item)
		if err != nil || !allowed {
			restoreBody(item, before)
		}
		return allowed, err
	}
	next := omitItem(append([]*basicItem(nil), (*walk.items)...), item)
	allowed, err := walk.allowed(next, item)
	if err != nil || !allowed {
		return false, err
	}
	*walk.items = next
	*walk.excluded = append(*walk.excluded, budgetExclusion(item))
	return true, nil
}

func (walk reductionWalk) allowed(items []*basicItem, item *basicItem) (bool, error) {
	if walk.floor == nil {
		return true, nil
	}
	return walk.floor.allow(items, item)
}

func omitItem(items []*basicItem, item *basicItem) []*basicItem {
	for i, active := range items {
		if active == item {
			return append(items[:i], items[i+1:]...)
		}
	}
	return items
}

func budgetExclusion(item *basicItem) map[string]any {
	return map[string]any{
		"item_id": item.id, "reason": "over_budget", "stage": "assembler", "slot": item.slot,
	}
}
