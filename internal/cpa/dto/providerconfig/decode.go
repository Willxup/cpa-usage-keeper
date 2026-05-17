package providerconfig

import "fmt"

func decodeOpenAIApiKeyEntries(value any) ([]OpenAIApiKeyEntry, error) {
	rawEntries, ok := value.([]any)
	if !ok {
		return nil, nil
	}
	entries := make([]OpenAIApiKeyEntry, 0, len(rawEntries))
	for _, rawEntry := range rawEntries {
		entry, err := decodeOpenAIApiKeyEntry(rawEntry)
		if err != nil {
			return nil, err
		}
		if entry.APIKey == "" {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func decodeOpenAIApiKeyEntry(raw any) (OpenAIApiKeyEntry, error) {
	switch value := raw.(type) {
	case string:
		return OpenAIApiKeyEntry{APIKey: value}, nil
	case map[string]any:
		return OpenAIApiKeyEntry{
			APIKey:    firstString(value, "api-key", "api_key", "apiKey", "key"),
			AuthIndex: firstString(value, "auth-index", "auth_index", "authIndex"),
		}, nil
	case nil:
		return OpenAIApiKeyEntry{}, nil
	default:
		return OpenAIApiKeyEntry{}, fmt.Errorf("unsupported openai api key entry type %T", raw)
	}
}

func firstString(raw map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := raw[key]
		if !ok {
			continue
		}
		if text, ok := value.(string); ok {
			return text
		}
	}
	return ""
}

func firstStringPtr(raw map[string]any, keys ...string) *string {
	for _, key := range keys {
		value, ok := raw[key]
		if !ok {
			continue
		}
		if text, ok := value.(string); ok {
			return &text
		}
	}
	return nil
}

func firstInt(raw map[string]any, keys ...string) *int {
	for _, key := range keys {
		value, ok := raw[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case float64:
			converted := int(typed)
			return &converted
		case int:
			return &typed
		}
	}
	return nil
}

func firstBool(raw map[string]any, keys ...string) *bool {
	for _, key := range keys {
		value, ok := raw[key]
		if !ok {
			continue
		}
		if typed, ok := value.(bool); ok {
			return &typed
		}
	}
	return nil
}
