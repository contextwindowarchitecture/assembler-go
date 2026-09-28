package assembler

import "errors"

type itemBodyState struct {
	body, raw string
	tokens    int
	variant   map[string]any
}

func bodyState(item *basicItem) itemBodyState {
	return itemBodyState{item.body, item.bodyRaw, item.bodyTokens, item.variant}
}

func restoreBody(item *basicItem, state itemBodyState) {
	item.body, item.bodyRaw, item.bodyTokens, item.variant = state.body, state.raw, state.tokens, state.variant
}

func applyVariant(item *basicItem, variant map[string]any, tokenizer Tokenizer) error {
	item.bodyRaw = asString(variant["body"])
	item.body = escapeXML(item.bodyRaw)
	item.bodyTokens = tokenizer(item.body)
	if item.bodyTokens < 0 {
		return errors.New("tokenizer returned a negative count")
	}
	item.variant = variant
	return nil
}

// chooseVariant picks the largest fitting shorter body, or the shortest available.
func chooseVariant(snapshot map[string]any, items []*basicItem, item *basicItem, rendererID string, tokenizer Tokenizer) (bool, error) {
	return chooseVariantToFit(snapshot, item, rendererID, tokenizer, func() (bool, error) {
		_, _, count, err := renderBasic(snapshot, items, rendererID, tokenizer)
		if err != nil {
			return false, err
		}
		return fitsBudget(count, asObject(snapshot["budget"])), nil
	})
}

func chooseSlotVariant(snapshot map[string]any, items []*basicItem, item *basicItem, slot, rendererID string, cap float64, tokenizer Tokenizer) (bool, error) {
	return chooseVariantToFit(snapshot, item, rendererID, tokenizer, func() (bool, error) {
		size, err := slotSize(snapshot, items, slot, rendererID, tokenizer)
		return float64(size) <= cap, err
	})
}

func chooseVariantToFit(snapshot map[string]any, item *basicItem, rendererID string, tokenizer Tokenizer, targetFits func() (bool, error)) (bool, error) {
	currentSize, err := maxBodyTokens(snapshot, item, rendererID, tokenizer)
	if err != nil {
		return false, err
	}
	baseline := bodyState(item)
	bestSize, bestFits := 0, false
	var best map[string]any
	for _, raw := range asArray(item.data["variants"]) {
		variant := asObject(raw)
		if err := applyVariant(item, variant, tokenizer); err != nil {
			restoreBody(item, baseline)
			return false, err
		}
		size, err := maxBodyTokens(snapshot, item, rendererID, tokenizer)
		if err != nil {
			restoreBody(item, baseline)
			return false, err
		}
		if size < currentSize {
			fits, err := targetFits()
			if err != nil {
				restoreBody(item, baseline)
				return false, err
			}
			if best == nil || (fits && !bestFits) || (fits && bestFits && size > bestSize) || (!fits && !bestFits && size < bestSize) {
				best, bestSize, bestFits = variant, size, fits
			}
		}
		restoreBody(item, baseline)
	}
	if best == nil {
		return false, nil
	}
	if err := applyVariant(item, best, tokenizer); err != nil {
		restoreBody(item, baseline)
		return false, err
	}
	return true, nil
}

// chooseCapVariant takes the largest variant within the item's rendered body cap.
func chooseCapVariant(snapshot map[string]any, item *basicItem, rendererID string, tokenizer Tokenizer) (bool, error) {
	cap := item.data["token_budget"].(float64)
	baseline := bodyState(item)
	bestSize := -1
	var best map[string]any
	for _, raw := range asArray(item.data["variants"]) {
		variant := asObject(raw)
		if err := applyVariant(item, variant, tokenizer); err != nil {
			restoreBody(item, baseline)
			return false, err
		}
		size, err := maxBodyTokens(snapshot, item, rendererID, tokenizer)
		if err != nil {
			restoreBody(item, baseline)
			return false, err
		}
		if float64(size) <= cap && size > bestSize {
			best, bestSize = variant, size
		}
		restoreBody(item, baseline)
	}
	if best == nil {
		return false, nil
	}
	if err := applyVariant(item, best, tokenizer); err != nil {
		restoreBody(item, baseline)
		return false, err
	}
	return true, nil
}
