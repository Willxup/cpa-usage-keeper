package tokenprocessor_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"cpa-usage-keeper/internal/service/tokenprocessor"
)

func TestEachCPAExecutorHasAnIndependentDefinitionFile(t *testing.T) {
	// 独立文件是 CPA 升级后的维护边界；相同 handler 可以复用，但 executor definition 不能重新塞回大 switch。
	files := []string{
		"claude_executor.go",
		"gemini_executor.go",
		"gemini_vertex_executor.go",
		"gemini_cli_executor.go",
		"ai_studio_executor.go",
		"antigravity_executor.go",
		"codex_executor.go",
		"codex_websockets_executor.go",
		"codex_auto_executor.go",
		"xai_executor.go",
		"xai_websockets_executor.go",
		"xai_auto_executor.go",
		"kimi_executor.go",
		"openai_compat_executor.go",
	}

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate architecture test")
	}
	packageDir := filepath.Dir(filepath.Dir(currentFile))
	for _, name := range files {
		path := filepath.Join(packageDir, name)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected independent executor definition file %q: %v", name, err)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read executor definition file %q: %v", name, err)
		}
		// 每个独立文件都必须留下 CPA 升级维护说明和显式 handler 绑定，不能只靠文件名表达合同。
		if !strings.Contains(string(content), "维护说明") || !strings.Contains(string(content), "handlerID:") {
			t.Fatalf("executor file %q must document CPA maintenance and handler binding", name)
		}
	}
}

func TestHandlerResolutionIsAnOpaqueResolverResult(t *testing.T) {
	// 接口包含包内私有标记方法，包外调用方只能接收 resolver 结果，不能伪造 parser_contract。
	typeOfResolution := reflect.TypeOf((*tokenprocessor.HandlerResolution)(nil)).Elem()
	if typeOfResolution.Kind() != reflect.Interface {
		t.Fatalf("HandlerResolution must be an opaque interface, got %s", typeOfResolution.Kind())
	}

	resolution, err := tokenprocessor.ResolveExecutor("CodexExecutor")
	if err != nil {
		t.Fatalf("ResolveExecutor returned error: %v", err)
	}
	if resolution == nil {
		t.Fatal("resolver must return a non-nil opaque resolution")
	}
}

func TestTokenProcessorKeepsThePureTokenOnlyPackageBoundary(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate architecture test")
	}
	packageDir := filepath.Dir(filepath.Dir(currentFile))
	files, err := filepath.Glob(filepath.Join(packageDir, "*.go"))
	if err != nil {
		t.Fatalf("list tokenprocessor source files: %v", err)
	}
	for _, path := range files {
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, imported := range parsed.Imports {
			importPath, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				t.Fatalf("unquote import %s in %s: %v", imported.Path.Value, path, err)
			}
			// Token 纯计算包不得依赖数据库、日志、repository DTO 或完整 UsageEvent 实体。
			if strings.Contains(importPath, "gorm") || strings.Contains(importPath, "logrus") || strings.Contains(importPath, "/repository") || strings.Contains(importPath, "/entities") {
				t.Fatalf("tokenprocessor source %s imports forbidden boundary %q", filepath.Base(path), importPath)
			}
		}
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if ok && function.Name.Name == "init" {
				t.Fatalf("tokenprocessor source %s must not use runtime init registration", filepath.Base(path))
			}
		}
	}
}

func TestIssue272ImplementationStaysInsideOpenAICompatibilityExecutor(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate architecture test")
	}
	packageDir := filepath.Dir(filepath.Dir(currentFile))
	files, err := filepath.Glob(filepath.Join(packageDir, "*.go"))
	if err != nil {
		t.Fatalf("list tokenprocessor source files: %v", err)
	}
	for _, path := range files {
		if filepath.Base(path) == "openai_compat_executor.go" {
			continue
		}
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		// 公共 processor/types 不得按 #272 action code 分支；专属规则变化只修改 OpenAI Compatibility 文件。
		if strings.Contains(string(content), "apply_issue272_reasoning_fold") || strings.Contains(string(content), "issue272SeparatedReasoningRule") {
			t.Fatalf("issue #272 implementation leaked into %s", filepath.Base(path))
		}
	}
}
