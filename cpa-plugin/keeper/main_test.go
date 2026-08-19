package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestNormalizeKeeperURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "adds trailing slash", in: "https://keeper.example.com", want: "https://keeper.example.com/"},
		{name: "keeps subpath root", in: " https://cpa.example.com/keeper/ ", want: "https://cpa.example.com/keeper/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeKeeperURL(tt.in)
			if err != nil {
				t.Fatalf("normalizeKeeperURL() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("normalizeKeeperURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeKeeperURLRejectsInvalidRoots(t *testing.T) {
	for _, input := range []string{"", "ftp://keeper.example.com", "https://keeper.example.com/app?x=1", "https://keeper.example.com/app#top", "https:///app"} {
		t.Run(input, func(t *testing.T) {
			if got, err := normalizeKeeperURL(input); err == nil {
				t.Fatalf("normalizeKeeperURL(%q) = %q, want error", input, got)
			}
		})
	}
}

func TestEmbedURLForKeeperURL(t *testing.T) {
	tests := []struct {
		root string
		want string
	}{
		{root: "https://cpa.example.com/keeper/", want: "https://cpa.example.com/keeper/?embed=cpamc"},
		{root: "https://keeper.example.com/", want: "https://keeper.example.com/?embed=cpamc"},
	}

	for _, tt := range tests {
		t.Run(tt.root, func(t *testing.T) {
			got, err := embedURLForKeeperURL(tt.root)
			if err != nil {
				t.Fatalf("embedURLForKeeperURL() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("embedURLForKeeperURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRegisterResultDeclaresManagementAPIAndKeeperURLField(t *testing.T) {
	originalVersion := pluginVersion
	pluginVersion = "9.8.7-test"
	t.Cleanup(func() {
		pluginVersion = originalVersion
	})

	raw, err := handleMethod("plugin.register", lifecyclePayload("keeper_url: https://keeper.example.com/"))
	if err != nil {
		t.Fatalf("handleMethod(plugin.register) error = %v", err)
	}
	var env rpcEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if !env.OK {
		t.Fatalf("envelope ok = false: %s", string(raw))
	}
	var result struct {
		Metadata struct {
			Name         string `json:"Name"`
			Version      string `json:"Version"`
			ConfigFields []struct {
				Name        string `json:"Name"`
				Type        string `json:"Type"`
				Description string `json:"Description"`
			} `json:"ConfigFields"`
		} `json:"metadata"`
		Capabilities struct {
			ManagementAPI bool `json:"management_api"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(env.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result.Metadata.Name != pluginName {
		t.Fatalf("metadata name = %q, want %q", result.Metadata.Name, pluginName)
	}
	if result.Metadata.Version != "9.8.7-test" {
		t.Fatalf("metadata version = %q, want runtime pluginVersion", result.Metadata.Version)
	}
	if !result.Capabilities.ManagementAPI {
		t.Fatal("management_api capability is false")
	}
	if len(result.Metadata.ConfigFields) != 1 || result.Metadata.ConfigFields[0].Name != "keeper_url" || result.Metadata.ConfigFields[0].Type != "string" {
		t.Fatalf("config fields = %#v, want one keeper_url string field", result.Metadata.ConfigFields)
	}
	if !strings.Contains(result.Metadata.ConfigFields[0].Description, minimumKeeperVersion) {
		t.Fatalf("config field description = %q, want minimum Keeper version", result.Metadata.ConfigFields[0].Description)
	}
}

func TestManagementRegisterReturnsSingleKeeperResource(t *testing.T) {
	if _, err := handleMethod("plugin.register", lifecyclePayload("keeper_url: https://keeper.example.com/")); err != nil {
		t.Fatalf("register: %v", err)
	}
	raw, err := handleMethod("management.register", nil)
	if err != nil {
		t.Fatalf("handleMethod(management.register) error = %v", err)
	}
	var env rpcEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	var result struct {
		Resources []struct {
			Path        string `json:"Path"`
			Menu        string `json:"Menu"`
			Description string `json:"Description"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(env.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(result.Resources) != 1 {
		t.Fatalf("resource count = %d, want 1", len(result.Resources))
	}
	if got := result.Resources[0].Path; got != "/open" {
		t.Fatalf("resource path = %q, want /open", got)
	}
	if got := result.Resources[0].Menu; got != pluginMenu {
		t.Fatalf("resource menu = %q, want %q", got, pluginMenu)
	}
	if !strings.Contains(result.Resources[0].Description, minimumKeeperVersion) {
		t.Fatalf("resource description = %q, want minimum Keeper version", result.Resources[0].Description)
	}
}

func TestManagementHandleRendersKeeperShell(t *testing.T) {
	if _, err := handleMethod("plugin.register", lifecyclePayload("keeper_url: https://keeper.example.com/")); err != nil {
		t.Fatalf("register: %v", err)
	}
	raw, err := handleMethod("management.handle", nil)
	if err != nil {
		t.Fatalf("handleMethod(management.handle) error = %v", err)
	}
	resp := decodeManagementResponse(t, raw)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := string(resp.Body)
	for _, want := range []string{
		`<iframe id="keeperFrame"`,
		`src="https://keeper.example.com/?embed=cpamc"`,
		`cpa-usage-keeper:ready`,
		`CPA Usage Keeper is not available`,
		minimumKeeperVersion,
		`Open Keeper`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("shell body missing %q:\n%s", want, body)
		}
	}
	for _, unexpected := range []string{
		`window.location.replace`,
		`<meta http-equiv="refresh"`,
		`target="_self"`,
	} {
		if strings.Contains(body, unexpected) {
			t.Fatalf("shell body still contains direct redirect fragment %q:\n%s", unexpected, body)
		}
	}
}

func TestKeeperShellFallbackDoesNotShowConfiguredURL(t *testing.T) {
	if _, err := handleMethod("plugin.register", lifecyclePayload("keeper_url: https://private.example.com/hidden/")); err != nil {
		t.Fatalf("register: %v", err)
	}
	raw, err := handleMethod("management.handle", nil)
	if err != nil {
		t.Fatalf("handleMethod(management.handle) error = %v", err)
	}
	resp := decodeManagementResponse(t, raw)
	body := string(resp.Body)
	start := strings.Index(body, `<section id="keeperFallback"`)
	if start < 0 {
		t.Fatalf("fallback section missing:\n%s", body)
	}
	end := strings.Index(body[start:], `</section>`)
	if end < 0 {
		t.Fatalf("fallback section is not closed:\n%s", body)
	}
	fallback := body[start : start+end]
	for _, unexpected := range []string{
		"https://private.example.com/hidden/",
		"private.example.com",
		"keeper_url:",
	} {
		if strings.Contains(fallback, unexpected) {
			t.Fatalf("fallback guidance exposes configured URL fragment %q:\n%s", unexpected, fallback)
		}
	}
}

func TestManagementHandleShowsConfigFallbackForMissingKeeperURL(t *testing.T) {
	if _, err := handleMethod("plugin.register", lifecyclePayload("")); err != nil {
		t.Fatalf("register: %v", err)
	}
	raw, err := handleMethod("management.handle", nil)
	if err != nil {
		t.Fatalf("handleMethod(management.handle) error = %v", err)
	}
	resp := decodeManagementResponse(t, raw)
	body := string(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, "keeper_url") || !strings.Contains(body, "CPA Usage Keeper") {
		t.Fatalf("fallback body does not explain keeper_url:\n%s", body)
	}
	if !strings.Contains(body, minimumKeeperVersion) {
		t.Fatalf("fallback body does not mention minimum Keeper version:\n%s", body)
	}
}

func TestManagementHandleShowsConfigFallbackForInvalidKeeperURL(t *testing.T) {
	if _, err := handleMethod("plugin.register", lifecyclePayload("keeper_url: javascript://keeper.example.com/")); err != nil {
		t.Fatalf("register: %v", err)
	}
	raw, err := handleMethod("management.handle", nil)
	if err != nil {
		t.Fatalf("handleMethod(management.handle) error = %v", err)
	}
	resp := decodeManagementResponse(t, raw)
	body := string(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, "keeper_url must start with http:// or https://") {
		t.Fatalf("fallback body does not explain invalid keeper_url:\n%s", body)
	}
}

func TestKeeperShellHTMLEscapesEmbedURL(t *testing.T) {
	body := keeperShellHTML(`https://keeper.example.com/?embed=cpamc&next="<script>alert(1)</script>`)
	for _, bad := range []string{
		`src="https://keeper.example.com/?embed=cpamc&next="<script>alert(1)</script>"`,
		`const keeperURL = "https://keeper.example.com/?embed=cpamc&next="<script>alert(1)</script>"`,
	} {
		if strings.Contains(body, bad) {
			t.Fatalf("shell body contains unescaped URL fragment %q:\n%s", bad, body)
		}
	}
	for _, want := range []string{
		`src="https://keeper.example.com/?embed=cpamc&amp;next=&#34;&lt;script&gt;alert(1)&lt;/script&gt;"`,
		`const keeperURL = "https://keeper.example.com/?embed=cpamc\u0026next=\"\u003cscript\u003ealert(1)\u003c/script\u003e";`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("shell body missing escaped fragment %q:\n%s", want, body)
		}
	}
}

func lifecyclePayload(configYAML string) []byte {
	raw, err := json.Marshal(struct {
		ConfigYAML    []byte `json:"config_yaml"`
		SchemaVersion uint32 `json:"schema_version"`
	}{ConfigYAML: []byte(configYAML), SchemaVersion: 1})
	if err != nil {
		panic(err)
	}
	return raw
}

func decodeManagementResponse(t *testing.T, raw []byte) managementResponse {
	t.Helper()
	var env rpcEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if !env.OK {
		t.Fatalf("envelope ok = false: %s", string(raw))
	}
	var resp managementResponse
	if err := json.Unmarshal(env.Result, &resp); err != nil {
		t.Fatalf("unmarshal management response: %v", err)
	}
	return resp
}

func TestManagementBodyIsBase64EncodedByJSON(t *testing.T) {
	body := []byte("<!doctype html>")
	raw, err := json.Marshal(managementResponse{Body: body})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), base64.StdEncoding.EncodeToString(body)) {
		t.Fatalf("management response body is not base64 JSON: %s", raw)
	}
}
