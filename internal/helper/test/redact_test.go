package test

import (
	"testing"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/helper"
)

func TestRedactSensitiveValueUsesCanonicalFormat(t *testing.T) {
	for _, test := range []struct{ input, want string }{
		{"sk-BabcdefghijklmnopqrstuvwxyzmaWyTA", "sk-*********maWyTA"},
		{"short", "*********"},
		{"sk-123456", "*********"},
		{"", "unknown"},
		{"unknown", "unknown"},
	} {
		t.Run(test.input, func(t *testing.T) {
			if got := helper.RedactSensitiveValue(test.input); got != test.want {
				t.Fatalf("masked value = %q, want %q", got, test.want)
			}
		})
	}
}

func TestCPAAPIKeyDisplayNamePrefersAlias(t *testing.T) {
	row := entities.CPAAPIKey{APIKey: "sk-alpha123456", KeyAlias: "  Production  ", DisplayKey: "sk-B********************************Zejy"}

	if got := helper.CPAAPIKeyDisplayName(row); got != "Production" {
		t.Fatalf("expected alias label, got %q", got)
	}
}

func TestCPAAPIKeyDisplayNameFallsBackToMaskedRawKey(t *testing.T) {
	row := entities.CPAAPIKey{APIKey: "sk-alpha123456", DisplayKey: "sk-B********************************Zejy"}

	if got := helper.CPAAPIKeyDisplayName(row); got != "sk-*********123456" {
		t.Fatalf("expected canonical masked key fallback, got %q", got)
	}
}

func TestCPAAPIKeyMaskedDisplayKeyMasksRawKeyWithCanonicalFormat(t *testing.T) {
	row := entities.CPAAPIKey{APIKey: "sk-BabcdefghijklmnopqrstuvwxyzmaWyTA", DisplayKey: "sk-B********************************maWy"}

	if got := helper.CPAAPIKeyMaskedDisplayKey(row); got != "sk-*********maWyTA" {
		t.Fatalf("expected canonical display key, got %q", got)
	}
}

func TestCPAAPIKeyMaskedDisplayKeyFallsBackToStoredDisplayKeyWhenRawKeyIsMissing(t *testing.T) {
	row := entities.CPAAPIKey{DisplayKey: "sk-*********maWyTA"}

	if got := helper.CPAAPIKeyMaskedDisplayKey(row); got != "sk-*********maWyTA" {
		t.Fatalf("expected stored display key fallback, got %q", got)
	}
}
