package extend_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// Built-in console copy declared with extend.T is extracted into the web
// catalogue by a literal scan (web/scripts/i18n-lib.mjs). A non-literal
// argument would silently drop out of translation, so every extend.T call in
// the repository must take exactly one string literal. Inside package extend,
// bare T calls are checked the same way.
func TestExtendTCarriesOnlyLiterals(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	internal := filepath.Join(root, "internal")
	fset := token.NewFileSet()
	var bad []string
	var calls int
	err := filepath.WalkDir(internal, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		// Test files never reach the catalogue (the extractor skips them
		// too), so test helpers may build labels dynamically.
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		aliases := map[string]bool{}
		for _, imp := range f.Imports {
			p, err := strconv.Unquote(imp.Path.Value)
			if err != nil || p != "github.com/braedonsaunders/ether/internal/extend" {
				continue
			}
			if imp.Name != nil {
				aliases[imp.Name.Name] = true
			} else {
				aliases["extend"] = true
			}
		}
		inExtendPkg := f.Name.Name == "extend"
		if len(aliases) == 0 && !inExtendPkg {
			return nil
		}
		ast.Inspect(f, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			isT := false
			switch fun := call.Fun.(type) {
			case *ast.SelectorExpr:
				if fun.Sel.Name == "T" {
					if id, ok := fun.X.(*ast.Ident); ok && aliases[id.Name] {
						isT = true
					}
				}
			case *ast.Ident:
				isT = inExtendPkg && fun.Name == "T"
			}
			if !isT {
				return true
			}
			// The declaration of T itself is a FuncDecl, not a call.
			calls++
			pos := fset.Position(call.Pos())
			rel, err := filepath.Rel(root, pos.Filename)
			if err != nil {
				rel = pos.Filename
			}
			here := rel + ":" + strconv.Itoa(pos.Line)
			if len(call.Args) != 1 {
				bad = append(bad, here+": extend.T takes exactly one argument")
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				bad = append(bad, here+": extend.T argument must be a string literal (it is extracted into the console catalogue)")
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if calls == 0 {
		t.Fatal("no extend.T calls found; the catalogued built-in copy (internal/config/schema) must declare some")
	}
	if len(bad) > 0 {
		t.Fatalf("non-literal extend.T arguments:\n%s", strings.Join(bad, "\n"))
	}
}
