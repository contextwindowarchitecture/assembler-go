package assembler

import "testing"

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
