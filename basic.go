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
	"github.com/santhosh-tekuri/jsonschema/v6"
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

type basicAdmission struct {
	schema       *jsonschema.Schema
	tokenizer    Tokenizer
	defaults     map[string]map[string]any
	producers    map[string]any
	assemblyTime string
	requestScope map[string]any
	seenIDs      map[string]bool
	items        []*basicItem
	excluded     []any
}

func admitBasic(snapshot map[string]any, tokenizer Tokenizer) ([]*basicItem, []any, error) {
	compiled, err := itemSchema()
	if err != nil {
		return nil, nil, err
	}
	defaults, err := slotDefaults()
	if err != nil {
		return nil, nil, err
	}
	a := &basicAdmission{
		schema: compiled, tokenizer: tokenizer, defaults: defaults,
		producers:    asObject(asObject(snapshot["route_policy"])["producers"]),
		assemblyTime: asString(snapshot["assembly_time"]), requestScope: asObject(snapshot["scope"]),
		seenIDs: map[string]bool{}, items: []*basicItem{}, excluded: []any{},
	}
	batches := append([]any(nil), asArray(snapshot["batches"])...)
	sort.Slice(batches, func(i, j int) bool {
		left := asString(asObject(asObject(batches[i])["producer"])["id"])
		right := asString(asObject(asObject(batches[j])["producer"])["id"])
		return utf16Less(left, right)
	})
	for _, rawBatch := range batches {
		if err := a.addBatch(asObject(rawBatch)); err != nil {
			return nil, nil, err
		}
	}
	return a.items, a.excluded, nil
}

func (a *basicAdmission) addBatch(batch map[string]any) error {
	producer := asObject(batch["producer"])
	producerID, producerKind := asString(producer["id"]), asString(producer["kind"])
	ruleValue, ok := a.producers[producerID]
	if !ok || asObject(ruleValue)["kind"] != producerKind {
		return featureGap("producer authentication exclusion")
	}
	if err := a.addProducerExclusions(asArray(batch["excluded"])); err != nil {
		return err
	}
	allowedSlots := asArray(asObject(ruleValue)["slots"])
	for _, rawItem := range asArray(batch["items"]) {
		if err := a.addItem(rawItem, producerKind, allowedSlots); err != nil {
			return err
		}
	}
	return nil
}

func (a *basicAdmission) addProducerExclusions(rawRows []any) error {
	rows, err := sortedRows(rawRows, "item_id")
	if err != nil {
		return err
	}
	for _, rawRow := range rows {
		row := asObject(rawRow)
		id := asString(row["item_id"])
		if a.seenIDs[id] {
			return featureGap("duplicate producer exclusion id")
		}
		a.seenIDs[id] = true
		a.excluded = append(a.excluded, row)
	}
	return nil
}

func (a *basicAdmission) addItem(rawItem any, producerKind string, allowedSlots []any) error {
	if err := a.schema.Validate(rawItem); err != nil {
		return featureGap("invalid item admission")
	}
	item := asObject(rawItem)
	id, slot := asString(item["id"]), asString(item["slot"])
	if a.seenIDs[id] {
		return featureGap("duplicate item admission")
	}
	a.seenIDs[id] = true
	allowed := false
	for _, candidateSlot := range allowedSlots {
		allowed = allowed || candidateSlot == slot
	}
	if !allowed || !basicKindSlotAllowed(producerKind, slot) {
		return featureGap("producer slot admission")
	}
	if err := a.checkMetadata(item); err != nil {
		return err
	}
	if err := a.checkTimeAndScope(item); err != nil {
		return err
	}
	body := escapeXML(asString(item["body"]))
	bodyTokens := a.tokenizer(body)
	if bodyTokens < 0 {
		return errors.New("tokenizer returned a negative count")
	}
	if cap, ok := item["token_budget"].(float64); ok && float64(bodyTokens) > cap {
		return featureGap("item token cap")
	}
	a.items = append(a.items, &basicItem{id: id, slot: slot, body: body,
		sourceVersion: asString(item["source_version"]), eligibility: asString(item["eligibility"]),
		bodyTokens: bodyTokens})
	return nil
}

func (a *basicAdmission) checkMetadata(item map[string]any) error {
	for _, field := range []string{"token_budget", "variants", "conflict_policy", "lineage", "eligibility", "injection_risk"} {
		if _, ok := item[field]; !ok {
			return featureGap("item defaults")
		}
	}
	if len(asArray(item["variants"])) != 0 || item["revoked_by"] != nil {
		return featureGap("variants or revocation")
	}
	slot := asString(item["slot"])
	slotDefault := a.defaults[slot]
	if item["authority"] != slotDefault["authority"] || item["injection_risk"] != slotDefault["injection_risk"] {
		return featureGap("authority or injection risk admission")
	}
	if strings.HasPrefix(slot, "governance.") && item["trust"] != "verified" {
		return featureGap("governance trust admission")
	}
	if tier, ok := item["tier"].(string); ok && tier != slotDefault["tier"] {
		return featureGap("tier admission")
	}
	return nil
}

func (a *basicAdmission) checkTimeAndScope(item map[string]any) error {
	if itemScope, ok := item["scope"].(map[string]any); ok {
		for key, value := range itemScope {
			if a.requestScope[key] != value {
				return featureGap("scope admission")
			}
		}
	}
	freshness, err := compareInstants(asString(item["freshness"]), a.assemblyTime)
	if err != nil {
		return err
	}
	if freshness > 0 {
		return featureGap("future freshness admission")
	}
	if expires, ok := item["expires"].(string); ok {
		remaining, err := compareInstants(expires, a.assemblyTime)
		if err != nil {
			return err
		}
		if remaining <= 0 {
			return featureGap("expiration admission")
		}
	}
	return nil
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
