package tokenprocessor_test

import (
	"testing"

	"cpa-usage-keeper/internal/service/tokenprocessor"
)

func mustResolveExecutor(t *testing.T, executor string) tokenprocessor.HandlerResolution {
	t.Helper()
	resolution, err := tokenprocessor.ResolveExecutor(executor)
	if err != nil {
		t.Fatalf("ResolveExecutor(%q) returned error: %v", executor, err)
	}
	return resolution
}

func mustResolveIdentity(t *testing.T, executor, identity string) tokenprocessor.HandlerResolution {
	t.Helper()
	resolution, err := tokenprocessor.ResolveIdentity(executor, identity)
	if err != nil {
		t.Fatalf("ResolveIdentity(%q, %q) returned error: %v", executor, identity, err)
	}
	return resolution
}

func hasAction(result tokenprocessor.ProcessResult, code string) bool {
	for _, action := range result.Actions {
		if action.Code == code {
			return true
		}
	}
	return false
}

func hasViolation(result tokenprocessor.ProcessResult, code string) bool {
	for _, violation := range result.Violations {
		if violation.Code == code {
			return true
		}
	}
	return false
}
