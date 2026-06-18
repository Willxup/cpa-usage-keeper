package service

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func LoadPricingSyncModelAliases(path string) (map[string][]string, error) {
	if strings.TrimSpace(path) == "" {
		return map[string][]string{}, nil
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read pricing sync model aliases file: %w", err)
	}

	aliases, err := decodePricingSyncModelAliases(content)
	if err != nil {
		return nil, fmt.Errorf("decode pricing sync model aliases file: %w", err)
	}
	return normalizePricingSyncModelAliases(aliases), nil
}

func pricingSyncModelAliasesForModel(model string, aliases map[string][]string) []string {
	if aliases == nil {
		return nil
	}
	return aliases[normalizePricingSyncAliasKey(model)]
}

func normalizePricingSyncAliasKey(value string) string {
	return normalizePricingModelKey(stripPricingModelPrefix(value))
}

func decodePricingSyncModelAliases(content []byte) (map[string][]string, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(content, &raw); err != nil {
		return nil, err
	}

	decoded := make(map[string][]string, len(raw))
	for rawKey, rawValue := range raw {
		key := normalizePricingSyncAliasKey(rawKey)
		if key == "" {
			continue
		}
		values, err := decodePricingSyncModelAliasValues(rawValue)
		if err != nil {
			return nil, fmt.Errorf("model %q: %w", rawKey, err)
		}
		decoded[key] = normalizePricingSyncAliasValues(values)
	}
	return decoded, nil
}

func normalizePricingSyncModelAliases(source map[string][]string) map[string][]string {
	if len(source) == 0 {
		return map[string][]string{}
	}

	normalized := make(map[string][]string, len(source))
	for rawKey, values := range source {
		key := normalizePricingSyncAliasKey(rawKey)
		if key == "" {
			continue
		}
		normalized[key] = normalizePricingSyncAliasValues(values)
	}
	return normalized
}

func decodePricingSyncModelAliasValues(rawValue json.RawMessage) ([]string, error) {
	if pricingSyncAliasRawValueIsEmptyOrNull(rawValue) {
		return nil, nil
	}

	var single string
	if err := json.Unmarshal(rawValue, &single); err == nil {
		return []string{single}, nil
	}

	var multiple []string
	if err := json.Unmarshal(rawValue, &multiple); err == nil {
		return multiple, nil
	}

	return nil, fmt.Errorf("must be a string, string array, or null")
}

func pricingSyncAliasRawValueIsEmptyOrNull(rawValue json.RawMessage) bool {
	start := 0
	end := len(rawValue)
	for start < end && isJSONWhitespace(rawValue[start]) {
		start++
	}
	for start < end && isJSONWhitespace(rawValue[end-1]) {
		end--
	}
	if start == end {
		return true
	}
	return end-start == 4 &&
		rawValue[start] == 'n' &&
		rawValue[start+1] == 'u' &&
		rawValue[start+2] == 'l' &&
		rawValue[start+3] == 'l'
}

func isJSONWhitespace(value byte) bool {
	switch value {
	case ' ', '\n', '\r', '\t':
		return true
	default:
		return false
	}
}

func normalizePricingSyncAliasValues(values []string) []string {
	normalized := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		key := normalizePricingSyncAliasKey(trimmed)
		if trimmed == "" || key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, trimmed)
	}
	return normalized
}

func clonePricingSyncModelAliases(source map[string][]string) map[string][]string {
	if len(source) == 0 {
		return map[string][]string{}
	}
	cloned := make(map[string][]string, len(source))
	for key, values := range source {
		cloned[key] = append([]string(nil), values...)
	}
	return cloned
}

func mergePricingSyncModelAliases(base map[string][]string, overrides map[string][]string) map[string][]string {
	merged := clonePricingSyncModelAliases(base)
	for key, values := range overrides {
		merged[key] = append([]string(nil), values...)
	}
	return merged
}
