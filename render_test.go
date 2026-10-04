package assembler

import (
	"encoding/json"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// TestMessagesMarkSurfacedMembersInText checks cwa-messages/v1 against its README rule: a surfaced
// conflict member placed with system or tools carries the mark in its text, since the text is all the
// model receives (R-11). The wrapper counts only in the whole payload's count; included and compressed
// rows count the unescaped body alone.
func TestMessagesMarkSurfacedMembersInText(t *testing.T) {
	snapshot := map[string]any{"profile": map[string]any{"placement": []any{
		map[string]any{"slot": "governance.instructions", "wrap": "system"},
		map[string]any{"slot": "governance.examples", "wrap": "system"},
		map[string]any{"slot": "governance.capabilities", "wrap": "tools"},
		map[string]any{"slot": "interaction.query", "wrap": "xml:query"},
	}}}
	item := func(id, slot, raw, conflictID string) *basicItem {
		return &basicItem{id: id, slot: slot, body: escapeXML(raw), bodyRaw: raw,
			initialBody: escapeXML(raw), initialRaw: raw, conflictID: conflictID, data: map[string]any{}}
	}
	compressed := item("ex:1", "governance.examples", "Short one.", "g-ex")
	compressed.initialRaw = "A much longer example."
	compressed.initialBody = compressed.initialRaw
	compressed.variant = map[string]any{"id": "ex:1/short", "method": "summary"}
	items := []*basicItem{
		item("policy:b", "governance.instructions", "Be brief.", ""),
		item("policy:a", "governance.instructions", "Cite a & <b> sources.", `g"&<>`),
		compressed,
		item("cap:x", "governance.capabilities", `{"name": "x"}`, "g-tool"),
		item("turn:1", "interaction.query", "Hi?", ""),
	}

	payload, included, count, err := renderBasic(snapshot, items, "cwa-messages/v1", fixtureWhitespace)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"messages":[{"content":"<query id=\"turn:1\">\nHi?\n</query>\n","role":"user"}],` +
		`"system":[{"conflict":"g\"&<>","id":"policy:a","text":"<conflict group=\"g&quot;&amp;&lt;&gt;\">\nCite a & <b> sources.\n</conflict>"},` +
		`{"id":"policy:b","text":"Be brief."},` +
		`{"conflict":"g-ex","id":"ex:1","text":"<conflict group=\"g-ex\">\nShort one.\n</conflict>"}],` +
		`"tools":[{"conflict":"g-tool","id":"cap:x","text":"<conflict group=\"g-tool\">\n{\"name\": \"x\"}\n</conflict>"}]}`
	if string(payload) != want {
		t.Errorf("payload\n got %s\nwant %s", payload, want)
	}
	// Each entry's text and the message content: 8 + 2 + 5 + 5 + 4.
	if count != 24 {
		t.Errorf("input tokens = %d, want 24", count)
	}
	wantTokens := map[string]float64{"policy:a": 5, "policy:b": 2, "ex:1": 2, "cap:x": 2, "turn:1": 1}
	if len(included) != len(wantTokens) {
		t.Fatalf("included %d rows, want %d", len(included), len(wantTokens))
	}
	for _, raw := range included {
		row := asObject(raw)
		id := asString(row["item_id"])
		if got, ok := row["tokens"].(int); !ok || float64(got) != wantTokens[id] {
			t.Errorf("included %s tokens = %v, want %v", id, row["tokens"], wantTokens[id])
		}
	}

	rows, err := compressedRows(snapshot, items, "cwa-messages/v1", fixtureWhitespace)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("compressed %d rows, want 1", len(rows))
	}
	row := asObject(rows[0])
	if row["item_id"] != "ex:1" || row["from"] != 4 || row["to"] != 2 {
		t.Errorf("compressed row = %v, want ex:1 from 4 to 2", row)
	}
}

// TestHistoryRendersInTheOrderSaid follows R-7 and the README's Ordering: within a placement items
// go by id, except interaction.history, whose turns go by freshness compared as instants at full
// precision, and by id only among turns said at the same instant. Both renderers order the same
// way, and included[] follows that order.
func TestHistoryRendersInTheOrderSaid(t *testing.T) {
	snapshot := map[string]any{"profile": map[string]any{"placement": []any{
		map[string]any{"slot": "governance.instructions", "wrap": "xml:rules"},
		map[string]any{"slot": "interaction.history", "wrap": "xml:turn"},
		map[string]any{"slot": "interaction.query", "wrap": "xml:query"},
	}}}
	item := func(id, slot, freshness string) *basicItem {
		return &basicItem{id: id, slot: slot, body: id, bodyRaw: id, initialBody: id, initialRaw: id,
			data: map[string]any{"freshness": freshness}}
	}
	items := []*basicItem{
		// Elsewhere freshness plays no part: rule:b was written first and still follows rule:a.
		item("rule:b", "governance.instructions", "2026-09-22T11:00:00Z"),
		item("rule:a", "governance.instructions", "2026-09-22T12:00:00Z"),
		item("turn:9", "interaction.history", "2026-09-22T11:46:00Z"),
		// The same instant as turn:9 written another way, so id breaks the tie.
		item("turn:10", "interaction.history", "2026-09-22T13:46:00+02:00"),
		// A fraction beyond nanoseconds still orders.
		item("turn:2", "interaction.history", "2026-09-22T11:46:00.0000000001Z"),
		item("turn:1", "interaction.history", "2026-09-22T11:47:00Z"),
		item("turn:3", "interaction.query", "2026-09-22T11:59:00Z"),
	}
	want := []string{"rule:a", "rule:b", "turn:10", "turn:9", "turn:2", "turn:1", "turn:3"}
	for _, renderer := range []string{"fixture-xml/v1", "cwa-messages/v1"} {
		payload, included, _, err := renderBasic(snapshot, items, renderer, fixtureWhitespace)
		if err != nil {
			t.Fatal(err)
		}
		var rows, rendered []string
		for _, raw := range included {
			rows = append(rows, asString(asObject(raw)["item_id"]))
		}
		for _, match := range renderedID.FindAllStringSubmatch(string(payload), -1) {
			rendered = append(rendered, match[1])
		}
		if !reflect.DeepEqual(rows, want) || !reflect.DeepEqual(rendered, want) {
			t.Errorf("%s: included %v and rendered %v, want %v", renderer, rows, rendered, want)
		}
	}
}

var renderedID = regexp.MustCompile(`id=\\?"([^"\\]+)\\?"`)

// TestMessageBlocksRenderOneEntryPerOccurrence checks cwa-message-blocks/v1 against its README
// rule: the request cwa-messages/v1 renders, except that the user message's content is one
// {id, text} entry per xml: occurrence, each text exactly as cwa-messages/v1 writes it, and a
// surfaced member's entry names its group in conflict, as a system entry does (R-7, R-11). The
// payload's count sums the tokenizer's count of every entry's text, so estimate-utf8/v1, which
// rounds each text up, counts more than for the joined text. Per-item tokens and compressed rows
// count as in cwa-messages/v1.
func TestMessageBlocksRenderOneEntryPerOccurrence(t *testing.T) {
	snapshot := map[string]any{"profile": map[string]any{"placement": []any{
		map[string]any{"slot": "governance.instructions", "wrap": "system"},
		map[string]any{"slot": "governance.capabilities", "wrap": "tools"},
		map[string]any{"slot": "governance.examples", "wrap": "xml:examples"},
		map[string]any{"slot": "interaction.history", "wrap": "xml:history"},
		map[string]any{"slot": "interaction.query", "wrap": "xml:query"},
	}}}
	item := func(id, slot, raw, conflictID, freshness string) *basicItem {
		return &basicItem{id: id, slot: slot, body: escapeXML(raw), bodyRaw: raw,
			initialBody: escapeXML(raw), initialRaw: raw, conflictID: conflictID,
			data: map[string]any{"freshness": freshness}}
	}
	compressed := item("ex:1", "governance.examples", "Short & sweet.", "g-ex", "2026-09-22T11:00:00Z")
	compressed.initialRaw = "A much longer example."
	compressed.initialBody = compressed.initialRaw
	compressed.variant = map[string]any{"id": "ex:1/short", "method": "summary"}
	// A system entry holds the unescaped body, so its compressed row counts the raw texts.
	policy := item("policy:b", "governance.instructions", "Refund <30 days & cite.", "", "2026-09-22T11:00:00Z")
	policy.initialRaw = "Refund orders under thirty days & cite <every> source."
	policy.initialBody = escapeXML(policy.initialRaw)
	policy.variant = map[string]any{"id": "policy:b/short", "method": "summary"}
	assistant := item("turn:2", "interaction.history", "Which order?", "", "2026-09-22T11:47:00Z")
	assistant.data["lineage"] = "generated"
	items := []*basicItem{
		item("policy:a", "governance.instructions", "Cite a & <b> sources.", "g-pol", "2026-09-22T11:00:00Z"),
		policy,
		item("cap:x", "governance.capabilities", `{"name": "x"}`, "", "2026-09-22T11:00:00Z"),
		compressed,
		assistant,
		item("turn:10", "interaction.history", "I bought it.", "", "2026-09-22T11:46:00Z"),
		item("turn:3", "interaction.query", "Hi?", "", "2026-09-22T11:59:00Z"),
	}

	payload, included, count, err := renderBasic(snapshot, items, "cwa-message-blocks/v1", estimateUTF8)
	if err != nil {
		t.Fatal(err)
	}
	texts := []string{
		"<examples id=\"ex:1\" conflict=\"g-ex\">\nShort &amp; sweet.\n</examples>\n",
		"<history id=\"turn:10\" speaker=\"user\">\nI bought it.\n</history>\n",
		"<history id=\"turn:2\" speaker=\"assistant\">\nWhich order?\n</history>\n",
		"<query id=\"turn:3\">\nHi?\n</query>\n",
	}
	want := `{"messages":[{"content":[` +
		`{"conflict":"g-ex","id":"ex:1","text":"<examples id=\"ex:1\" conflict=\"g-ex\">\nShort &amp; sweet.\n</examples>\n"},` +
		`{"id":"turn:10","text":"<history id=\"turn:10\" speaker=\"user\">\nI bought it.\n</history>\n"},` +
		`{"id":"turn:2","text":"<history id=\"turn:2\" speaker=\"assistant\">\nWhich order?\n</history>\n"},` +
		`{"id":"turn:3","text":"<query id=\"turn:3\">\nHi?\n</query>\n"}],"role":"user"}],` +
		`"system":[{"conflict":"g-pol","id":"policy:a","text":"<conflict group=\"g-pol\">\nCite a & <b> sources.\n</conflict>"},` +
		`{"id":"policy:b","text":"Refund <30 days & cite."}],` +
		`"tools":[{"id":"cap:x","text":"{\"name\": \"x\"}"}]}`
	if string(payload) != want {
		t.Errorf("payload\n got %s\nwant %s", payload, want)
	}

	// Joined in order, the entries' texts are the content cwa-messages/v1 renders.
	messages, messagesIncluded, joinedCount, err := renderBasic(snapshot, items, "cwa-messages/v1", estimateUTF8)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Messages []struct{ Content string } `json:"messages"`
	}
	if err := json.Unmarshal(messages, &decoded); err != nil {
		t.Fatal(err)
	}
	if joined := strings.Join(texts, ""); len(decoded.Messages) != 1 || decoded.Messages[0].Content != joined {
		t.Errorf("cwa-messages/v1 content is not the joined entries: %+v", decoded.Messages)
	}

	// Each system, tool and message entry is counted on its own.
	wantCount := estimateUTF8("<conflict group=\"g-pol\">\nCite a & <b> sources.\n</conflict>") +
		estimateUTF8("Refund <30 days & cite.") + estimateUTF8(`{"name": "x"}`)
	for _, text := range texts {
		wantCount += estimateUTF8(text)
	}
	if count != wantCount || count <= joinedCount {
		t.Errorf("input tokens = %d, want %d, above the joined count %d", count, wantCount, joinedCount)
	}
	if !reflect.DeepEqual(included, messagesIncluded) {
		t.Errorf("included\n got %v\nwant %v", included, messagesIncluded)
	}
	rows, err := compressedRows(snapshot, items, "cwa-message-blocks/v1", estimateUTF8)
	if err != nil {
		t.Fatal(err)
	}
	messagesRows, err := compressedRows(snapshot, items, "cwa-messages/v1", estimateUTF8)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || !reflect.DeepEqual(rows, messagesRows) {
		t.Errorf("compressed rows\n got %v\nwant %v", rows, messagesRows)
	}
}
