package main

import (
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"testing"

	assembler "github.com/contextwindowarchitecture/assembler-go"
)

// declarations maps each function in a Go source to its text, doc comment included, and each import path to its name.
func declarations(t *testing.T, name, src string) (map[string]string, map[string]string) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, src, parser.ParseComments)
	if err != nil {
		t.Fatalf("%s is not a complete Go declaration: %v", name, err)
	}
	funcs, imports := map[string]string{}, map[string]string{}
	for _, spec := range file.Imports {
		imports[spec.Path.Value] = ""
		if spec.Name != nil {
			imports[spec.Path.Value] = spec.Name.Name
		}
	}
	for _, decl := range file.Decls {
		switch decl := decl.(type) {
		case *ast.FuncDecl:
			start := decl.Pos()
			if decl.Doc != nil {
				start = decl.Doc.Pos()
			}
			funcs[decl.Name.Name] = src[fset.Position(start).Offset:fset.Position(decl.End()).Offset]
		case *ast.GenDecl:
			if decl.Tok != token.IMPORT {
				t.Fatalf("%s declares a %s; only functions are pinned", name, decl.Tok)
			}
		}
	}
	return funcs, imports
}

// Every Go block in the README is a function from readme.go, so the code a caller copies compiles and runs.
func TestTheReadmeSnippetsAreCompiledCode(t *testing.T) {
	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("readme.go")
	if err != nil {
		t.Fatal(err)
	}
	compiled, compiledImports := declarations(t, "readme.go", string(source))
	blocks := regexp.MustCompile("(?s)```go\n(.*?)```").FindAllSubmatch(readme, -1)
	if len(blocks) == 0 {
		t.Fatal("the README has no Go blocks")
	}
	for i, block := range blocks {
		name := "README Go block " + string(rune('1'+i))
		funcs, imports := declarations(t, name, "package main\n\n"+string(block[1]))
		if len(funcs) == 0 {
			t.Errorf("%s declares no function", name)
		}
		for path, alias := range imports {
			if got, ok := compiledImports[path]; !ok || got != alias {
				t.Errorf("%s imports %s that readme.go does not import the same way", name, path)
			}
		}
		for fn, text := range funcs {
			if compiled[fn] != text {
				t.Errorf("%s: %s differs from readme.go:\n%s", name, fn, compiled[fn])
			}
		}
	}
}

// withTokenizer names another tokenizer in a snapshot.
func withTokenizer(t *testing.T, raw []byte, id string) []byte {
	t.Helper()
	var snapshot map[string]any
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatal(err)
	}
	snapshot["tokenizer"] = id
	out, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestTheReadmeSnippetsRun(t *testing.T) {
	if err := assembleRequest(buildSnapshot(docs)); err != nil {
		t.Fatalf("assembled: %v", err)
	}
	if err := assembleRequest(buildSnapshot(nil)); err != nil {
		t.Fatalf("refused: %v", err)
	}
	var rejected *assembler.SnapshotRejectedError
	if err := assembleRequest([]byte(`{}`)); !errors.As(err, &rejected) {
		t.Fatalf("rejected: %v", err)
	}
	var unsupported *assembler.UnsupportedComponentError
	if err := assembleRequest(withTokenizer(t, buildSnapshot(docs), "my-chars/v1")); !errors.As(err, &unsupported) {
		t.Fatalf("unsupported: %v", err)
	}
	result, err := assembleWithOwnTokenizer(withTokenizer(t, buildSnapshot(docs), "my-chars/v1"))
	if err != nil {
		t.Fatal(err)
	}
	if id := result.Trace["context"].(map[string]any)["tokenizer"]; id != "my-chars/v1" {
		t.Fatalf("tokenizer: %v", id)
	}
}
