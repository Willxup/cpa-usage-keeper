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

func TestMaskAccountNameAndIdentityDisplayName(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"alice@example.com", "al***ce@e***.com"},
		{"bob@outlook.com", "b***b@o***.com"},
		{"ab@liif.cc", "a***@l***.cc"},
		{"x@a.io", "*@a***.io"},
		{"codex-alice@gmail.com.json", "co***ce@g***.com"},
		{"my-account-name", "my***me"},
		{"xy", "***"},
		{"", ""},
	} {
		if got := helper.MaskAccountName(tc.input); got != tc.want {
			t.Fatalf("MaskAccountName(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}

	alias := "Codex Node 1"
	emailAlias := "secret@company.com"
	if got := helper.UsageIdentityMaskedDisplayName(entities.UsageIdentity{Name: "alice@example.com", Alias: &alias}); got != "Codex Node 1" {
		t.Fatalf("expected non-email alias preserved, got %q", got)
	}
	if got := helper.UsageIdentityMaskedDisplayName(entities.UsageIdentity{Name: "alice@example.com", Alias: &emailAlias}); got != "se***et@c***.com" {
		t.Fatalf("expected email alias masked, got %q", got)
	}
	if got := helper.UsageIdentityMaskedDisplayName(entities.UsageIdentity{Name: "alice@example.com"}); got != "al***ce@e***.com" {
		t.Fatalf("expected email name masked, got %q", got)
	}
}
