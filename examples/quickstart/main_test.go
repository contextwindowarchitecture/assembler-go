// The quickstart the README mirrors admits relevant evidence and leaves out the rest (R-13), renders governance
// apart from untrusted material (R-7, R-10), and refuses when the evidence is missing (R-12).
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	assembler "github.com/contextwindowarchitecture/assembler-go"
)

func assemble(t *testing.T, d []doc) assembler.Result {
	t.Helper()
	result, err := assembler.Assemble(buildSnapshot(d), assembler.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func decode(t *testing.T, payload []byte) request {
	t.Helper()
	var r request
	if err := json.Unmarshal(payload, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func excludedRows(trace map[string]any) [][2]any {
	rows := [][2]any{}
	for _, row := range trace["excluded"].([]any) {
		row := row.(map[string]any)
		rows = append(rows, [2]any{row["item_id"], row["reason"]})
	}
	return rows
}

func TestQuickstartSendsTheRelevantChunkAndTracesTheWeakOne(t *testing.T) {
	result := assemble(t, docs)
	r := decode(t, result.Payload)
	if len(r.System) != 1 || r.System[0].ID != "policy:tone" {
		t.Fatalf("system: %+v", r.System)
	}
	if len(r.Messages) != 1 {
		t.Fatalf("messages: %s", r.Messages)
	}
	var message struct{ Role, Content string }
	if err := json.Unmarshal(r.Messages[0], &message); err != nil {
		t.Fatal(err)
	}
	if message.Role != "user" || !strings.Contains(message.Content, "kb:refunds") || strings.Contains(message.Content, "kb:pricing") {
		t.Fatalf("user message: %+v", message)
	}
	if got, want := excludedRows(result.Trace), [][2]any{{"kb:pricing", "below_threshold"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("excluded: %v", got)
	}
}

func TestQuickstartRefusesBeforeAnyModelCallWithoutEvidence(t *testing.T) {
	result := assemble(t, nil)
	if result.Payload != nil {
		t.Fatal("a refusal has no payload")
	}
	if reason := result.Trace["refused"].(map[string]any)["reason"]; reason != "evidence_required" {
		t.Fatalf("refused: %v", reason)
	}
	if action := result.Trace["recovery"].(map[string]any)["action"]; action != "request_context" {
		t.Fatalf("recovery: %v", action)
	}
}

func TestQuickstartMapsThePayloadToProviderRequests(t *testing.T) {
	r := decode(t, assemble(t, docs).Payload)
	anthropic := toAnthropic(r)
	if anthropic.System != r.System[0].Text || !reflect.DeepEqual(anthropic.Messages, r.Messages) || anthropic.Tools != nil {
		t.Fatalf("anthropic: %+v", anthropic)
	}
	openai, err := toOpenAI(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(openai.Messages) != 2 || !reflect.DeepEqual(openai.Messages[1:], r.Messages) || openai.Tools != nil {
		t.Fatalf("openai: %s", openai.Messages)
	}
	var system struct{ Role, Content string }
	if err := json.Unmarshal(openai.Messages[0], &system); err != nil || system.Role != "system" || system.Content != r.System[0].Text {
		t.Fatalf("openai system message: %s", openai.Messages[0])
	}
}

func TestQuickstartRuns(t *testing.T) {
	var out bytes.Buffer
	if err := run(&out); err != nil {
		t.Fatal(err)
	}
	if s := out.String(); !strings.Contains(s, "kb:pricing  below_threshold") || !strings.Contains(s, "refused: evidence_required") {
		t.Fatalf("output:\n%s", s)
	}
}

func TestTheReadmeShowsWhatTheQuickstartPrints(t *testing.T) {
	var out bytes.Buffer
	if err := run(&out); err != nil {
		t.Fatal(err)
	}
	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	shown := regexp.MustCompile("(?s)go run ./examples/quickstart\n```\n\n```text\n(.*?)```").FindSubmatch(readme)
	if shown == nil || string(shown[1]) != out.String() {
		t.Fatalf("README's quickstart output differs from what the example prints:\n%s", out.String())
	}
}
