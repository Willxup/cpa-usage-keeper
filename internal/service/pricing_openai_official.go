package service

import (
	"context"
	"encoding/json"
	"fmt"
	stdhtml "html"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

var (
	openAIOfficialHTMLTagPattern = regexp.MustCompile(`<[^>]+>`)
	openAIOfficialPricePattern   = regexp.MustCompile(`[-+]?\d+(?:\.\d+)?`)
)

func normalizePricingSyncSource(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "models.dev", "models_dev", "modelsdev":
		return pricingSyncSourceModelsDevID
	case "openai official", "openai_official", "openai-official":
		return pricingSyncSourceOpenAIOfficialID
	default:
		return pricingSyncSourceModelsDevID
	}
}

func fetchOpenAIOfficialCatalog(ctx context.Context, pageURL string) ([]pricingCatalogEntry, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build openai official pricing request: %w", err)
	}
	request.Header.Set("Accept", "text/html")
	request.Header.Set("Accept-Language", "en-US,en;q=0.9")
	request.Header.Set("Cache-Control", "no-cache")
	request.Header.Set("Pragma", "no-cache")
	request.Header.Set("Referer", "https://developers.openai.com/")
	request.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/137.0.0.0 Safari/537.36")

	response, err := pricingSyncHTTPClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetch openai official pricing page: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("fetch openai official pricing page: unexpected status %d", response.StatusCode)
	}

	document, err := html.Parse(io.LimitReader(response.Body, 64<<20))
	if err != nil {
		return nil, fmt.Errorf("parse openai official pricing html: %w", err)
	}

	entries := make([]pricingCatalogEntry, 0)
	visitOpenAIOfficialPricingNodes(document, "", &entries)
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].serviceTier != entries[j].serviceTier {
			return entries[i].serviceTier < entries[j].serviceTier
		}
		return entries[i].model.ID < entries[j].model.ID
	})
	return entries, nil
}

func visitOpenAIOfficialPricingNodes(node *html.Node, currentPane string, entries *[]pricingCatalogEntry) {
	if node == nil {
		return
	}
	nextPane := currentPane
	if node.Type == html.ElementNode {
		attrs := htmlNodeAttributes(node)
		if attrs["data-content-switcher-pane"] == "true" {
			nextPane = strings.TrimSpace(attrs["data-value"])
		}
		if node.Data == "astro-island" {
			switch attrs["component-export"] {
			case "TextTokenPricingTables":
				props, err := decodeAstroProps(attrs["props"])
				if err == nil {
					*entries = append(*entries, extractOpenAIOfficialTextTokenEntries(props)...)
				}
			case "GroupedPricingTable":
				props, err := decodeAstroProps(attrs["props"])
				if err == nil {
					*entries = append(*entries, extractOpenAIOfficialGroupedEntries(props, nextPane)...)
				}
			}
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		visitOpenAIOfficialPricingNodes(child, nextPane, entries)
	}
}

func htmlNodeAttributes(node *html.Node) map[string]string {
	attrs := make(map[string]string, len(node.Attr))
	for _, attr := range node.Attr {
		attrs[attr.Key] = attr.Val
	}
	return attrs
}

func decodeAstroProps(raw string) (map[string]any, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("astro props are empty")
	}
	unescaped := stdhtml.UnescapeString(raw)
	var payload any
	if err := json.Unmarshal([]byte(unescaped), &payload); err != nil {
		return nil, fmt.Errorf("decode astro props json: %w", err)
	}
	props, ok := decodeAstroValue(payload).(map[string]any)
	if !ok {
		return nil, fmt.Errorf("astro props root is not an object")
	}
	return props, nil
}

func decodeAstroValue(value any) any {
	switch typed := value.(type) {
	case []any:
		if len(typed) == 2 {
			if tag, ok := astroTypedEncodingTag(typed[0]); ok {
				switch tag {
				case 0:
					return decodeAstroValue(typed[1])
				case 1:
					items, ok := typed[1].([]any)
					if !ok {
						return []any{}
					}
					decoded := make([]any, 0, len(items))
					for _, item := range items {
						decoded = append(decoded, decodeAstroValue(item))
					}
					return decoded
				}
			}
		}
		decoded := make([]any, 0, len(typed))
		for _, item := range typed {
			decoded = append(decoded, decodeAstroValue(item))
		}
		return decoded
	case map[string]any:
		decoded := make(map[string]any, len(typed))
		for key, item := range typed {
			decoded[key] = decodeAstroValue(item)
		}
		return decoded
	default:
		return typed
	}
}

func astroTypedEncodingTag(value any) (int, bool) {
	number, ok := value.(float64)
	if !ok {
		return 0, false
	}
	return int(number), true
}

func extractOpenAIOfficialTextTokenEntries(props map[string]any) []pricingCatalogEntry {
	serviceTier, ok := openAIOfficialServiceTier(props["tier"])
	if !ok {
		return nil
	}
	rows, ok := props["rows"].([]any)
	if !ok {
		return nil
	}
	entries := make([]pricingCatalogEntry, 0, len(rows))
	for _, rowValue := range rows {
		row, ok := rowValue.([]any)
		if !ok || len(row) < 4 {
			continue
		}
		entry, ok := openAIOfficialPricingEntryFromCells(row[0], row[1], row[2], row[3], serviceTier)
		if ok {
			entries = append(entries, entry)
		}
	}
	return entries
}

func extractOpenAIOfficialGroupedEntries(props map[string]any, currentPane string) []pricingCatalogEntry {
	serviceTier, ok := openAIOfficialServiceTier(currentPane)
	if !ok || !matchesOpenAIOfficialTokenGroupHeadings(props["headings"]) {
		return nil
	}
	groups, ok := props["groups"].([]any)
	if !ok {
		return nil
	}
	entries := make([]pricingCatalogEntry, 0)
	for _, groupValue := range groups {
		group, ok := groupValue.(map[string]any)
		if !ok {
			continue
		}
		rows, ok := group["rows"].([]any)
		if !ok {
			continue
		}
		for _, rowValue := range rows {
			row, ok := rowValue.([]any)
			if !ok || len(row) < 4 {
				continue
			}
			entry, ok := openAIOfficialPricingEntryFromCells(row[0], row[1], row[2], row[3], serviceTier)
			if ok {
				entries = append(entries, entry)
			}
		}
	}
	return entries
}

func matchesOpenAIOfficialTokenGroupHeadings(value any) bool {
	headings, ok := value.([]any)
	if !ok || len(headings) < 5 {
		return false
	}
	return strings.EqualFold(openAIOfficialPlainText(headings[0]), "Category") &&
		strings.EqualFold(openAIOfficialPlainText(headings[1]), "Model") &&
		strings.EqualFold(openAIOfficialPlainText(headings[2]), "Input") &&
		strings.EqualFold(openAIOfficialPlainText(headings[3]), "Cached input") &&
		strings.HasPrefix(strings.ToLower(openAIOfficialPlainText(headings[4])), "output")
}

func openAIOfficialServiceTier(value any) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(openAIOfficialPlainText(value))) {
	case "standard":
		return "default", true
	case "priority":
		return "priority", true
	default:
		return "", false
	}
}

func openAIOfficialPricingEntryFromCells(modelCell, inputCell, cacheCell, outputCell any, serviceTier string) (pricingCatalogEntry, bool) {
	modelName := normalizeOpenAIOfficialModelName(openAIOfficialPlainText(modelCell))
	if modelName == "" {
		return pricingCatalogEntry{}, false
	}
	input, ok := openAIOfficialPriceValue(inputCell)
	if !ok {
		return pricingCatalogEntry{}, false
	}
	output, ok := openAIOfficialPriceValue(outputCell)
	if !ok {
		return pricingCatalogEntry{}, false
	}
	cache := 0.0
	if value, ok := openAIOfficialPriceValue(cacheCell); ok {
		cache = value
	}
	return pricingCatalogEntry{
		providerID:   "openai",
		providerName: "OpenAI",
		serviceTier:  serviceTier,
		model: modelsDevModel{
			ID:     modelName,
			Name:   modelName,
			Family: "openai",
			Cost: modelsDevCost{
				Input:     float64Ptr(input),
				Output:    float64Ptr(output),
				CacheRead: float64Ptr(cache),
			},
		},
	}, true
}

func openAIOfficialPlainText(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return normalizeOpenAIOfficialWhitespace(stripOpenAIOfficialHTML(typed))
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case map[string]any:
		if htmlText, ok := typed["__pricingHtml"].(string); ok {
			return normalizeOpenAIOfficialWhitespace(stripOpenAIOfficialHTML(htmlText))
		}
		return ""
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func openAIOfficialPriceValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case nil:
		return 0, false
	case float64:
		return typed, true
	default:
		text := strings.TrimSpace(openAIOfficialPlainText(typed))
		switch strings.ToLower(text) {
		case "", "-", "n/a":
			return 0, false
		case "free":
			return 0, true
		}
		match := openAIOfficialPricePattern.FindString(text)
		if match == "" {
			return 0, false
		}
		parsed, err := strconv.ParseFloat(match, 64)
		if err != nil {
			return 0, false
		}
		return parsed, true
	}
}

func normalizeOpenAIOfficialModelName(value string) string {
	normalized := normalizeOpenAIOfficialWhitespace(value)
	if index := strings.Index(normalized, " ("); index >= 0 {
		normalized = normalized[:index]
	}
	return strings.TrimSpace(normalized)
}

func stripOpenAIOfficialHTML(value string) string {
	if value == "" {
		return ""
	}
	unescaped := stdhtml.UnescapeString(value)
	return openAIOfficialHTMLTagPattern.ReplaceAllString(unescaped, " ")
}

func normalizeOpenAIOfficialWhitespace(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func float64Ptr(value float64) *float64 {
	return &value
}
