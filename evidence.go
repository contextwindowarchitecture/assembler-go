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
