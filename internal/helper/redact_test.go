package helper

import (
	"testing"

	"cpa-usage-keeper/internal/entities"
)

func TestRedactSensitiveValueUsesCanonicalFormat(t *testing.T) {
	if got := RedactSensitiveValue("sk-BabcdefghijklmnopqrstuvwxyzmaWyTA"); got != "sk-*********maWyTA" {
		t.Fatalf("expected canonical masked key, got %q", got)
	}
	if got := RedactSensitiveValue("short"); got != "*********" {
		t.Fatalf("expected short key to use fixed mask, got %q", got)
	}
	if got := RedactSensitiveValue("sk-123456"); got != "*********" {
		t.Fatalf("expected boundary-length key to be fully masked, got %q", got)
	}
	if got := RedactSensitiveValue(""); got != "unknown" {
		t.Fatalf("expected empty key to stay compatible with public fallback, got %q", got)
	}
	if got := RedactSensitiveValue("unknown"); got != "unknown" {
		t.Fatalf("expected unknown key to remain unknown, got %q", got)
	}
}

func TestCPAAPIKeyDisplayNamePrefersAlias(t *testing.T) {
	row := entities.CPAAPIKey{APIKey: "sk-alpha123456", KeyAlias: "  Production  ", DisplayKey: "sk-B********************************Zejy"}

	if got := CPAAPIKeyDisplayName(row); got != "Production" {
		t.Fatalf("expected alias label, got %q", got)
	}
}

func TestCPAAPIKeyDisplayNameFallsBackToMaskedRawKey(t *testing.T) {
	row := entities.CPAAPIKey{APIKey: "sk-alpha123456", DisplayKey: "sk-B********************************Zejy"}

	if got := CPAAPIKeyDisplayName(row); got != "sk-*********123456" {
		t.Fatalf("expected canonical masked key fallback, got %q", got)
	}
}

func TestCPAAPIKeyMaskedDisplayKeyMasksRawKeyWithCanonicalFormat(t *testing.T) {
	row := entities.CPAAPIKey{APIKey: "sk-BabcdefghijklmnopqrstuvwxyzmaWyTA", DisplayKey: "sk-B********************************maWy"}

	if got := CPAAPIKeyMaskedDisplayKey(row); got != "sk-*********maWyTA" {
		t.Fatalf("expected canonical display key, got %q", got)
	}
}

func TestCPAAPIKeyMaskedDisplayKeyFallsBackToStoredDisplayKeyWhenRawKeyIsMissing(t *testing.T) {
	row := entities.CPAAPIKey{DisplayKey: "sk-*********maWyTA"}

	if got := CPAAPIKeyMaskedDisplayKey(row); got != "sk-*********maWyTA" {
		t.Fatalf("expected stored display key fallback, got %q", got)
	}
}

func TestCPAAPIKeyMaskedDisplayKeyUsesPluginPreview(t *testing.T) {
	row := entities.CPAAPIKey{APIKey: "marketplace1", DisplayKey: "cpa_mar...place", Source: entities.CPAAPIKeySourceCPAKeyPolicy}
	if got := CPAAPIKeyMaskedDisplayKey(row); got != "cpa_mar...place" {
		t.Fatalf("expected plugin key preview, got %q", got)
	}
}
