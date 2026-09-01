package readonlyaudit

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestOnlyIMAPXImportsGoIMAP(t *testing.T) {
	root := projectRoot(t)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return parseErr
		}
		for _, spec := range file.Imports {
			value := strings.Trim(spec.Path.Value, `"`)
			if strings.HasPrefix(value, "github.com/emersion/go-imap/v2") && !strings.HasPrefix(filepath.ToSlash(rel), "internal/imapx/") {
				t.Errorf("go-imap import escaped imapx boundary: %s", rel)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestIMAPXHasNoMutationCalls(t *testing.T) {
	root := filepath.Join(projectRoot(t), "internal", "imapx")
	banned := map[string]bool{"Store": true, "Move": true, "Copy": true, "Append": true, "Expunge": true, "Create": true, "Delete": true, "Rename": true, "Subscribe": true, "Unsubscribe": true}
	set := token.NewFileSet()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		filename := filepath.Join(root, entry.Name())
		file, err := parser.ParseFile(set, filename, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if ok && banned[selector.Sel.Name] {
				t.Errorf("forbidden IMAP mutation method %s in %s", selector.Sel.Name, filename)
			}
			return true
		})
	}
}

func TestProtocolUsesExamineAndPeek(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(projectRoot(t), "internal", "imapx", "client.go"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	if !strings.Contains(source, "ReadOnly: true") || !strings.Contains(source, "Peek: true") {
		t.Fatal("EXAMINE/PEEK readonly protocol guards are missing")
	}
}

func projectRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test file")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
