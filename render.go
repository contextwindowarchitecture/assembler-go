package assembler

import (
	"errors"
	"sort"
	"strings"

	"github.com/ucarion/jcs"
)

func renderBasic(snapshot map[string]any, items []*basicItem, rendererID string, tokenizer Tokenizer) ([]byte, []any, int, error) {
	switch rendererID {
	case "cwa-messages/v1":
		return renderMessages(snapshot, items, tokenizer)
	case "cwa-message-blocks/v1":
		return renderMessageBlocks(snapshot, items, tokenizer)
	}
	payload, included, err := renderBasicXML(snapshot, items, tokenizer)
	if err != nil {
		return nil, nil, 0, err
	}
	return payload, included, tokenizer(string(payload)), nil
}

// messageRenderer reports whether a renderer writes a message request, as cwa-messages/v1 and
// cwa-message-blocks/v1 do: a system or tools entry holds the unescaped body (R-10), and an xml:
// occurrence renders escaped into the user message.
func messageRenderer(rendererID string) bool {
	return rendererID == "cwa-messages/v1" || rendererID == "cwa-message-blocks/v1"
}

// messageRequest is what cwa-messages/v1 and cwa-message-blocks/v1 share: the system and tools
// entries, one entry per xml: occurrence with its text as cwa-messages/v1 writes it into the user
// message, the included rows, and the count of the system and tools texts.
type messageRequest struct {
	system, tools, content, included []any
	count                            int
}

func buildMessageRequest(snapshot map[string]any, items []*basicItem, tokenizer Tokenizer) (messageRequest, error) {
	request := messageRequest{system: []any{}, tools: []any{}, content: []any{}, included: []any{}}
	for _, raw := range asArray(asObject(snapshot["profile"])["placement"]) {
		placement := asObject(raw)
		slot, wrap := asString(placement["slot"]), asString(placement["wrap"])
		for _, item := range placementItems(items, slot) {
			entry := map[string]any{"id": item.id}
			if item.conflictID != "" {
				entry["conflict"] = item.conflictID
			}
			body := item.body
			if wrap == "system" || wrap == "tools" {
				body = item.bodyRaw
				text := body
				if item.conflictID != "" {
					text = conflictMark(item.conflictID, body)
				}
				entry["text"] = text
				if wrap == "system" {
					request.system = append(request.system, entry)
				} else {
					request.tools = append(request.tools, entry)
				}
				// The mark is wrapper text: it counts here, in the payload's count, and never in
				// the occurrence's own tokens below, which count the unescaped body alone.
				textCount, err := countText(tokenizer, text)
				if err != nil {
					return messageRequest{}, err
				}
				request.count += textCount
			} else {
				entry["text"] = messageXML(item, strings.TrimPrefix(wrap, "xml:"))
				request.content = append(request.content, entry)
			}
			bodyCount, err := countText(tokenizer, body)
			if err != nil {
				return messageRequest{}, err
			}
			request.included = append(request.included, map[string]any{
				"slot": slot, "item_id": item.id, "tokens": bodyCount,
				"source_version": item.sourceVersion, "eligibility": item.eligibility,
			})
		}
	}
	return request, nil
}

// encode writes the request as RFC 8785 JSON, with content as the one user message's content.
func (request messageRequest) encode(content any) ([]byte, error) {
	encoded, err := jcs.Format(map[string]any{
		"system": request.system, "tools": request.tools,
		"messages": []any{map[string]any{"role": "user", "content": content}},
	})
	return []byte(encoded), err
}

// renderMessages renders cwa-messages/v1: the user message's content is the xml: occurrences'
// texts joined in order, and the payload's count adds the count of that one text to the system
// and tools texts' counts.
func renderMessages(snapshot map[string]any, items []*basicItem, tokenizer Tokenizer) ([]byte, []any, int, error) {
	request, err := buildMessageRequest(snapshot, items, tokenizer)
	if err != nil {
		return nil, nil, 0, err
	}
	var content strings.Builder
	for _, entry := range request.content {
		content.WriteString(asString(asObject(entry)["text"]))
	}
	contentCount, err := countText(tokenizer, content.String())
	if err != nil {
		return nil, nil, 0, err
	}
	payload, err := request.encode(content.String())
	if err != nil {
		return nil, nil, 0, err
	}
	return payload, request.included, request.count + contentCount, nil
}

// renderMessageBlocks renders cwa-message-blocks/v1, the optional renderer: the request
// cwa-messages/v1 renders, with the user message's content as one {id, text} entry per xml:
// occurrence, in the same order. A surfaced conflict member's entry also names its group, as a
// system entry does, and keeps its mark in its text. The payload's count sums the tokenizer's
// count of every entry's text, system, tools and message alike, each on its own; a tokenizer that
// does not count a whole as the sum of its parts can count more here than for the joined text.
func renderMessageBlocks(snapshot map[string]any, items []*basicItem, tokenizer Tokenizer) ([]byte, []any, int, error) {
	request, err := buildMessageRequest(snapshot, items, tokenizer)
	if err != nil {
		return nil, nil, 0, err
	}
	count := request.count
	for _, entry := range request.content {
		textCount, err := countText(tokenizer, asString(asObject(entry)["text"]))
		if err != nil {
			return nil, nil, 0, err
		}
		count += textCount
	}
	payload, err := request.encode(request.content)
	if err != nil {
		return nil, nil, 0, err
	}
	return payload, request.included, count, nil
}

func countText(tokenizer Tokenizer, text string) (int, error) {
	count := tokenizer(text)
	if count < 0 {
		return 0, errors.New("tokenizer returned a negative count")
	}
	return count, nil
}

// conflictMark puts a surfaced conflict member's mark into its system or tools text, since an
// application hands the model each entry's text and nothing else (R-11). The body stays unescaped
// (R-10); the group id is escaped as fixture-xml/v1 escapes attribute values.
func conflictMark(groupID, body string) string {
	return "<conflict group=\"" + xmlAttrEscaper.Replace(groupID) + "\">\n" + body + "\n</conflict>"
}

func placementItems(items []*basicItem, slot string) []*basicItem {
	selected := []*basicItem{}
	for _, item := range items {
		if item.slot == slot {
			selected = append(selected, item)
		}
	}
	sort.Slice(selected, func(i, j int) bool { return placementLess(selected[i], selected[j]) })
	return selected
}

// placementLess orders the items of one placement by id, in UTF-16 code units, except in
// interaction.history, whose turns render in the order they were said: by freshness compared as
// instants at full precision, and by id only among turns said at the same instant (R-7; README,
// Ordering). Admission has already checked every freshness, so the comparison cannot fail here.
func placementLess(left, right *basicItem) bool {
	if left.slot == "interaction.history" && right.slot == "interaction.history" {
		if cmp, _ := compareInstants(asString(left.data["freshness"]), asString(right.data["freshness"])); cmp != 0 {
			return cmp < 0
		}
	}
	return utf16Less(left.id, right.id)
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

func compressedRows(snapshot map[string]any, items []*basicItem, rendererID string, tokenizer Tokenizer) ([]any, error) {
	rows := []any{}
	for _, raw := range asArray(asObject(snapshot["profile"])["placement"]) {
		placement := asObject(raw)
		slot, wrap := asString(placement["slot"]), placement["wrap"]
		for _, item := range placementItems(items, slot) {
			if item.variant == nil {
				continue
			}
			before := item.initialBody
			if messageRenderer(rendererID) && (wrap == "system" || wrap == "tools") {
				before = item.initialRaw
			}
			from, to := tokenizer(before), tokenizer(bodyForWrap(item, rendererID, wrap))
			if from < 0 || to < 0 {
				return nil, errors.New("tokenizer returned a negative count")
			}
			rows = append(rows, map[string]any{
				"slot": slot, "item_id": item.id, "from": from, "to": to,
				"method": item.variant["method"], "variant_id": item.variant["id"],
			})
		}
	}
	return rows, nil
}
