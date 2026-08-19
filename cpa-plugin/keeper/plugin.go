package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
	"gopkg.in/yaml.v3"
)

const (
	pluginName           = "Keeper"
	pluginMenu           = "Keeper"
	pluginAuthor         = "Willxup"
	pluginRepository     = "https://github.com/Willxup/cpa-usage-keeper"
	resourcePath         = "/open"
	configKeeperURL      = "keeper_url"
	shellPageTitle       = "CPA Usage Keeper"
	minimumKeeperVersion = "v1.12.2"
)

var (
	pluginVersion = "0.0.0-dev"
	stateMu       sync.RWMutex
	state         = runtimeState{configError: "keeper_url is required"}
)

type runtimeState struct {
	keeperURL   string
	embedURL    string
	configError string
}

type pluginConfig struct {
	KeeperURL string `yaml:"keeper_url"`
}

type lifecycleRequest struct {
	ConfigYAML    []byte `json:"config_yaml"`
	SchemaVersion uint32 `json:"schema_version"`
}

type rpcEnvelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type capabilityFlags struct {
	ManagementAPI bool `json:"management_api"`
}

type registrationResult struct {
	SchemaVersion uint32             `json:"schema_version"`
	Metadata      pluginapi.Metadata `json:"metadata"`
	Capabilities  capabilityFlags    `json:"capabilities"`
}

type managementRegistrationResponse struct {
	Resources []resourceRoute `json:"resources,omitempty"`
}

type resourceRoute struct {
	Path        string `json:"Path"`
	Menu        string `json:"Menu"`
	Description string `json:"Description"`
}

type managementResponse struct {
	StatusCode int         `json:"StatusCode"`
	Headers    http.Header `json:"Headers,omitempty"`
	Body       []byte      `json:"Body,omitempty"`
}

func handleMethod(method string, request []byte) ([]byte, error) {
	switch method {
	case pluginabi.MethodPluginRegister, pluginabi.MethodPluginReconfigure:
		applyConfig(request)
		return okEnvelope(registrationResult{
			SchemaVersion: pluginabi.SchemaVersion,
			Metadata:      pluginMetadata(),
			Capabilities:  capabilityFlags{ManagementAPI: true},
		})
	case pluginabi.MethodManagementRegister:
		return okEnvelope(managementRegistrationResponse{Resources: []resourceRoute{{
			Path:        resourcePath,
			Menu:        pluginMenu,
			Description: "Open cpa-usage-keeper inside CPAMC. Requires cpa-usage-keeper " + minimumKeeperVersion + " or later.",
		}}})
	case pluginabi.MethodManagementHandle:
		return okEnvelope(currentManagementResponse())
	default:
		return errorEnvelope("unknown_method", "unknown method: "+method), nil
	}
}

func pluginMetadata() pluginapi.Metadata {
	return pluginapi.Metadata{
		Name:             pluginName,
		Version:          pluginVersion,
		Author:           pluginAuthor,
		GitHubRepository: pluginRepository,
		ConfigFields: []pluginapi.ConfigField{{
			Name:        configKeeperURL,
			Type:        pluginapi.ConfigFieldTypeString,
			Description: "Full cpa-usage-keeper application root URL. Requires cpa-usage-keeper " + minimumKeeperVersion + " or later.",
		}},
	}
}

func applyConfig(request []byte) {
	next := runtimeState{}
	if len(bytes.TrimSpace(request)) == 0 {
		next.configError = "keeper_url is required"
		setRuntimeState(next)
		return
	}

	var req lifecycleRequest
	if err := json.Unmarshal(request, &req); err != nil {
		next.configError = "invalid plugin lifecycle request"
		setRuntimeState(next)
		return
	}

	var cfg pluginConfig
	if len(bytes.TrimSpace(req.ConfigYAML)) > 0 {
		if err := yaml.Unmarshal(req.ConfigYAML, &cfg); err != nil {
			next.configError = "invalid plugin YAML config"
			setRuntimeState(next)
			return
		}
	}

	normalized, err := normalizeKeeperURL(cfg.KeeperURL)
	if err != nil {
		next.configError = err.Error()
		setRuntimeState(next)
		return
	}
	embedURL, err := embedURLForKeeperURL(normalized)
	if err != nil {
		next.configError = err.Error()
		setRuntimeState(next)
		return
	}
	next.keeperURL = normalized
	next.embedURL = embedURL
	setRuntimeState(next)
}

func setRuntimeState(next runtimeState) {
	stateMu.Lock()
	state = next
	stateMu.Unlock()
}

func currentRuntimeState() runtimeState {
	stateMu.RLock()
	defer stateMu.RUnlock()
	return state
}

func normalizeKeeperURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("keeper_url is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("keeper_url is invalid: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", errors.New("keeper_url must start with http:// or https://")
	}
	if u.Host == "" {
		return "", errors.New("keeper_url host is required")
	}
	if u.RawQuery != "" || u.ForceQuery {
		return "", errors.New("keeper_url must not include query parameters")
	}
	if u.Fragment != "" {
		return "", errors.New("keeper_url must not include a fragment")
	}
	if u.Path == "" {
		u.Path = "/"
	} else if !strings.HasSuffix(u.Path, "/") {
		u.Path += "/"
	}
	u.RawPath = ""
	return u.String(), nil
}

func embedURLForKeeperURL(raw string) (string, error) {
	normalized, err := normalizeKeeperURL(raw)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(normalized)
	if err != nil {
		return "", fmt.Errorf("keeper_url is invalid: %w", err)
	}
	query := u.Query()
	query.Set("embed", "cpamc")
	u.RawQuery = query.Encode()
	return u.String(), nil
}

func currentManagementResponse() managementResponse {
	current := currentRuntimeState()
	body := configFallbackHTML(current.configError)
	if current.configError == "" && current.embedURL != "" {
		body = keeperShellHTML(current.embedURL)
	}
	return managementResponse{
		StatusCode: http.StatusOK,
		Headers: http.Header{
			"content-type":           []string{"text/html; charset=utf-8"},
			"cache-control":          []string{"no-store"},
			"x-content-type-options": []string{"nosniff"},
		},
		Body: []byte(body),
	}
}

func keeperShellHTML(embedURL string) string {
	escapedURL := html.EscapeString(embedURL)
	jsURL, _ := json.Marshal(embedURL)
	return "<!doctype html>\n" +
		"<html lang=\"en\">\n" +
		"<head>\n" +
		"  <meta charset=\"utf-8\">\n" +
		"  <meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n" +
		"  <title>" + shellPageTitle + "</title>\n" +
		"  <style>html,body{margin:0;width:100%;height:100%;font:14px system-ui,-apple-system,BlinkMacSystemFont,\"Segoe UI\",sans-serif;background:#f8fafc;color:#111827}.shell{position:fixed;inset:0;overflow:hidden}.keeper-frame{display:block;width:100%;height:100%;border:0;background:#fff}.status,.fallback{position:absolute;inset:0;display:grid;place-items:center;background:#f8fafc}.status-panel,.fallback-panel{max-width:560px;padding:28px;line-height:1.6;text-align:center}.fallback{display:none}.fallback h1{margin:0 0 12px;font-size:22px}.code{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;background:#e5e7eb;border-radius:6px;padding:2px 6px}.actions{display:flex;gap:12px;justify-content:center;margin-top:18px}.actions button{border:1px solid #cbd5e1;border-radius:8px;background:#fff;color:#111827;cursor:pointer;padding:8px 14px}.actions button.primary{border-color:#2563eb;background:#2563eb;color:#fff}html[data-ready=\"true\"] .status{display:none}html[data-failed=\"true\"] .status{display:none}html[data-failed=\"true\"] .fallback{display:grid}</style>\n" +
		"</head>\n" +
		"<body><main class=\"shell\"><iframe id=\"keeperFrame\" class=\"keeper-frame\" title=\"CPA Usage Keeper\" src=\"" + escapedURL + "\"></iframe><section id=\"keeperStatus\" class=\"status\" aria-live=\"polite\"><div class=\"status-panel\">Connecting to CPA Usage Keeper...</div></section><section id=\"keeperFallback\" class=\"fallback\" aria-live=\"assertive\"><div class=\"fallback-panel\"><h1>CPA Usage Keeper is not available</h1><p>Please check the CPA plugin configuration. Set <span class=\"code\">keeper_url</span> to the Keeper application root URL, and make sure Keeper is running and reachable from this browser.</p><p>This plugin requires cpa-usage-keeper " + minimumKeeperVersion + " or later. If Keeper is served under a base path, include that base path in the configured URL.</p><div class=\"actions\"><button id=\"retryKeeper\" class=\"primary\" type=\"button\">Retry</button><button id=\"openKeeper\" type=\"button\">Open Keeper</button></div></div></section></main><script>(function(){const keeperURL = " + string(jsURL) + ";const readyType=\"cpa-usage-keeper:ready\";const frame=document.getElementById(\"keeperFrame\");const retryButton=document.getElementById(\"retryKeeper\");const openButton=document.getElementById(\"openKeeper\");let timer=0;const expectedOrigin=(function(){try{return new URL(keeperURL).origin;}catch{return \"\";}})();function markConnecting(){document.documentElement.removeAttribute(\"data-ready\");document.documentElement.removeAttribute(\"data-failed\");}function markReady(){window.clearTimeout(timer);document.documentElement.removeAttribute(\"data-failed\");document.documentElement.setAttribute(\"data-ready\",\"true\");}function markFailed(){document.documentElement.removeAttribute(\"data-ready\");document.documentElement.setAttribute(\"data-failed\",\"true\");}function startTimer(){window.clearTimeout(timer);timer=window.setTimeout(markFailed,8000);}window.addEventListener(\"message\",function(event){if(!frame||event.source!==frame.contentWindow)return;if(expectedOrigin&&event.origin!==expectedOrigin)return;const data=event.data||{};if(data.type===readyType)markReady();});retryButton.addEventListener(\"click\",function(){markConnecting();frame.src=keeperURL;startTimer();});openButton.addEventListener(\"click\",function(){window.open(keeperURL,\"_blank\",\"noopener\");});startTimer();})();</script></body>\n" +
		"</html>\n"
}

func configFallbackHTML(message string) string {
	if strings.TrimSpace(message) == "" {
		message = "keeper_url is not configured"
	}
	return "<!doctype html>\n" +
		"<html lang=\"en\">\n" +
		"<head>\n" +
		"  <meta charset=\"utf-8\">\n" +
		"  <meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n" +
		"  <title>CPA Usage Keeper</title>\n" +
		"  <style>body{margin:0;font:14px system-ui,-apple-system,BlinkMacSystemFont,\"Segoe UI\",sans-serif;background:#f8fafc;color:#111827;display:grid;min-height:100vh;place-items:center}.panel{max-width:560px;padding:24px;line-height:1.6}.label{font-weight:700}.code{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;background:#e5e7eb;border-radius:6px;padding:2px 6px}</style>\n" +
		"</head>\n" +
		"<body><main class=\"panel\"><h1>CPA Usage Keeper</h1><p class=\"label\">Plugin configuration is incomplete.</p><p>" + html.EscapeString(message) + "</p><p>Set <span class=\"code\">keeper_url</span> to the full Keeper application root URL.</p><p>This plugin requires cpa-usage-keeper " + minimumKeeperVersion + " or later.</p></main></body>\n" +
		"</html>\n"
}

func okEnvelope(v any) ([]byte, error) {
	result, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return json.Marshal(rpcEnvelope{OK: true, Result: result})
}

func errorEnvelope(code, message string) []byte {
	raw, _ := json.Marshal(rpcEnvelope{OK: false, Error: &rpcError{Code: code, Message: message}})
	return raw
}
