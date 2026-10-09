package test

import (
	"context"
	_ "cpa-usage-keeper/internal/service"
	"errors"
	"strings"
	"testing"
	_ "unsafe"
)

func TestJoinAuthFilesManagementErrorDedupesContextCancellation(t *testing.T) {
	var joined error

	joined = joinAuthFilesManagementError(joined, context.Canceled)
	joined = joinAuthFilesManagementError(joined, context.Canceled)

	if !errors.Is(joined, context.Canceled) {
		t.Fatalf("expected joined error to contain context cancellation, got %v", joined)
	}
	if strings.Count(joined.Error(), context.Canceled.Error()) != 1 {
		t.Fatalf("expected context cancellation to appear once, got %q", joined.Error())
	}
}

func TestJoinAuthFilesManagementErrorReturnsFirstErrorDirectly(t *testing.T) {
	first := errors.New("first failure")

	if joined := joinAuthFilesManagementError(nil, first); joined != first {
		t.Fatalf("expected first error to be returned directly, got %T %[1]v", joined)
	}
}

// 直接保留错误链去重与 error 对象身份的白盒覆盖。
//
//go:linkname joinAuthFilesManagementError cpa-usage-keeper/internal/service.joinAuthFilesManagementError
func joinAuthFilesManagementError(joined error, err error) error
