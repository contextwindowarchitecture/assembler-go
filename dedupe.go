package assembler

import (
	"sort"
	"strings"
)

type duplicateKey struct{ slot, body string }

// collapseBodyWhitespace uses the contract's fixed ECMAScript whitespace set.
func collapseBodyWhitespace(body string) string {
	var result strings.Builder
	space := false
	for _, r := range body {
		if ecmaWhitespace(r) {
			space = result.Len() > 0
			continue
		}
		if space {
			result.WriteByte(' ')
			space = false
		}
		result.WriteRune(r)
	}
	return result.String()
}

func dedupeExact(items []*basicItem, policy map[string]any) ([]*basicItem, []any) {
	slotRules := objectValue(policy["slots"])
	groups := map[duplicateKey][]*basicItem{}
	for _, item := range items {
		if objectValue(slotRules[item.slot])["dedupe"] == "exact" {
			key := duplicateKey{item.slot, collapseBodyWhitespace(asString(item.data["body"]))}
			groups[key] = append(groups[key], item)
		}
	}
	dropped := map[*basicItem]string{}
	for key, group := range groups {
		if len(group) < 2 {
			continue
		}
		rule := objectValue(slotRules[key.slot])
		var bestExempt, bestAny *basicItem
		for _, item := range group {
			if bestAny == nil || rankHigher(item, bestAny, rule) {
				bestAny = item
			}
			if item.tier == "protected" || item.groupID != "" {
				if bestExempt == nil || rankHigher(item, bestExempt, rule) {
					bestExempt = item
				}
			}
		}
		winner := bestAny
		if bestExempt != nil {
			winner = bestExempt
		}
		for _, item := range group {
			if item != winner && !(item.tier == "protected" || item.groupID != "") {
				dropped[item] = winner.id
			}
		}
	}
	kept, excluded := make([]*basicItem, 0, len(items)), []any{}
	for _, item := range items {
		if winner, lost := dropped[item]; lost {
			excluded = append(excluded, map[string]any{
				"item_id": item.id, "reason": "duplicate_content", "stage": "assembler",
				"slot": item.slot, "duplicate_of": winner,
			})
		} else {
			kept = append(kept, item)
		}
	}
	sort.Slice(excluded, func(i, j int) bool {
		return utf16Less(asString(asObject(excluded[i])["item_id"]), asString(asObject(excluded[j])["item_id"]))
	})
	return kept, excluded
}
