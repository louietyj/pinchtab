package config

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestFileConfigFromRuntimeSharesNothingWithTheRuntime(t *testing.T) {
	cfg := populatedRuntimeConfig(t)
	before := *cfg
	beforePatterns := append([]string(nil), cfg.IDPI.CustomPatterns...)
	beforeAllowHosts := append([]string(nil), cfg.AttachAllowHosts...)

	fc := FileConfigFromRuntime(cfg)
	*fc.Server.TrustProxyHeaders = !*fc.Server.TrustProxyHeaders
	fc.Security.IDPI.CustomPatterns[0] = "mutated"
	fc.Security.IDPI.Enabled = !fc.Security.IDPI.Enabled
	fc.Security.Attach.AllowHosts[0] = "mutated"
	*fc.Security.AllowEvaluate = !*fc.Security.AllowEvaluate
	*fc.MultiInstance.Restart.MaxRestarts++
	fc.Browser.ExtensionPaths[0] = "mutated"

	if cfg.TrustProxyHeaders != before.TrustProxyHeaders {
		t.Fatalf("Server.TrustProxyHeaders aliases the runtime field")
	}
	if cfg.IDPI.CustomPatterns[0] != beforePatterns[0] || cfg.IDPI.Enabled != before.IDPI.Enabled {
		t.Fatalf("Security.IDPI shares memory with the runtime: %+v", cfg.IDPI)
	}
	if cfg.AttachAllowHosts[0] != beforeAllowHosts[0] || cfg.AllowEvaluate != before.AllowEvaluate || cfg.RestartMaxRestarts != before.RestartMaxRestarts || cfg.ExtensionPaths[0] == "mutated" {
		t.Fatalf("a FileConfig write reached the runtime: %+v", cfg)
	}
}

func TestFileConfigFromRuntimeDelegatesToPerSectionBuilders(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "config_file_marshal.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "FileConfigFromRuntime" {
			continue
		}
		lines := fset.Position(fn.End()).Line - fset.Position(fn.Pos()).Line
		if lines > 60 {
			t.Fatalf("FileConfigFromRuntime spans %d lines, want under 60 with the sections built by their own functions", lines)
		}
		var addressOfRuntime []string
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if u, ok := n.(*ast.UnaryExpr); ok && u.Op == token.AND {
				addressOfRuntime = append(addressOfRuntime, fset.Position(u.Pos()).String())
			}
			return true
		})
		for _, b := range []string{"serverConfigFromRuntime", "securityConfigFromRuntime", "instanceDefaultsFromRuntime"} {
			if !callsFunction(fn.Body, b) {
				t.Errorf("FileConfigFromRuntime does not delegate to %s", b)
			}
		}
		return
	}
	t.Fatal("FileConfigFromRuntime not found")
}

func TestSectionBuildersNeverTakeTheAddressOfARuntimeField(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "config_file_marshal.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Type.Params.NumFields() != 1 {
			continue
		}
		param := fn.Type.Params.List[0]
		star, ok := param.Type.(*ast.StarExpr)
		if !ok {
			continue
		}
		if ident, ok := star.X.(*ast.Ident); !ok || ident.Name != "RuntimeConfig" {
			continue
		}
		checked++
		cfgName := param.Names[0].Name
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			u, ok := n.(*ast.UnaryExpr)
			if !ok || u.Op != token.AND {
				return true
			}
			if sel, ok := u.X.(*ast.SelectorExpr); ok {
				if root, ok := sel.X.(*ast.Ident); ok && root.Name == cfgName {
					t.Errorf("%s: %s takes the address of a runtime field; copy the value through ptr instead", fset.Position(u.Pos()), fn.Name.Name)
				}
			}
			return true
		})
	}
	if checked < 10 {
		t.Fatalf("checked %d runtime-taking builders, want the section builders plus FileConfigFromRuntime", checked)
	}
}

func callsFunction(body *ast.BlockStmt, name string) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if id, ok := call.Fun.(*ast.Ident); ok && id.Name == name {
				found = true
			}
		}
		return true
	})
	return found
}
