package assembler

// Tokenizer counts the text a renderer emits.
type Tokenizer func(string) int

// Options supplies model tokenizers for one assembly call.
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
