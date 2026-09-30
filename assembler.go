package assembler

// Tokenizer counts the text a renderer emits.
type Tokenizer func(string) int

// Options supplies the application's own tokenizers for one assembly call, keyed by id. A
// tokenizer under a published tokenizer's id stops the call before assembly, with no result (R-16).
// It has no field for a renderer: only the published fixture-xml/v1 and cwa-messages/v1 render, so
// the stop R-16 also puts on an application renderer under a published id cannot arise.
type Options struct {
	Tokenizers map[string]Tokenizer
}

// Result holds the rendered bytes, or a nil payload on refusal, and its trace.
type Result struct {
	Payload []byte
	Trace   map[string]any
}

// SnapshotRejectedError reports an invalid snapshot before assembly starts.
type SnapshotRejectedError struct{ Problem string }

func (e *SnapshotRejectedError) Error() string { return e.Problem }

// UnsupportedComponentError reports an unprovided tokenizer or renderer.
type UnsupportedComponentError struct{ Component, ID string }

func (e *UnsupportedComponentError) Error() string {
	return e.Component + " " + e.ID + " is not provided"
}

// Assemble consumes a frozen JSON snapshot without external reads.
func Assemble(raw []byte, options Options) (Result, error) {
	snapshot, err := validateSnapshot(raw)
	if err != nil {
		return Result{}, err
	}
	return assembleBasic(snapshot, options)
}
