// Package assembler implements the Context Window Architecture (CWA) draft assembler.
//
// Assemble turns a frozen snapshot, the JSON document schema/snapshot.schema.json describes, into the payload to
// send to a model and a trace of every decision, or into a refusal and its trace. It admits candidate items,
// resolves declared conflicts, supersedes stale observations, removes exact duplicates, caps sources, fits the
// budget and renders, as the vendored conformance/README.md defines each stage. It reads nothing outside the
// snapshot, so the same snapshot always yields the same payload and trace.
//
// The built-in tokenizers are fixture-whitespace/v1 and estimate-utf8/v1; the built-in renderers are
// fixture-xml/v1, cwa-messages/v1 and the optional cwa-message-blocks/v1. Options adds the application's own
// tokenizers; there is no way to add a renderer.
package assembler
