package test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestUsageEventsCleanupRemainsBehindStorageCleanupOptions(t *testing.T) {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "../db.go", nil, 0)
	if err != nil {
		t.Fatalf("parse repository db.go: %v", err)
	}

	var hasPrivateCleanup bool
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if function.Name.Name == "CleanupUsageEvents" {
			t.Fatal("usage_events cleanup should stay behind CleanupStorageOptions instead of exposing a direct exported cleanup entrypoint")
		}
		if function.Name.Name == "cleanupUsageEvents" {
			hasPrivateCleanup = true
		}
	}
	if !hasPrivateCleanup {
		t.Fatal("expected private cleanupUsageEvents helper to be present")
	}
}
