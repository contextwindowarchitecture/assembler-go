package assembler

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

type admissionResult struct {
	items          []*basicItem
	excluded       []any
	defaultsFilled []any
}

func objectValue(value any) map[string]any {
	result, _ := value.(map[string]any)
	return result
}

func arrayValue(value any) []any {
	result, _ := value.([]any)
	return result
}

func slotString(item map[string]any) string {
	slot, _ := item["slot"].(string)
	return slot
}

type admissionPass struct {
	schema       *jsonschema.Schema
	tokenizer    Tokenizer
	slotDefaults map[string]map[string]any
	policy       map[string]any
	producers    map[string]any
	capabilities map[string]any
	assemblyTime string
	requestScope map[string]any
	placements   map[string]bool
	idCounts     map[string]int
	result       admissionResult
}

func admit(snapshot map[string]any, tokenizer Tokenizer) (admissionResult, error) {
	schema, err := itemSchema()
	if err != nil {
		return admissionResult{}, err
	}
	defaults, err := slotDefaults()
	if err != nil {
		return admissionResult{}, err
	}
	policy := asObject(snapshot["route_policy"])
	a := &admissionPass{
		schema: schema, tokenizer: tokenizer, slotDefaults: defaults,
		policy: policy, producers: asObject(policy["producers"]),
		capabilities: objectValue(snapshot["capabilities"]),
		assemblyTime: asString(snapshot["assembly_time"]),
		requestScope: objectValue(snapshot["scope"]),
		placements:   map[string]bool{}, idCounts: map[string]int{},
		result: admissionResult{items: []*basicItem{}, excluded: []any{}, defaultsFilled: []any{}},
	}
	for _, raw := range asArray(asObject(snapshot["profile"])["placement"]) {
		a.placements[asString(asObject(raw)["slot"])] = true
	}
	batches := append([]any(nil), asArray(snapshot["batches"])...)
	sort.Slice(batches, func(i, j int) bool {
		left := asString(asObject(asObject(batches[i])["producer"])["id"])
		right := asString(asObject(asObject(batches[j])["producer"])["id"])
		return utf16Less(left, right)
	})
	for _, rawBatch := range batches {
		batch := asObject(rawBatch)
		for _, raw := range asArray(batch["items"]) {
			if id, ok := objectValue(raw)["id"].(string); ok && !blank(id) {
				a.idCounts[id]++
			}
		}
		for _, raw := range asArray(batch["excluded"]) {
			a.idCounts[asString(asObject(raw)["item_id"])]++
		}
	}
	// Producer rows precede all assembler admission rows, including unauthenticated batches.
	for _, rawBatch := range batches {
		rows, err := sortedRows(asArray(asObject(rawBatch)["excluded"]), "item_id")
		if err != nil {
			return admissionResult{}, err
		}
		a.result.excluded = append(a.result.excluded, rows...)
	}
	for _, rawBatch := range batches {
		if err := a.addBatch(asObject(rawBatch)); err != nil {
			return admissionResult{}, err
		}
	}
	sort.SliceStable(a.result.defaultsFilled, func(i, j int) bool {
		left, right := asObject(a.result.defaultsFilled[i]), asObject(a.result.defaultsFilled[j])
		if left["item_id"] != right["item_id"] {
			return utf16Less(asString(left["item_id"]), asString(right["item_id"]))
		}
		return defaultFieldOrder(asString(left["field"])) < defaultFieldOrder(asString(right["field"]))
	})
	return a.result, nil
}

func defaultFieldOrder(field string) int {
	for i, candidate := range []string{"token_budget", "variants", "conflict_policy", "lineage", "eligibility", "injection_risk"} {
		if field == candidate {
			return i
		}
	}
	return 6
}

func (a *admissionPass) addBatch(batch map[string]any) error {
	startExcluded := len(a.result.excluded)
	producer := asObject(batch["producer"])
	producerID, producerKind := asString(producer["id"]), asString(producer["kind"])
	rule, authenticated := a.producers[producerID]
	if authenticated {
		authenticated = asObject(rule)["kind"] == producerKind
	}
	items, err := sortedCandidates(asArray(batch["items"]))
	if err != nil {
		return err
	}
	invalidOrdinal := 0
	for _, raw := range items {
		item := objectValue(raw)
		id, validID := item["id"].(string)
		if !validID || blank(id) {
			id = fmt.Sprintf("%s#invalid-%d", producerID, invalidOrdinal)
			invalidOrdinal++
		}
		reason, validStructure := a.structuralReason(raw)
		if !authenticated {
			reason = "producer_not_authenticated"
		} else if reason == "" && a.idCounts[id] > 1 {
			reason = "duplicate_item_id"
		}
		if reason == "" && validStructure {
			copyItem := make(map[string]any, len(item)+6)
			for key, value := range item {
				copyItem[key] = value
			}
			a.fillDefaults(copyItem, id)
			reason, err = a.admissionReason(copyItem, producerID, producerKind, objectValue(rule))
			if err != nil {
				return err
			}
			if reason == "" {
				if err := a.include(copyItem, producerID); err != nil {
					return err
				}
			}
		}
		if reason != "" {
			row := map[string]any{"item_id": id, "reason": reason, "stage": "assembler"}
			if _, exists := a.slotDefaults[slotString(item)]; exists {
				row["slot"] = item["slot"]
			}
			a.result.excluded = append(a.result.excluded, row)
		}
	}
	// Invalid IDs are numbered in producer order but traced by their recorded IDs.
	sort.SliceStable(a.result.excluded[startExcluded:], func(i, j int) bool {
		rows := a.result.excluded[startExcluded:]
		return utf16Less(asString(asObject(rows[i])["item_id"]), asString(asObject(rows[j])["item_id"]))
	})
	return nil
}

func (a *admissionPass) structuralReason(raw any) (string, bool) {
	item := objectValue(raw)
	if item != nil {
		missing := []string{}
		for _, field := range []string{"id", "slot", "source", "source_version", "authority", "trust", "freshness", "body"} {
			if _, ok := item[field]; !ok {
				missing = append(missing, field)
			}
		}
		switch item["slot"] {
		case "interaction.memory":
			if _, ok := item["expires"]; !ok {
				missing = append(missing, "expires")
			}
		case "evidence.knowledge":
			if _, ok := item["relevance"]; !ok {
				missing = append(missing, "relevance")
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			return "missing_field:" + missing[0], false
		}
		// A slot or authority outside its closed set is unknown whatever its JSON type: a number,
		// null or an array also fails the schema, but these codes precede invalid_structure (R-1, R-21).
		if _, exists := a.slotDefaults[slotString(item)]; !exists {
			return "unknown_slot", false
		}
		switch item["authority"] {
		case "governing", "user", "state", "reference_only", "observation", "generated", "untrusted":
		default:
			return "unknown_authority", false
		}
	}
	if err := a.schema.Validate(raw); err != nil {
		return "invalid_structure", false
	}
	return "", true
}

func (a *admissionPass) fillDefaults(item map[string]any, id string) {
	slot := asString(item["slot"])
	base := a.slotDefaults[slot]
	override := objectValue(objectValue(a.policy["default_overrides"])[slot])
	for _, field := range []string{"token_budget", "variants", "conflict_policy", "lineage", "eligibility", "injection_risk"} {
		if _, exists := item[field]; exists {
			continue
		}
		value := base[field]
		if changed, exists := override[field]; exists {
			value = changed
		}
		item[field] = value
		a.result.defaultsFilled = append(a.result.defaultsFilled, map[string]any{"item_id": id, "field": field})
	}
}

// admissionReason checks conditions in contract/reasons.json order.
func (a *admissionPass) admissionReason(item map[string]any, producerID, producerKind string, rule map[string]any) (string, error) {
	if reason := a.permissionReason(item, producerID, producerKind, rule); reason != "" {
		return reason, nil
	}
	if reason := a.trustAndTierReason(item, producerKind, rule); reason != "" {
		return reason, nil
	}
	if reason, err := a.timeReason(item); reason != "" || err != nil {
		return reason, err
	}
	if reason, err := a.eligibilityReason(item); reason != "" || err != nil {
		return reason, err
	}
	slot := asString(item["slot"])
	if !a.placements[slot] && effectiveTier(item, a.slotDefaults[slot], a.policy, slot) != "protected" {
		return "slot_unplaced", nil
	}
	return "", nil
}

func (a *admissionPass) permissionReason(item map[string]any, producerID, producerKind string, rule map[string]any) string {
	slot := asString(item["slot"])
	allowed := false
	for _, raw := range asArray(rule["slots"]) {
		allowed = allowed || raw == slot
	}
	// The route's listing and the producer's kind must both allow the slot; producer_slot_not_allowed
	// precedes capability_not_allowed, so an mcp tool reaches the capability check below only where
	// the route lists governance.capabilities for its producer (R-15, R-21).
	allowed = allowed && basicKindSlotAllowed(producerKind, slot)
	if !allowed {
		return "producer_slot_not_allowed"
	}
	if !authorityAllowed(item) {
		return "authority_not_allowed"
	}
	if slot == "governance.capabilities" {
		policyProducer, _ := a.capabilities["policy_producer"].(string)
		grantRule := objectValue(a.producers[policyProducer])
		ok := producerID == policyProducer && producerKind == "capability_policy" && grantRule["kind"] == "capability_policy"
		allowedIDs := false
		for _, raw := range arrayValue(a.capabilities["allowed_ids"]) {
			allowedIDs = allowedIDs || raw == item["id"]
		}
		if !ok || !allowedIDs {
			return "capability_not_allowed"
		}
	}
	return ""
}

func (a *admissionPass) trustAndTierReason(item map[string]any, producerKind string, rule map[string]any) string {
	slot := asString(item["slot"])
	if strings.HasPrefix(slot, "governance.") && (item["trust"] != "verified" || item["injection_risk"] == "untrusted_content") {
		return "untrusted_in_governance"
	}
	if a.slotDefaults[slot]["injection_risk"] == "untrusted_content" && item["injection_risk"] != "untrusted_content" && !(producerKind == "mcp" && rule["verified"] == true) {
		return "untrusted_content_unmarked"
	}
	defaultTier := asString(a.slotDefaults[slot]["tier"])
	if tier, exists := item["tier"].(string); exists {
		if defaultTier == "protected" && tier != "protected" {
			return "protected_tier_changed"
		}
		effective := defaultTier
		if upgrade, ok := objectValue(a.policy["tier_upgrades"])[slot].(string); ok && tierRank(upgrade) > tierRank(effective) {
			effective = upgrade
		}
		if tierRank(tier) > tierRank(effective) {
			return "tier_upgrade_not_allowed"
		}
	}
	variantIDs := map[string]bool{asString(item["id"]): true}
	for _, raw := range asArray(item["variants"]) {
		id := asString(asObject(raw)["id"])
		if variantIDs[id] {
			return "duplicate_variant_id"
		}
		variantIDs[id] = true
	}
	return ""
}

func (a *admissionPass) timeReason(item map[string]any) (string, error) {
	if item["revoked_by"] != nil {
		return "revoked", nil
	}
	if expires, ok := item["expires"].(string); ok {
		cmp, err := compareInstants(expires, a.assemblyTime)
		if err != nil {
			return "", err
		}
		if cmp <= 0 {
			return "expired", nil
		}
	}
	clockSkew, _ := a.policy["clock_skew_seconds"].(float64)
	later, err := compareInstantOffset(asString(item["freshness"]), a.assemblyTime, int64(clockSkew))
	if err != nil {
		return "", err
	}
	if later > 0 {
		return "future_freshness", nil
	}
	slot := asString(item["slot"])
	if strings.HasPrefix(slot, "state.") {
		slotRule := objectValue(objectValue(a.policy["slots"])[slot])
		stale, err := olderThan(item, a.assemblyTime, slotRule["max_age_seconds"])
		if err != nil {
			return "", err
		}
		if stale {
			return "stale_state", nil
		}
	}
	return "", nil
}

func (a *admissionPass) eligibilityReason(item map[string]any) (string, error) {
	slot := asString(item["slot"])
	slotRule := objectValue(objectValue(a.policy["slots"])[slot])
	if prefix, ok := slotRule["source_prefix"].(string); ok && !strings.HasPrefix(asString(item["source"]), prefix) {
		return "source_invalid", nil
	}
	scope := objectValue(item["scope"])
	for key, value := range scope {
		if a.requestScope[key] != value {
			return "out_of_scope", nil
		}
	}
	for _, raw := range arrayValue(slotRule["required_scope"]) {
		if _, ok := scope[asString(raw)]; !ok {
			return "out_of_scope", nil
		}
	}
	if threshold, ok := slotRule["min_relevance"].(float64); ok {
		relevance, exists := item["relevance"].(float64)
		if !exists || relevance < threshold {
			return "below_threshold", nil
		}
	}
	if !strings.HasPrefix(slot, "state.") {
		stale, err := olderThan(item, a.assemblyTime, slotRule["max_age_seconds"])
		if err != nil {
			return "", err
		}
		if stale {
			return "not_eligible", nil
		}
	}
	return "", nil
}

func authorityAllowed(item map[string]any) bool {
	slot, authority := asString(item["slot"]), asString(item["authority"])
	switch slot {
	case "governance.instructions", "governance.capabilities", "governance.examples", "governance.output_contract":
		return authority == "governing"
	case "state.user", "state.task":
		return authority == "state"
	case "evidence.knowledge":
		return authority == "reference_only"
	case "evidence.tool_results":
		return authority == "observation" || authority == "untrusted"
	case "interaction.memory":
		return authority == "generated" || authority == "untrusted"
	case "interaction.history":
		if item["lineage"] == "generated" {
			return authority == "untrusted"
		}
		return authority == "user" || authority == "untrusted"
	case "interaction.query":
		return authority == "user"
	}
	return false
}

func tierRank(tier string) int {
	switch tier {
	case "protected":
		return 3
	case "compressible":
		return 2
	default:
		return 1
	}
}

func effectiveTier(item, slotDefault, policy map[string]any, slot string) string {
	if own, ok := item["tier"].(string); ok {
		return own
	}
	tier := asString(slotDefault["tier"])
	if upgrade, ok := objectValue(policy["tier_upgrades"])[slot].(string); ok && tierRank(upgrade) > tierRank(tier) {
		return upgrade
	}
	return tier
}

func olderThan(item map[string]any, assemblyTime string, limit any) (bool, error) {
	seconds, ok := limit.(float64)
	if !ok {
		return false, nil
	}
	cmp, err := compareInstantOffset(asString(item["freshness"]), assemblyTime, -int64(seconds))
	return cmp < 0, err
}

func (a *admissionPass) include(item map[string]any, producerID string) error {
	rawBody := asString(item["body"])
	body := escapeXML(rawBody)
	count := a.tokenizer(body)
	if count < 0 {
		return errors.New("tokenizer returned a negative count")
	}
	a.result.items = append(a.result.items, &basicItem{
		id: asString(item["id"]), slot: asString(item["slot"]), body: body,
		bodyRaw: rawBody, initialBody: body, initialRaw: rawBody,
		sourceVersion: asString(item["source_version"]), eligibility: asString(item["eligibility"]),
		bodyTokens: count, tier: effectiveTier(item, a.slotDefaults[asString(item["slot"])], a.policy, asString(item["slot"])),
		data: item, producerID: producerID,
	})
	return nil
}
