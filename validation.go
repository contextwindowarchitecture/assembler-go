package assembler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/contextwindowarchitecture/assembler-go/internal/generated"
	"github.com/dlclark/regexp2"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

type ecmaRegexp regexp2.Regexp

func (re *ecmaRegexp) MatchString(s string) bool {
	matched, err := (*regexp2.Regexp)(re).MatchString(s)
	return err == nil && matched
}

func (re *ecmaRegexp) String() string { return (*regexp2.Regexp)(re).String() }

func compileECMA(pattern string) (jsonschema.Regexp, error) {
	re, err := regexp2.Compile(pattern, regexp2.ECMAScript)
	if err != nil {
		return nil, err
	}
	return (*ecmaRegexp)(re), nil
}

var (
	schemaOnce          sync.Once
	schema              *jsonschema.Schema
	traceSchemaCompiled *jsonschema.Schema
	itemSchemaCompiled  *jsonschema.Schema
	schemaErr           error
)

func snapshotSchema() (*jsonschema.Schema, error) {
	schemaOnce.Do(func() {
		compiler := jsonschema.NewCompiler()
		compiler.UseRegexpEngine(compileECMA)
		compiler.AssertFormat()
		schemaErr = fs.WalkDir(generated.Files, "schema", func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			body, err := generated.Files.ReadFile(path)
			if err != nil {
				return err
			}
			var document any
			if err := json.Unmarshal(body, &document); err != nil {
				return err
			}
			id := document.(map[string]any)["$id"].(string)
			return compiler.AddResource(id, document)
		})
		if schemaErr == nil {
			schema, schemaErr = compiler.Compile("https://contextwindowarchitecture.io/schema/snapshot.schema.json")
		}
		if schemaErr == nil {
			traceSchemaCompiled, schemaErr = compiler.Compile("https://contextwindowarchitecture.io/schema/trace.schema.json")
		}
		if schemaErr == nil {
			itemSchemaCompiled, schemaErr = compiler.Compile("https://contextwindowarchitecture.io/schema/context_item.schema.json")
		}
	})
	return schema, schemaErr
}

func traceSchema() (*jsonschema.Schema, error) {
	_, err := snapshotSchema()
	return traceSchemaCompiled, err
}

func itemSchema() (*jsonschema.Schema, error) {
	_, err := snapshotSchema()
	return itemSchemaCompiled, err
}

func reject(format string, args ...any) error {
	return &SnapshotRejectedError{Problem: fmt.Sprintf(format, args...)}
}

func validateSnapshot(raw []byte) (map[string]any, error) {
	if !utf8.Valid(raw) {
		return nil, reject("snapshot is not valid UTF-8")
	}
	if err := checkSurrogateEscapes(raw); err != nil {
		return nil, reject("%v", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, reject("invalid JSON: %v", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return nil, reject("snapshot has trailing JSON")
	} else if err != io.EOF {
		return nil, reject("invalid trailing data: %v", err)
	}
	compiled, err := snapshotSchema()
	if err != nil {
		return nil, fmt.Errorf("compile vendored snapshot schema: %w", err)
	}
	if err := compiled.Validate(value); err != nil {
		return nil, reject("snapshot schema: %v", err)
	}
	snapshot := value.(map[string]any)
	if err := checkSnapshotReferences(snapshot); err != nil {
		return nil, err
	}
	if err := checkProfile(snapshot); err != nil {
		return nil, err
	}
	return snapshot, nil
}

// encoding/json replaces lone surrogates; scan escapes before decoding loses them.
func checkSurrogateEscapes(raw []byte) error {
	inString := false
	for i := 0; i < len(raw); i++ {
		switch raw[i] {
		case '"':
			inString = !inString
		case '\\':
			if !inString || i+1 >= len(raw) {
				continue
			}
			if raw[i+1] != 'u' {
				i++
				continue
			}
			if i+6 > len(raw) {
				return fmt.Errorf("incomplete Unicode escape")
			}
			code, err := strconv.ParseUint(string(raw[i+2:i+6]), 16, 16)
			if err != nil {
				return fmt.Errorf("invalid Unicode escape")
			}
			if code >= 0xdc00 && code <= 0xdfff {
				return fmt.Errorf("unpaired low surrogate")
			}
			if code >= 0xd800 && code <= 0xdbff {
				if i+12 > len(raw) || raw[i+6] != '\\' || raw[i+7] != 'u' {
					return fmt.Errorf("unpaired high surrogate")
				}
				low, err := strconv.ParseUint(string(raw[i+8:i+12]), 16, 16)
				if err != nil || low < 0xdc00 || low > 0xdfff {
					return fmt.Errorf("unpaired high surrogate")
				}
				i += 12 - 1
				continue
			}
			i += 6 - 1
		}
	}
	return nil
}

func asObject(value any) map[string]any { return value.(map[string]any) }
func asArray(value any) []any           { return value.([]any) }
func asString(value any) string         { return value.(string) }

func checkSnapshotReferences(snapshot map[string]any) error {
	batchIDs := map[string]bool{}
	itemIDs := map[string]bool{}
	for _, rawBatch := range asArray(snapshot["batches"]) {
		batch := asObject(rawBatch)
		producer := asObject(batch["producer"])
		id := asString(producer["id"])
		if batchIDs[id] {
			return reject("producer %q heads two batches", id)
		}
		batchIDs[id] = true
		candidateIDs := map[string]bool{}
		for _, rawItem := range asArray(batch["items"]) {
			if itemID, ok := asObject(rawItem)["id"].(string); ok {
				candidateIDs[itemID] = true
				itemIDs[itemID] = true
			}
		}
		for _, rawExcluded := range asArray(batch["excluded"]) {
			excluded := asObject(rawExcluded)
			itemIDs[asString(excluded["item_id"])] = true
			for _, field := range []string{"duplicate_of", "superseded_by"} {
				if target, ok := excluded[field].(string); ok && !candidateIDs[target] {
					return reject("producer exclusion %s %q names no candidate in its batch", field, target)
				}
			}
		}
	}
	groups := map[string]bool{}
	groupedItems := map[string]bool{}
	facts, _ := asObject(snapshot["route_policy"])["facts"].(map[string]any)
	for _, rawGroup := range asArray(snapshot["conflicts"]) {
		group := asObject(rawGroup)
		id := asString(group["id"])
		if groups[id] {
			return reject("conflict group id %q repeats", id)
		}
		groups[id] = true
		if asString(group["kind"]) == "fact" {
			if _, ok := facts[asString(group["fact"])]; !ok {
				return reject("conflict group %q names unknown fact", id)
			}
		}
		for _, rawID := range asArray(group["items"]) {
			itemID := asString(rawID)
			if !itemIDs[itemID] {
				return reject("conflict group %q names unknown item %q", id, itemID)
			}
			if groupedItems[itemID] {
				return reject("item %q belongs to two conflict groups", itemID)
			}
			groupedItems[itemID] = true
		}
	}
	return nil
}

var xmlTag = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`)

func checkProfile(snapshot map[string]any) error {
	profile := asObject(snapshot["profile"])
	policy := asObject(snapshot["route_policy"])
	if profile["route"] != policy["route"] || profile["route_policy_version"] != policy["version"] {
		return reject("profile route or policy version differs from route policy")
	}
	placed := map[string]bool{}
	placements := asArray(profile["placement"])
	for _, rawPlacement := range placements {
		placed[asString(asObject(rawPlacement)["slot"])] = true
	}
	for _, slot := range []string{"governance.instructions", "interaction.query"} {
		if !placed[slot] {
			return reject("profile omits required slot %s", slot)
		}
	}
	if policy["parser"] == true && !placed["governance.output_contract"] {
		return reject("parser profile omits governance.output_contract")
	}
	renderer := asString(snapshot["renderer"])
	if renderer != "fixture-xml/v1" && renderer != "cwa-messages/v1" {
		return nil
	}
	seenXML := false
	for _, rawPlacement := range placements {
		placement := asObject(rawPlacement)
		slot, wrap := asString(placement["slot"]), asString(placement["wrap"])
		if strings.HasPrefix(wrap, "xml:") {
			if !xmlTag.MatchString(strings.TrimPrefix(wrap, "xml:")) {
				return reject("profile has invalid XML tag in %q", wrap)
			}
			seenXML = true
			continue
		}
		if renderer == "fixture-xml/v1" {
			return reject("fixture-xml cannot realize wrap %q", wrap)
		}
		switch wrap {
		case "system":
			if !strings.HasPrefix(slot, "governance.") || seenXML {
				return reject("system placement is invalid for %s or follows XML", slot)
			}
		case "tools":
			if slot != "governance.capabilities" {
				return reject("tools placement is invalid for %s", slot)
			}
		default:
			return reject("renderer cannot realize wrap %q", wrap)
		}
	}
	return nil
}
