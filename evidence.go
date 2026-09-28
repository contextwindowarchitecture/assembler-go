package assembler

// evidenceRequired checks the post-fitting evidence floor without interpreting bodies.
func evidenceRequired(items []*basicItem, policy map[string]any) bool {
	if policy["requires_evidence"] != true {
		return false
	}
	counts := map[string]int{"evidence.knowledge": 0, "evidence.tool_results": 0}
	for _, item := range items {
		if _, evidenceSlot := counts[item.slot]; evidenceSlot {
			counts[item.slot]++
		}
	}
	if counts["evidence.knowledge"]+counts["evidence.tool_results"] == 0 {
		return true
	}
	for _, slot := range []string{"evidence.knowledge", "evidence.tool_results"} {
		rule := objectValue(objectValue(policy["slots"])[slot])
		if minimum, ok := rule["min_included"].(float64); ok && float64(counts[slot]) < minimum {
			return true
		}
	}
	return false
}

func evidenceRecovery(excluded []any, admitted []*basicItem) string {
	byID := map[string]*basicItem{}
	for _, item := range admitted {
		byID[item.id] = item
	}
	omittedEvidence := false
	for _, raw := range excluded {
		row := asObject(raw)
		if row["stage"] != "assembler" || row["reason"] != "over_budget" {
			continue
		}
		if row["slot"] != "evidence.knowledge" && row["slot"] != "evidence.tool_results" {
			continue
		}
		item := byID[asString(row["item_id"])]
		if item == nil {
			continue
		}
		omittedEvidence = true
		if len(asArray(item.data["variants"])) == 0 {
			return "precompute_summary"
		}
	}
	if omittedEvidence {
		return "retrieve_narrower"
	}
	return "request_context"
}
