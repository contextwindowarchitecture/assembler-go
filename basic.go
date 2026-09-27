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
	id, slot, body, sourceVersion, eligibility string
	bodyTokens                                 int
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
	if rendererID != "fixture-xml/v1" {
		return Result{}, featureGap("cwa-messages rendering")
	}
	policy := asObject(snapshot["route_policy"])
	keys := make([]string, 0, len(policy))
	for key := range policy {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return utf16Less(keys[i], keys[j]) })
	for _, key := range keys {
		if key != "route" && key != "version" && key != "producers" {
			return Result{}, featureGap("route policy " + key)
		}
	}
	if len(asArray(snapshot["conflicts"])) != 0 || snapshot["capabilities"] != nil {
		return Result{}, featureGap("conflicts or capabilities")
	}
	items, excluded, err := admitBasic(snapshot, tokenizer)
	if err != nil {
		return Result{}, err
	}
	placed := map[string]bool{}
	for _, rawPlacement := range asArray(asObject(snapshot["profile"])["placement"]) {
		placed[asString(asObject(rawPlacement)["slot"])] = true
	}
	for _, item := range items {
		if !placed[item.slot] {
			return Result{}, featureGap("unplaced slot handling")
		}
	}
	for _, required := range []string{"governance.instructions", "interaction.query"} {
		found := false
		for _, item := range items {
			found = found || item.slot == required
		}
		if !found {
			return Result{}, featureGap("required slot refusal")
		}
	}
	payload, included, err := renderBasicXML(snapshot, items, tokenizer)
	if err != nil {
		return Result{}, err
	}
	count := tokenizer(string(payload))
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
	digest, err := snapshotDigest(snapshot)
	if err != nil {
		return Result{}, err
	}
	hash := sha256.Sum256(payload)
	profile := asObject(snapshot["profile"])
	trace := map[string]any{
		"trace_id": digest,
		"profile":  map[string]any{"id": profile["id"], "version": profile["version"]},
		"budget":   budget,
		"result":   map[string]any{"input_tokens": count, "hash": hex.EncodeToString(hash[:])},
		"included": included, "compressed": []any{}, "excluded": excluded,
		"conflicts": []any{}, "refused": map[string]any{"bool": false, "reason": nil},
		"context": map[string]any{
			"spec": profile["spec"], "assembly_time": snapshot["assembly_time"],
			"route_policy_version": policy["version"], "tokenizer": tokenizerID,
			"renderer": rendererID, "snapshot_digest": digest,
		},
		"defaults_filled": []any{},
	}
	return Result{Payload: payload, Trace: trace}, nil
}

func admitBasic(snapshot map[string]any, tokenizer Tokenizer) ([]*basicItem, []any, error) {
	itemSchema, err := itemSchema()
	if err != nil {
		return nil, nil, err
	}
	defaults, err := slotDefaults()
	if err != nil {
		return nil, nil, err
	}
	producers := asObject(asObject(snapshot["route_policy"])["producers"])
	batches := append([]any(nil), asArray(snapshot["batches"])...)
	sort.Slice(batches, func(i, j int) bool {
		left := asString(asObject(asObject(batches[i])["producer"])["id"])
		right := asString(asObject(asObject(batches[j])["producer"])["id"])
		return utf16Less(left, right)
	})
	seenIDs := map[string]bool{}
	items := []*basicItem{}
	excluded := []any{}
	assemblyTime := asString(snapshot["assembly_time"])
	requestScope := asObject(snapshot["scope"])
	for _, rawBatch := range batches {
		batch := asObject(rawBatch)
		producer := asObject(batch["producer"])
		producerID, producerKind := asString(producer["id"]), asString(producer["kind"])
		ruleValue, ok := producers[producerID]
		if !ok || asObject(ruleValue)["kind"] != producerKind {
			return nil, nil, featureGap("producer authentication exclusion")
		}
		allowedSlots := asArray(asObject(ruleValue)["slots"])
		rows, err := sortedRows(asArray(batch["excluded"]), "item_id")
		if err != nil {
			return nil, nil, err
		}
		for _, rawRow := range rows {
			row := asObject(rawRow)
			id := asString(row["item_id"])
			if seenIDs[id] {
				return nil, nil, featureGap("duplicate producer exclusion id")
			}
			seenIDs[id] = true
			excluded = append(excluded, row)
		}
		for _, rawItem := range asArray(batch["items"]) {
			if err := itemSchema.Validate(rawItem); err != nil {
				return nil, nil, featureGap("invalid item admission")
			}
			item := asObject(rawItem)
			id, slot := asString(item["id"]), asString(item["slot"])
			if seenIDs[id] {
				return nil, nil, featureGap("duplicate item admission")
			}
			seenIDs[id] = true
			allowed := false
			for _, candidateSlot := range allowedSlots {
				allowed = allowed || candidateSlot == slot
			}
			if !allowed || !basicKindSlotAllowed(producerKind, slot) {
				return nil, nil, featureGap("producer slot admission")
			}
			for _, field := range []string{"token_budget", "variants", "conflict_policy", "lineage", "eligibility", "injection_risk"} {
				if _, ok := item[field]; !ok {
					return nil, nil, featureGap("item defaults")
				}
			}
			if len(asArray(item["variants"])) != 0 || item["revoked_by"] != nil {
				return nil, nil, featureGap("variants or revocation")
			}
			slotDefault := defaults[slot]
			if item["authority"] != slotDefault["authority"] || item["injection_risk"] != slotDefault["injection_risk"] {
				return nil, nil, featureGap("authority or injection risk admission")
			}
			if strings.HasPrefix(slot, "governance.") && item["trust"] != "verified" {
				return nil, nil, featureGap("governance trust admission")
			}
			if tier, ok := item["tier"].(string); ok && tier != slotDefault["tier"] {
				return nil, nil, featureGap("tier admission")
			}
			if itemScope, ok := item["scope"].(map[string]any); ok {
				for key, value := range itemScope {
					if requestScope[key] != value {
						return nil, nil, featureGap("scope admission")
					}
				}
			}
			freshness, err := compareInstants(asString(item["freshness"]), assemblyTime)
			if err != nil {
				return nil, nil, err
			}
			if freshness > 0 {
				return nil, nil, featureGap("future freshness admission")
			}
			if expires, ok := item["expires"].(string); ok {
				remaining, err := compareInstants(expires, assemblyTime)
				if err != nil {
					return nil, nil, err
				}
				if remaining <= 0 {
					return nil, nil, featureGap("expiration admission")
				}
			}
			body := escapeXML(asString(item["body"]))
			bodyTokens := tokenizer(body)
			if bodyTokens < 0 {
				return nil, nil, errors.New("tokenizer returned a negative count")
			}
			if cap, ok := item["token_budget"].(float64); ok && float64(bodyTokens) > cap {
				return nil, nil, featureGap("item token cap")
			}
			items = append(items, &basicItem{id: id, slot: slot, body: body,
				sourceVersion: asString(item["source_version"]), eligibility: asString(item["eligibility"]),
				bodyTokens: bodyTokens})
		}
	}
	return items, excluded, nil
}

func basicKindSlotAllowed(kind, slot string) bool {
	switch kind {
	case "state":
		return strings.HasPrefix(slot, "state.")
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
