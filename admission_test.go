package assembler

import "testing"

// TestMCPCapabilityNeedsRouteSlot checks R-15's two codes for a tool specification an mcp producer
// sends to governance.capabilities: the route must list that slot for the producer before the
// capability check applies, and producer_slot_not_allowed comes first in contract/reasons.json (R-21).
func TestMCPCapabilityNeedsRouteSlot(t *testing.T) {
	a := &admissionPass{
		producers: map[string]any{
			"crm-mcp":  map[string]any{"kind": "mcp", "slots": []any{"evidence.tool_results"}},
			"docs-mcp": map[string]any{"kind": "mcp", "slots": []any{"evidence.knowledge", "governance.capabilities"}},
			"grants":   map[string]any{"kind": "capability_policy", "slots": []any{"governance.capabilities"}},
		},
		capabilities: map[string]any{"policy_producer": "grants", "allowed_ids": []any{"cap:granted"}},
	}
	for _, test := range []struct {
		producer, kind, id, slot, authority, want string
	}{
		{"crm-mcp", "mcp", "cap:refund-direct", "governance.capabilities", "governing", "producer_slot_not_allowed"},
		{"docs-mcp", "mcp", "cap:search-docs", "governance.capabilities", "governing", "capability_not_allowed"},
		{"docs-mcp", "mcp", "cap:granted", "governance.capabilities", "governing", "capability_not_allowed"},
		{"docs-mcp", "mcp", "policy:x", "governance.instructions", "governing", "producer_slot_not_allowed"},
		{"crm-mcp", "mcp", "crm:order", "evidence.tool_results", "observation", ""},
		{"grants", "capability_policy", "cap:granted", "governance.capabilities", "governing", ""},
	} {
		item := map[string]any{"id": test.id, "slot": test.slot, "authority": test.authority}
		rule := objectValue(a.producers[test.producer])
		if got := a.permissionReason(item, test.producer, test.kind, rule); got != test.want {
			t.Errorf("%s from %s in %s: got %q, want %q", test.id, test.producer, test.slot, got, test.want)
		}
	}
}
