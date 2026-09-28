package assembler

import (
	"errors"
	"sort"
	"strings"

	"github.com/ucarion/jcs"
)

func renderBasic(snapshot map[string]any, items []*basicItem, rendererID string, tokenizer Tokenizer) ([]byte, []any, int, error) {
	if rendererID == "cwa-messages/v1" {
		return renderMessages(snapshot, items, tokenizer)
	}
	payload, included, err := renderBasicXML(snapshot, items, tokenizer)
	if err != nil {
		return nil, nil, 0, err
	}
	return payload, included, tokenizer(string(payload)), nil
}

func renderMessages(snapshot map[string]any, items []*basicItem, tokenizer Tokenizer) ([]byte, []any, int, error) {
	system, tools, included := []any{}, []any{}, []any{}
	var content strings.Builder
	count := 0
	for _, raw := range asArray(asObject(snapshot["profile"])["placement"]) {
		placement := asObject(raw)
		slot, wrap := asString(placement["slot"]), asString(placement["wrap"])
		for _, item := range placementItems(items, slot) {
			body := item.body
			if wrap == "system" || wrap == "tools" {
				body = asString(item.data["body"])
				entry := map[string]any{"id": item.id, "text": body}
				if item.conflictID != "" {
					entry["conflict"] = item.conflictID
				}
				if wrap == "system" {
					system = append(system, entry)
				} else {
					tools = append(tools, entry)
				}
				textCount := tokenizer(body)
				if textCount < 0 {
					return nil, nil, 0, errors.New("tokenizer returned a negative count")
				}
				count += textCount
			} else {
				content.WriteString(messageXML(item, strings.TrimPrefix(wrap, "xml:")))
			}
			bodyCount := tokenizer(body)
			if bodyCount < 0 {
				return nil, nil, 0, errors.New("tokenizer returned a negative count")
			}
			included = append(included, map[string]any{
				"slot": slot, "item_id": item.id, "tokens": bodyCount,
				"source_version": item.sourceVersion, "eligibility": item.eligibility,
			})
		}
	}
	messageContent := content.String()
	contentCount := tokenizer(messageContent)
	if contentCount < 0 {
		return nil, nil, 0, errors.New("tokenizer returned a negative count")
	}
	count += contentCount
	encoded, err := jcs.Format(map[string]any{
		"system": system, "tools": tools,
		"messages": []any{map[string]any{"role": "user", "content": messageContent}},
	})
	if err != nil {
		return nil, nil, 0, err
	}
	return []byte(encoded), included, count, nil
}

func placementItems(items []*basicItem, slot string) []*basicItem {
	selected := []*basicItem{}
	for _, item := range items {
		if item.slot == slot {
			selected = append(selected, item)
		}
	}
	sort.Slice(selected, func(i, j int) bool { return utf16Less(selected[i].id, selected[j].id) })
	return selected
}

func messageXML(item *basicItem, tag string) string {
	var content strings.Builder
	content.WriteByte('<')
	content.WriteString(tag)
	content.WriteString(" id=\"")
	content.WriteString(xmlAttrEscaper.Replace(item.id))
	content.WriteByte('"')
	if item.slot == "interaction.history" {
		speaker := "user"
		if item.data["lineage"] == "generated" {
			speaker = "assistant"
		}
		content.WriteString(" speaker=\"")
		content.WriteString(speaker)
		content.WriteByte('"')
	}
	if item.conflictID != "" {
		content.WriteString(" conflict=\"")
		content.WriteString(xmlAttrEscaper.Replace(item.conflictID))
		content.WriteByte('"')
	}
	content.WriteString(">\n")
	content.WriteString(item.body)
	content.WriteString("\n</")
	content.WriteString(tag)
	content.WriteString(">\n")
	return content.String()
}
