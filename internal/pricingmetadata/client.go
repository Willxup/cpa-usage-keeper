// Package pricingmetadata 负责外部价格目录的下载和基础 token 价格归一化。
package pricingmetadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var ErrInvalidSource = errors.New("unsupported pricing source")

type Source struct{ ID, Name, URL string }

func SourceByID(id string) (Source, error) {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "", "models-dev":
		return Source{"models-dev", "Models.dev", "https://models.dev/api.json"}, nil
	case "litellm":
		return Source{"litellm", "LiteLLM", "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"}, nil
	case "model-json", "local", "models-json":
		return Source{"model-json", "Model JSON", "file://model.json"}, nil
	case "dp-agents":
		return Source{"dp-agents", "DP Agents", "https://raw.githubusercontent.com/darkrei08/dp-agents/main/models.json"}, nil
	default:
		return Source{}, ErrInvalidSource
	}
}

// Cost 保留来源字段的缺失状态；服务层沿用预览中缺失缓存价按 0 处理的约定。
type Cost struct {
	Input      *float64 `json:"input"`
	Output     *float64 `json:"output"`
	CacheRead  *float64 `json:"cache_read"`
	CacheWrite *float64 `json:"cache_write"`
}

type Model struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	Family              string `json:"family"`
	LastUpdated         string `json:"last_updated"`
	Status              string `json:"status"`
	ContextWindowTokens int64  `json:"context_window_tokens,omitempty"`
	MaxOutputTokens     int64  `json:"max_output_tokens,omitempty"`
	Cost                Cost   `json:"cost"`
}

type Entry struct {
	ProviderID   string
	ProviderName string
	Model        Model
}

type Catalog struct {
	Source  Source
	Entries []Entry
}

type Client struct{ httpClient *http.Client }

func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 12 * time.Second}
	}
	return &Client{httpClient: httpClient}
}

func (c *Client) Fetch(ctx context.Context, sourceID string) (Catalog, error) {
	source, err := SourceByID(sourceID)
	if err != nil {
		return Catalog{}, err
	}
	if strings.HasPrefix(source.URL, "file://") {
		filePath := strings.TrimPrefix(source.URL, "file://")
		return LoadModelJSONFile(filePath)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source.URL, nil)
	if err != nil {
		return Catalog{}, fmt.Errorf("build pricing catalog request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return Catalog{}, fmt.Errorf("fetch %s pricing catalog: %w", source.Name, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Catalog{}, fmt.Errorf("fetch %s pricing catalog: unexpected status %d", source.Name, response.StatusCode)
	}
	// 每次预览读取所选来源；适配器统一为同一种目录，后续匹配不依赖上游 JSON 格式。
	decoder := json.NewDecoder(io.LimitReader(response.Body, 64<<20))
	var entries []Entry
	switch source.ID {
	case "litellm":
		entries, err = decodeLiteLLM(decoder)
	case "model-json", "dp-agents":
		entries, err = decodeModelJSON(response.Body)
	default:
		entries, err = decodeModelsDev(decoder)
	}
	if err != nil {
		return Catalog{}, fmt.Errorf("decode %s pricing catalog: %w", source.Name, err)
	}
	return Catalog{Source: source, Entries: entries}, nil
}
