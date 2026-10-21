// Command quickstart assembles one support-chat request, then watches the assembler refuse one.
//
//	go run ./examples/quickstart
//
// A snapshot is everything assembly may look at, frozen: the items producers proposed, the route's policy, the
// placement profile, the budget, the tokenizer, the renderer and the clock. Assemble turns it into a payload and a
// trace, or a refusal and a trace, and reads nothing else (R-23). The README's Quickstart mirrors this file.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	assembler "github.com/contextwindowarchitecture/assembler-go"
)

const now = "2026-10-21T12:00:00Z"

type doc struct {
	ID, Body  string
	Relevance float64
}

// What a retriever returned for the question, with its scores. The route below admits only chunks scoring 0.5 or more.
var docs = []doc{
	{"kb:refunds", "Pro plans can be refunded in full within 30 days of purchase.", 0.82},
	{"kb:pricing", "The Pro plan costs $20 a month, billed annually.", 0.31},
}

type object = map[string]any

// Which producers may write which slots, and the rules for each slot. Versioned, so a trace names the policy it used.
var routePolicy = object{
	"route":   "support-chat",
	"version": "support-chat/v1",
	"producers": object{
		"policy-registry": object{"kind": "policy", "slots": []string{"governance.instructions"}},
		"docs-search":     object{"kind": "retrieval", "slots": []string{"evidence.knowledge"}},
		"conversation":    object{"kind": "interaction", "slots": []string{"interaction.query"}},
	},
	"slots":             object{"evidence.knowledge": object{"min_relevance": 0.5}},
	"requires_evidence": true, // refuse rather than let the model answer from nothing (R-12)
}

// Where each slot lands in the request. Instructions go to the system channel; the rest is escaped material in the
// user message, so a retrieved chunk can never speak as the system (R-7, R-10).
var profile = object{
	"spec":                 "cwa/draft",
	"id":                   "support-chat-default",
	"version":              1,
	"route":                "support-chat",
	"model_family":         nil,
	"route_policy_version": "support-chat/v1",
	"placement": []object{
		{"slot": "governance.instructions", "wrap": "system"},
		{"slot": "evidence.knowledge", "wrap": "xml:evidence"},
		{"slot": "interaction.query", "wrap": "xml:query"},
	},
	"evaluation": object{"status": "unevaluated", "suite": nil, "date": nil, "result": nil, "artifact": nil},
}

// item has the eight fields every item carries; the slot's defaults fill in the rest (R-2, R-3).
func item(id, slot, authority, trust, body string) object {
	return object{"id": id, "slot": slot, "source": "src:" + id, "source_version": "1", "authority": authority,
		"trust": trust, "freshness": now, "body": body}
}

// buildSnapshot freezes one request. Everything that can change the payload is in here; nothing is read later.
func buildSnapshot(found []doc) []byte {
	chunks := []object{}
	for _, d := range found {
		chunk := item(d.ID, "evidence.knowledge", "reference_only", "unverified", d.Body)
		chunk["relevance"] = d.Relevance
		chunks = append(chunks, chunk)
	}
	snapshot := object{
		"assembly_time": now,
		"scope":         object{"tenant": "acme", "user": "u_42", "session": "s_1", "task": "support"},
		"budget":        object{"input": 8000, "reserved_output": 1024, "margin_percent": 10}, // headroom for an estimating tokenizer
		"profile":       profile,
		"route_policy":  routePolicy,
		"tokenizer":     "estimate-utf8/v1", // pass your model's own tokenizer for exact counts (see Extending)
		"renderer":      "cwa-messages/v1",  // {system, tools, messages}, ready to map onto a provider's request
		"batches": []object{
			{"producer": object{"id": "policy-registry", "kind": "policy"}, "excluded": []any{}, "items": []object{
				item("policy:tone", "governance.instructions", "governing", "verified",
					"Answer from the evidence only, and say so when it does not cover the question."),
			}},
			{"producer": object{"id": "docs-search", "kind": "retrieval"}, "excluded": []any{}, "items": chunks},
			{"producer": object{"id": "conversation", "kind": "interaction"}, "excluded": []any{}, "items": []object{
				item("turn:1", "interaction.query", "user", "unverified", "Can I get a refund on my Pro plan?"),
			}},
		},
		"conflicts": []any{},
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		panic(err) // the literals above always marshal
	}
	return raw
}

// request is what cwa-messages/v1 renders.
type request struct {
	System   []entry           `json:"system"`
	Tools    []entry           `json:"tools"`
	Messages []json.RawMessage `json:"messages"`
}

type entry struct {
	ID       string `json:"id"`
	Text     string `json:"text"`
	Conflict string `json:"conflict,omitempty"`
}

// anthropicRequest is the body of an Anthropic Messages API call, less model and max_tokens.
type anthropicRequest struct {
	System   string            `json:"system"`
	Messages []json.RawMessage `json:"messages"`
	Tools    []json.RawMessage `json:"tools,omitempty"`
}

// openAIRequest is the body of an OpenAI Chat Completions call, less model.
type openAIRequest struct {
	Messages []json.RawMessage `json:"messages"`
	Tools    []json.RawMessage `json:"tools,omitempty"`
}

func toAnthropic(r request) anthropicRequest {
	out := anthropicRequest{Messages: r.Messages, Tools: tools(r)}
	for i, e := range r.System {
		if i > 0 {
			out.System += "\n\n"
		}
		out.System += e.Text
	}
	return out
}

func toOpenAI(r request) (openAIRequest, error) {
	out := openAIRequest{Tools: tools(r)}
	for _, e := range r.System {
		message, err := json.Marshal(struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{"system", e.Text})
		if err != nil {
			return openAIRequest{}, err
		}
		out.Messages = append(out.Messages, message)
	}
	out.Messages = append(out.Messages, r.Messages...)
	return out, nil
}

// tools passes each tool through: its text is the body its capability producer wrote, in the provider's tool format.
func tools(r request) []json.RawMessage {
	var out []json.RawMessage
	for _, t := range r.Tools {
		out = append(out, json.RawMessage(t.Text))
	}
	return out
}

func run(w io.Writer) error {
	result, err := assembler.Assemble(buildSnapshot(docs), assembler.Options{})
	if err != nil {
		return err
	}
	var r request
	if err := json.Unmarshal(result.Payload, &r); err != nil {
		return err
	}
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false) // keep <evidence> readable
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(toAnthropic(r)); err != nil {
		return err
	}
	fmt.Fprintf(w, "\n%v tokens; left out:\n", result.Trace["result"].(map[string]any)["input_tokens"])
	for _, row := range result.Trace["excluded"].([]any) {
		row := row.(map[string]any)
		fmt.Fprintf(w, "  %v  %v\n", row["item_id"], row["reason"])
	}

	// The retriever found nothing: no payload, so no model call, and the trace says what to do next.
	refused, err := assembler.Assemble(buildSnapshot(nil), assembler.Options{})
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "\nrefused: %v, recovery: %v\n", refused.Trace["refused"].(map[string]any)["reason"],
		refused.Trace["recovery"].(map[string]any)["action"])
	return nil
}

func main() {
	if err := run(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
