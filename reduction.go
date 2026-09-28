package assembler

type reductionWalk struct {
	items      *[]*basicItem
	excluded   *[]any
	candidates []*basicItem
	compress   func(*basicItem) error
	fits       func() (bool, error)
}

func anyItem(*basicItem) bool { return true }

// visit applies eligible candidates in route order until the target fits.
func (walk reductionWalk) visit(action, slot string, eligible func(*basicItem) bool) (bool, error) {
	for _, item := range walk.candidates {
		if (slot != "" && item.slot != slot) || !containsItem(*walk.items, item) || !eligible(item) {
			continue
		}
		if action == "compress" {
			if err := walk.compress(item); err != nil {
				return false, err
			}
		} else {
			*walk.items = omitItem(*walk.items, item)
			*walk.excluded = append(*walk.excluded, budgetExclusion(item))
		}
		ok, err := walk.fits()
		if ok || err != nil {
			return ok, err
		}
	}
	return false, nil
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
