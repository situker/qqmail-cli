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

func TestGoIMAPMutationCallsAreConfinedToMutationBoundary(t *testing.T) {
	root := filepath.Join(projectRoot(t), "internal", "imapx")
	banned := map[string]bool{"Store": true, "Move": true, "Copy": true, "Append": true, "Expunge": true, "UIDExpunge": true, "UnselectAndExpunge": true, "Create": true, "Delete": true, "Rename": true, "Subscribe": true, "Unsubscribe": true}
	// Every expunge shape is banned everywhere, including mutate.go:
	// UnselectAndExpunge sends CLOSE, which RFC 3501 defines as an implicit
	// silent expunge. "Never expunge" is this project's flagship promise.
	alwaysBanned := map[string]bool{"Expunge": true, "UIDExpunge": true, "UnselectAndExpunge": true}
	set := token.NewFileSet()
	mutateFile := filepath.Join(root, "mutate.go")
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return walkErr
		}
		file, parseErr := parser.ParseFile(set, path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if ok && banned[selector.Sel.Name] {
				if path != mutateFile || alwaysBanned[selector.Sel.Name] {
					t.Errorf("go-imap mutation method %s escaped the reviewed boundary in %s", selector.Sel.Name, path)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMutationBoundaryCanOnlyBeCalledByPolicy(t *testing.T) {
	root := projectRoot(t)
	mutationMethods := map[string]bool{"SetSeen": true, "MoveUID": true, "CopyMarkDeletedUID": true}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if ok && mutationMethods[selector.Sel.Name] && !strings.HasPrefix(filepath.ToSlash(rel), "internal/policy/") {
				t.Errorf("mutation boundary method %s called outside policy: %s", selector.Sel.Name, rel)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
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
