package main

// The README's Go snippets, as compiled code. Each function here is copied into the README verbatim, doc comment
// included, and the tests hold the two copies together and run them.

import (
	"errors"
	"fmt"
	"unicode/utf8"

	assembler "github.com/contextwindowarchitecture/assembler-go"
)

// assembleRequest assembles one request from snapshot, the frozen JSON: items, route policy, placement profile,
// budget, tokenizer, renderer and clock.
func assembleRequest(snapshot []byte) error {
	result, err := assembler.Assemble(snapshot, assembler.Options{})
	var rejected *assembler.SnapshotRejectedError
	switch {
	case errors.As(err, &rejected):
		return err // the snapshot is invalid: no payload and no trace; rejected.Problem says why
	case err != nil:
		return err // e.g. *assembler.UnsupportedComponentError for a tokenizer or renderer this package lacks
	case result.Payload == nil:
		fmt.Println("refused:", result.Trace["refused"].(map[string]any)["reason"])
	default:
		// result.Payload is the request to send: {"messages": [...], "system": [...], "tools": [...]}
	}
	for _, row := range result.Trace["excluded"].([]any) {
		fmt.Println(row.(map[string]any)["item_id"], row.(map[string]any)["reason"]) // kb:pricing below_threshold
	}
	return nil
}

// assembleWithOwnTokenizer counts one token per character, for a snapshot whose "tokenizer" is "my-chars/v1".
func assembleWithOwnTokenizer(snapshot []byte) (assembler.Result, error) {
	chars := func(text string) int { return utf8.RuneCountInString(text) }
	return assembler.Assemble(snapshot, assembler.Options{
		Tokenizers: map[string]assembler.Tokenizer{"my-chars/v1": chars},
	})
}
