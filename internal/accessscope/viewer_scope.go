// Package accessscope 提供 API Key Viewer 请求的服务端访问范围。
package accessscope

import "context"

// ViewerScope 是已解析的只读访问范围。APIGroupKey 仅在后端查询中使用，绝不能返回给浏览器。
type ViewerScope struct {
	CPAAPIKeyID   int64
	APIGroupKey   string
	AuthFileNames []string
	AuthIndexes   []string
}

type viewerScopeContextKey struct{}

// WithViewerScope 将已验证的 Viewer 范围绑定到请求上下文。
func WithViewerScope(ctx context.Context, scope ViewerScope) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, viewerScopeContextKey{}, scope)
}

// ViewerScopeFromContext 返回当前请求的 Viewer 范围；管理员请求不会携带该值。
func ViewerScopeFromContext(ctx context.Context) (ViewerScope, bool) {
	if ctx == nil {
		return ViewerScope{}, false
	}
	scope, ok := ctx.Value(viewerScopeContextKey{}).(ViewerScope)
	return scope, ok
}
