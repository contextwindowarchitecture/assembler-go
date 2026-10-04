package assembler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/contextwindowarchitecture/assembler-go/internal/generated"
)

type basicItem struct {
	id, slot, body, bodyRaw, initialBody, initialRaw, sourceVersion, eligibility, tier string
	bodyTokens                                                                         int
	data                                                                               map[string]any
	variant                                                                            map[string]any
	producerID, groupID, conflictID                                                    string
}

var (
	defaultsOnce sync.Once
	defaults     map[string]map[string]any
	defaultsErr  error
)

func slotDefaults() (map[string]map[string]any, error) {
	defaultsOnce.Do(func() {
		body, err := generated.Files.ReadFile("slot-defaults.json")
		if err != nil {
			defaultsErr = err
			return
		}
		defaultsErr = json.Unmarshal(body, &defaults)
	})
	return defaults, defaultsErr
}

func featureGap(name string) error { return fmt.Errorf("assembly feature not implemented: %s", name) }

// resolveTokenizer stops the call before assembly when the application supplies a tokenizer under
// a published tokenizer's id, whatever id the snapshot names, so a trace that names a published
// tokenizer always means its published count (R-16). The guarded ids are the tokenizers the
// README's Tokenizers and renderers lists, the optional ones included, whether or not this port
// provides them.
func resolveTokenizer(id string, options Options) (Tokenizer, error) {
	for _, builtin := range []string{"fixture-whitespace/v1", "estimate-utf8/v1"} {
		if _, exists := options.Tokenizers[builtin]; exists {
			return nil, fmt.Errorf("built-in tokenizer %s cannot be redefined", builtin)
		}
	}
	switch id {
	case "fixture-whitespace/v1":
		return fixtureWhitespace, nil
	case "estimate-utf8/v1":
		return estimateUTF8, nil
	default:
		if tokenizer := options.Tokenizers[id]; tokenizer != nil {
			return tokenizer, nil
		}
		return nil, &UnsupportedComponentError{Component: "tokenizer", ID: id}
	}
}

func assembleBasic(snapshot map[string]any, options Options) (Result, error) {
	tokenizerID, rendererID := asString(snapshot["tokenizer"]), asString(snapshot["renderer"])
	tokenizer, err := resolveTokenizer(tokenizerID, options)
	if err != nil {
		return Result{}, err
	}
	if rendererID != "fixture-xml/v1" && rendererID != "cwa-messages/v1" {
		return Result{}, &UnsupportedComponentError{Component: "renderer", ID: rendererID}
	}
	items, trace, err := prepareBasic(snapshot, tokenizer, tokenizerID, rendererID)
	if err != nil {
		return Result{}, err
	}
	if asObject(trace["refused"])["bool"] == true {
		return Result{Trace: trace}, nil
	}
	admittedBeforeFitting := items
	protectedOverCap, err := protectedLimits(snapshot, items, rendererID, tokenizer)
	if err != nil {
		return Result{}, err
	}
	if protectedOverCap {
		trace["refused"] = map[string]any{"bool": true, "reason": "protected_content_over_budget"}
		return Result{Trace: trace}, nil
	}
	items, cappedItems, err := enforceItemCaps(snapshot, items, rendererID, tokenizer)
	if err != nil {
		return Result{}, err
	}
	trace["excluded"] = append(asArray(trace["excluded"]), cappedItems...)
	items, cappedSlots, err := enforceSlotCaps(snapshot, items, rendererID, tokenizer)
	if err != nil {
		return Result{}, err
	}
	trace["excluded"] = append(asArray(trace["excluded"]), cappedSlots...)
	floor := newFloorGuard(snapshot, rendererID, tokenizer)
	items, shed, err := shedDroppable(snapshot, items, rendererID, tokenizer, floor)
	if err != nil {
		return Result{}, err
	}
	items, compressedShed, err := reduceCompressible(snapshot, items, rendererID, tokenizer, floor)
	if err != nil {
		return Result{}, err
	}
	trace["excluded"] = append(asArray(trace["excluded"]), shed...)
	trace["excluded"] = append(asArray(trace["excluded"]), compressedShed...)
	payload, included, count, err := renderBasic(snapshot, items, rendererID, tokenizer)
	if err != nil {
		return Result{}, err
	}
	if count < 0 {
		return Result{}, errors.New("tokenizer returned a negative count")
	}
	if !fitsBudget(count, asObject(snapshot["budget"])) {
		trace["refused"] = map[string]any{"bool": true, "reason": "slot_floor_over_budget"}
		return Result{Trace: trace}, nil
	}
	if evidenceRequired(items, asObject(snapshot["route_policy"])) {
		trace["refused"] = map[string]any{"bool": true, "reason": "evidence_required"}
		trace["recovery"] = map[string]any{"action": evidenceRecovery(asArray(trace["excluded"]), admittedBeforeFitting)}
		return Result{Trace: trace}, nil
	}
	hash := sha256.Sum256(payload)
	trace["result"] = map[string]any{"input_tokens": count, "hash": hex.EncodeToString(hash[:])}
	trace["included"] = included
	compressed, err := compressedRows(snapshot, items, rendererID, tokenizer)
	if err != nil {
		return Result{}, err
	}
	trace["compressed"] = compressed
	return Result{Payload: payload, Trace: trace}, nil
}

func prepareBasic(snapshot map[string]any, tokenizer Tokenizer, tokenizerID, rendererID string) ([]*basicItem, map[string]any, error) {
	policy := asObject(snapshot["route_policy"])
	allowed := map[string]bool{
		"route": true, "version": true, "producers": true, "slots": true,
		"default_overrides": true, "tier_upgrades": true, "clock_skew_seconds": true,
		"parser": true, "on_unresolved_instruction": true, "facts": true, "fitting_order": true,
		"requires_evidence": true,
	}
	keys := make([]string, 0, len(policy))
	for key := range policy {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return utf16Less(keys[i], keys[j]) })
	for _, key := range keys {
		if !allowed[key] {
			return nil, nil, featureGap("route policy " + key)
		}
	}
	admission, err := admit(snapshot, tokenizer)
	if err != nil {
		return nil, nil, err
	}
	conflict, err := resolveConflicts(snapshot, admission.items)
	if err != nil {
		return nil, nil, err
	}
	items, superseded, err := supersedeObservations(conflict.items, policy)
	if err != nil {
		return nil, nil, err
	}
	items, duplicated := dedupeExact(items, policy)
	items, capped := capSourceDiversity(items, policy)
	excluded := append(admission.excluded, conflict.excluded...)
	excluded = append(excluded, superseded...)
	excluded = append(excluded, duplicated...)
	excluded = append(excluded, capped...)
	trace, err := basicTrace(snapshot, tokenizerID, rendererID, excluded, admission.defaultsFilled)
	if err != nil {
		return nil, nil, err
	}
	trace["conflicts"] = conflict.rows
	reason, err := basicRefusal(items, asObject(snapshot["profile"]), policy["parser"] == true)
	if err != nil {
		return nil, nil, err
	}
	if reason != "" {
		trace["refused"] = map[string]any{"bool": true, "reason": reason}
	} else if conflict.unresolved {
		trace["refused"] = map[string]any{"bool": true, "reason": "conflict_unresolved"}
		if conflict.requestContextOnly {
			trace["recovery"] = map[string]any{"action": "request_context"}
		}
	}
	return items, trace, nil
}

// basicRefusal evaluates the first two refusal conditions in registry order.
func basicRefusal(items []*basicItem, profile map[string]any, parser bool) (string, error) {
	present := map[string]bool{}
	for _, item := range items {
		present[item.slot] = true
	}
	for _, required := range []string{"governance.instructions", "interaction.query"} {
		if !present[required] {
			return "required_slot_missing", nil
		}
	}
	if parser && !present["governance.output_contract"] {
		return "required_slot_missing", nil
	}
	placed := map[string]bool{}
	for _, raw := range asArray(profile["placement"]) {
		placed[asString(asObject(raw)["slot"])] = true
	}
	for _, item := range items {
		if !placed[item.slot] {
			if item.tier != "protected" {
				return "", featureGap("unplaced nonprotected item")
			}
			return "protected_slot_unplaced", nil
		}
	}
	return "", nil
}

func basicTrace(snapshot map[string]any, tokenizerID, rendererID string, excluded, defaultsFilled []any) (map[string]any, error) {
	digest, err := snapshotDigest(snapshot)
	if err != nil {
		return nil, err
	}
	profile := asObject(snapshot["profile"])
	policy := asObject(snapshot["route_policy"])
	trace := map[string]any{
		"trace_id": digest,
		"profile":  map[string]any{"id": profile["id"], "version": profile["version"]},
		"budget":   snapshot["budget"],
		"result":   nil,
		"included": []any{}, "compressed": []any{}, "excluded": excluded,
		"conflicts": []any{}, "refused": map[string]any{"bool": false, "reason": nil},
		"context": map[string]any{
			"spec": profile["spec"], "assembly_time": snapshot["assembly_time"],
			"route_policy_version": policy["version"], "tokenizer": tokenizerID,
			"renderer": rendererID, "snapshot_digest": digest,
		},
		"defaults_filled": defaultsFilled,
	}
	return trace, nil
}

// basicKindSlotAllowed limits a producer's slots by its kind, whatever the route lists (R-8, R-13,
// R-14, R-15). It only narrows the route's listing: an item must pass both.
func basicKindSlotAllowed(kind, slot string) bool {
	if strings.HasPrefix(slot, "state.") {
		return kind == "state"
	}
	switch kind {
	case "state":
		return false
	case "memory":
		return slot == "interaction.memory"
	case "retrieval":
		return slot == "evidence.knowledge" || slot == "evidence.tool_results"
	case "mcp":
		// A tool specification may reach governance.capabilities where the route lists that slot
		// for the producer, and there meets the capability check's capability_not_allowed (R-15).
		return slot == "evidence.knowledge" || slot == "evidence.tool_results" || slot == "governance.capabilities"
	case "capability_policy":
		return slot == "governance.capabilities"
	default:
		return true
	}
}

var xmlBodyEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
var xmlAttrEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;")

func escapeXML(text string) string { return xmlBodyEscaper.Replace(text) }

func renderBasicXML(snapshot map[string]any, items []*basicItem, tokenizer Tokenizer) ([]byte, []any, error) {
	var payload strings.Builder
	included := []any{}
	profile := asObject(snapshot["profile"])
	for _, rawPlacement := range asArray(profile["placement"]) {
		placement := asObject(rawPlacement)
		slot := asString(placement["slot"])
		tag := strings.TrimPrefix(asString(placement["wrap"]), "xml:")
		for _, item := range placementItems(items, slot) {
			payload.WriteString("<")
			payload.WriteString(tag)
			payload.WriteString(" id=\"")
			payload.WriteString(xmlAttrEscaper.Replace(item.id))
			if item.conflictID != "" {
				payload.WriteString("\" conflict=\"")
				payload.WriteString(xmlAttrEscaper.Replace(item.conflictID))
			}
			payload.WriteString("\">\n")
			payload.WriteString(item.body)
			payload.WriteString("\n</")
			payload.WriteString(tag)
			payload.WriteString(">\n")
			included = append(included, map[string]any{
				"slot": item.slot, "item_id": item.id, "tokens": item.bodyTokens,
				"source_version": item.sourceVersion, "eligibility": item.eligibility,
			})
		}
	}
	return []byte(payload.String()), included, nil
}
