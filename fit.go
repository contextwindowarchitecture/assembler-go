package assembler

import "errors"

// protectedLimits checks all three protected refusal conditions before reduction.
func protectedLimits(snapshot map[string]any, items []*basicItem, rendererID string, tokenizer Tokenizer) (bool, error) {
	protected := []*basicItem{}
	for _, item := range items {
		if item.tier != "protected" {
			continue
		}
		protected = append(protected, item)
		over, err := itemOverCap(snapshot, item, rendererID, tokenizer)
		if err != nil {
			return false, err
		}
		if over {
			return true, nil
		}
	}
	slotRules := objectValue(asObject(snapshot["route_policy"])["slots"])
	for slot, rawRule := range slotRules {
		cap, ok := objectValue(rawRule)["max_tokens"].(float64)
		if !ok {
			continue
		}
		size, err := slotSize(snapshot, protected, slot, rendererID, tokenizer)
		if err != nil {
			return false, err
		}
		if float64(size) > cap {
			return true, nil
		}
	}
	_, _, count, err := renderBasic(snapshot, protected, rendererID, tokenizer)
	if err != nil {
		return false, err
	}
	return !fitsBudget(count, asObject(snapshot["budget"])), nil
}

func checkOtherItemCaps(snapshot map[string]any, items []*basicItem, rendererID string, tokenizer Tokenizer) error {
	for _, item := range items {
		if item.tier == "protected" {
			continue
		}
		over, err := itemOverCap(snapshot, item, rendererID, tokenizer)
		if err != nil {
			return err
		}
		if over {
			return featureGap("item token cap")
		}
	}
	return nil
}

func fitsBudget(count int, budget map[string]any) bool {
	margin := 0
	if value, ok := budget["margin_percent"].(float64); ok {
		margin = int(value)
	}
	if count < 0 || count > (int(^uint(0)>>1)-99)/(100+margin) {
		return false
	}
	return float64((count*(100+margin)+99)/100) <= budget["input"].(float64)
}

func itemOverCap(snapshot map[string]any, item *basicItem, rendererID string, tokenizer Tokenizer) (bool, error) {
	cap, ok := item.data["token_budget"].(float64)
	if !ok {
		return false, nil
	}
	count, err := maxBodyTokens(snapshot, item, rendererID, tokenizer)
	return float64(count) > cap, err
}

// maxBodyTokens uses the largest rendered occurrence when a slot appears twice.
func maxBodyTokens(snapshot map[string]any, item *basicItem, rendererID string, tokenizer Tokenizer) (int, error) {
	maximum := 0
	for _, raw := range asArray(asObject(snapshot["profile"])["placement"]) {
		placement := asObject(raw)
		if placement["slot"] != item.slot {
			continue
		}
		body := bodyForWrap(item, rendererID, placement["wrap"])
		count := tokenizer(body)
		if count < 0 {
			return 0, errors.New("tokenizer returned a negative count")
		}
		maximum = max(maximum, count)
	}
	return maximum, nil
}

func slotSize(snapshot map[string]any, items []*basicItem, slot, rendererID string, tokenizer Tokenizer) (int, error) {
	size := 0
	for _, raw := range asArray(asObject(snapshot["profile"])["placement"]) {
		placement := asObject(raw)
		if placement["slot"] != slot {
			continue
		}
		for _, item := range items {
			if item.slot != slot {
				continue
			}
			count := tokenizer(bodyForWrap(item, rendererID, placement["wrap"]))
			if count < 0 {
				return 0, errors.New("tokenizer returned a negative count")
			}
			size += count
		}
	}
	return size, nil
}

func bodyForWrap(item *basicItem, rendererID string, wrap any) string {
	if rendererID == "cwa-messages/v1" && (wrap == "system" || wrap == "tools") {
		return asString(item.data["body"])
	}
	return item.body
}
