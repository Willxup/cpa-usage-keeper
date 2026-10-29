package test

import (
	"encoding/json"
	"strings"
	"testing"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/helper"
)

func TestPluginAPIKeyIdentityEncodingPreservesCompleteStrings(t *testing.T) {
	provider := `plugin:插件:provider:"\\`
	principal := `team:01:"\\用户`
	key := helper.PluginAPIKeyGroupKey(provider, principal)
	var decoded []string
	if err := json.Unmarshal([]byte(strings.TrimPrefix(key, "plugin:")), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 2 || decoded[0] != provider || decoded[1] != principal || !helper.IsPluginAPIKeyGroupKey(key) {
		t.Fatalf("identity did not round trip: %+v", decoded)
	}
	if helper.PluginAPIKeyGroupKey(provider, principal) != key || helper.PluginAPIKeyGroupKey(provider+":other", principal) == key {
		t.Fatal("identity encoding must be stable and source scoped")
	}
	if helper.RedactSensitiveValue(key) != "Third-party Identity" {
		t.Fatal("unregistered identity must use a safe placeholder")
	}
	row := entities.CPAAPIKey{ID: 42, APIKey: key, SourceType: entities.CPAAPIKeySourcePlugin}
	if helper.CPAAPIKeyDisplayName(row) != "Third-party Identity #42" {
		t.Fatal("registered identity must use its stable ID")
	}
	row.KeyAlias = "Team"
	if helper.CPAAPIKeyDisplayName(row) != "Team" {
		t.Fatal("plugin aliases must remain preferred")
	}
}
