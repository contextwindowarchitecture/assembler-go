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
	id, slot, body, sourceVersion, eligibility, tier string
	bodyTokens                                       int
	data                                             map[string]any
	producerID, groupID, conflictID                  string
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
	payload, included, count, err := renderBasic(snapshot, items, rendererID, tokenizer)
	if err != nil {
		return Result{}, err
	}
	if count < 0 {
		return Result{}, errors.New("tokenizer returned a negative count")
	}
	budget := asObject(snapshot["budget"])
	margin := 0
	if value, ok := budget["margin_percent"].(float64); ok {
		margin = int(value)
	}
	if count > int(^uint(0)>>1)/(100+margin) {
		return Result{}, featureGap("oversized token count")
	}
	if float64((count*(100+margin)+99)/100) > budget["input"].(float64) {
		return Result{}, featureGap("budget fitting")
	}
	hash := sha256.Sum256(payload)
	trace["result"] = map[string]any{"input_tokens": count, "hash": hex.EncodeToString(hash[:])}
	trace["included"] = included
	return Result{Payload: payload, Trace: trace}, nil
}

func prepareBasic(snapshot map[string]any, tokenizer Tokenizer, tokenizerID, rendererID string) ([]*basicItem, map[string]any, error) {
	policy := asObject(snapshot["route_policy"])
	allowed := map[string]bool{
		"route": true, "version": true, "producers": true, "slots": true,
		"default_overrides": true, "tier_upgrades": true, "clock_skew_seconds": true,
		"parser": true, "on_unresolved_instruction": true, "facts": true,
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

func basicKindSlotAllowed(kind, slot string) bool {
	if strings.HasPrefix(slot, "state.") {
		return kind == "state"
	}
	switch kind {
	case "state":
		return false
	case "memory":
		return slot == "interaction.memory"
	case "retrieval", "mcp":
		return slot == "evidence.knowledge" || slot == "evidence.tool_results"
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
		placed := []*basicItem{}
		for _, item := range items {
			if item.slot == slot {
				placed = append(placed, item)
			}
		}
		sort.Slice(placed, func(i, j int) bool { return utf16Less(placed[i].id, placed[j].id) })
		for _, item := range placed {
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
