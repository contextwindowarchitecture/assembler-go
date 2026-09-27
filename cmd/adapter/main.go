// Command adapter bridges the Go package to the bootstrap conformance runner.
package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	assembler "github.com/contextwindowarchitecture/assembler-go"
)

func main() {
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	result, err := assembler.Assemble(raw, assembler.Options{})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		var rejected *assembler.SnapshotRejectedError
		var unsupported *assembler.UnsupportedComponentError
		switch {
		case errors.As(err, &rejected):
			os.Exit(2)
		case errors.As(err, &unsupported):
			os.Exit(3)
		default:
			os.Exit(1)
		}
	}
	var payload any
	if result.Payload != nil {
		payload = base64.StdEncoding.EncodeToString(result.Payload)
	}
	if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"payload": payload, "trace": result.Trace}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
