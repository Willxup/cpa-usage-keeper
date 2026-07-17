package relayusage

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

const platformOverridesSettingKey = "relay_usage.platform_overrides"

// ServiceOptions 配置 Service 行为。
type ServiceOptions struct {
	WorkerLimit int
}

// Service 编排中转商用量查询：取 identity -> 匹配平台 -> adapter 直连。
// 与 internal/quota.Service 隔离：后者只服务 OAuth AuthFile 身份，本服务只服务 AIProvider 身份。
type Service struct {
	db        *gorm.DB
	registry  map[string]Adapter
	workerSem chan struct{}
}

// NewService 构造 Service。registry 应已注入 HTTP client。
func NewService(db *gorm.DB, registry map[string]Adapter, options ServiceOptions) *Service {
	workerLimit := options.WorkerLimit
	if workerLimit <= 0 {
		workerLimit = 10
	}
	if workerLimit > 100 {
		workerLimit = 100
	}
	return &Service{
		db:        db,
		registry:  registry,
		workerSem: make(chan struct{}, workerLimit),
	}
}

// UsageRequest 是批量用量查询入参。identity_id 与 UsageIdentity.id 一致为 string。
type UsageRequest struct {
	IdentityIDs []string `json:"identity_ids"`
}

// UsageItem 是单条 identity 的查询结果。
type UsageItem struct {
	IdentityID string            `json:"identity_id"`
	Platform   string            `json:"platform,omitempty"`
	Result     *RelayUsageResult `json:"result,omitempty"`
	Skipped    string            `json:"skipped,omitempty"`
}

// UsageResponse 是批量用量查询响应。
type UsageResponse struct {
	Items []UsageItem `json:"items"`
}

// GetUsage 并发查询多条 identity 的中转商用量。
func (s *Service) GetUsage(ctx context.Context, request UsageRequest) (UsageResponse, error) {
	if len(request.IdentityIDs) == 0 {
		return UsageResponse{}, fmt.Errorf("%w: identity_ids is required", ErrValidation)
	}
	overrides, err := s.GetPlatformOverrides(ctx)
	if err != nil {
		return UsageResponse{}, err
	}
	items := make([]UsageItem, len(request.IdentityIDs))
	var wg sync.WaitGroup
	for idx, id := range request.IdentityIDs {
		items[idx] = UsageItem{IdentityID: id}
		wg.Add(1)
		go func(i int, identityID string) {
			defer wg.Done()
			select {
			case s.workerSem <- struct{}{}:
				defer func() { <-s.workerSem }()
				items[i] = s.fetchOne(ctx, identityID, overrides)
			case <-ctx.Done():
				items[i] = UsageItem{IdentityID: identityID, Skipped: ctx.Err().Error()}
			}
		}(idx, id)
	}
	wg.Wait()
	return UsageResponse{Items: items}, nil
}

func (s *Service) fetchOne(ctx context.Context, identityID string, overrides map[string]string) UsageItem {
	id, err := strconv.ParseInt(strings.TrimSpace(identityID), 10, 64)
	if err != nil {
		return UsageItem{IdentityID: identityID, Skipped: SkipIdentityNotFound}
	}
	identity, err := repository.FindUsageIdentityByID(ctx, s.db, id)
	if err != nil {
		return UsageItem{IdentityID: identityID, Skipped: SkipIdentityNotFound}
	}
	if identity.AuthType != entities.UsageIdentityAuthTypeAIProvider {
		return UsageItem{IdentityID: identityID, Skipped: SkipNotAIProvider}
	}
	if strings.TrimSpace(identity.LookupKey) == "" {
		return UsageItem{IdentityID: identityID, Skipped: SkipNoAPIKey}
	}
	platform := Match(identity, overrides)
	if platform == "" {
		return UsageItem{IdentityID: identityID, Skipped: SkipUnsupported}
	}
	adapter, ok := s.registry[platform]
	if !ok {
		return UsageItem{IdentityID: identityID, Skipped: SkipUnsupported}
	}
	result, err := adapter.Fetch(ctx, identity.LookupKey, identity.BaseURL)
	if err != nil {
		return UsageItem{
			IdentityID: identityID,
			Platform:   platform,
			Result:     &RelayUsageResult{Platform: platform, FetchedAt: time.Now(), Error: err.Error()},
		}
	}
	result.Platform = platform
	if result.FetchedAt.IsZero() {
		result.FetchedAt = time.Now()
	}
	return UsageItem{IdentityID: identityID, Platform: platform, Result: &result}
}

// PlatformAssignment 描述单条 identity 的平台判定，供前端展示「自动识别 / 手动覆盖」与下拉默认值。
type PlatformAssignment struct {
	IdentityID string `json:"identity_id"`
	Platform   string `json:"platform"` // 最终生效平台（空=不支持/官方）
	Source     string `json:"source"`   // "override" | "auto" | "none"
}

// GetPlatformAssignments 批量返回 identity 的平台判定，只做匹配不调用中转商接口。
func (s *Service) GetPlatformAssignments(ctx context.Context, identityIDs []string) ([]PlatformAssignment, error) {
	overrides, err := s.GetPlatformOverrides(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]PlatformAssignment, 0, len(identityIDs))
	for _, identityID := range identityIDs {
		id, err := strconv.ParseInt(strings.TrimSpace(identityID), 10, 64)
		if err != nil {
			out = append(out, PlatformAssignment{IdentityID: identityID, Source: "none"})
			continue
		}
		identity, err := repository.FindUsageIdentityByID(ctx, s.db, id)
		if err != nil {
			out = append(out, PlatformAssignment{IdentityID: identityID, Source: "none"})
			continue
		}
		assignment := PlatformAssignment{IdentityID: identityID, Source: "auto"}
		if _, ok := overrides[strconv.FormatInt(identity.ID, 10)]; ok {
			assignment.Source = "override"
		}
		assignment.Platform = Match(identity, overrides)
		out = append(out, assignment)
	}
	return out, nil
}

// GetPlatformOverrides 读取 identity_id -> 平台 的手动覆盖映射。
func (s *Service) GetPlatformOverrides(ctx context.Context) (map[string]string, error) {
	if s.db == nil {
		return nil, nil
	}
	setting, found, err := repository.GetAppSetting(ctx, s.db, platformOverridesSettingKey)
	if err != nil {
		return nil, err
	}
	if !found || setting.Value == nil || strings.TrimSpace(*setting.Value) == "" {
		return nil, nil
	}
	var raw map[string]string
	if err := json.Unmarshal([]byte(*setting.Value), &raw); err != nil {
		return nil, fmt.Errorf("decode platform overrides: %w", err)
	}
	out := make(map[string]string, len(raw))
	for key, platform := range raw {
		normalized := strings.TrimSpace(strings.ToLower(platform))
		if normalized == "" {
			continue
		}
		out[strings.TrimSpace(key)] = normalized
	}
	return out, nil
}

// PlatformOverridesRequest 是更新手动覆盖的入参。
// 平台值取 glm/minimax/kimi/deepseek/none；空值表示删除该条覆盖。
type PlatformOverridesRequest struct {
	Overrides map[string]string `json:"overrides"`
}

// UpdatePlatformOverrides 全量替换手动覆盖映射。
func (s *Service) UpdatePlatformOverrides(ctx context.Context, request PlatformOverridesRequest) (map[string]string, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	raw := make(map[string]string, len(request.Overrides))
	for key, platform := range request.Overrides {
		normalized := strings.TrimSpace(strings.ToLower(platform))
		if normalized == "" {
			continue
		}
		raw[strings.TrimSpace(key)] = normalized
	}
	payload, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("marshal platform overrides: %w", err)
	}
	value := string(payload)
	if _, err := repository.UpsertAppSetting(ctx, s.db, entities.AppSetting{
		SettingKey: platformOverridesSettingKey,
		Value:      &value,
		ValueType:  entities.AppSettingValueTypeJSON,
	}); err != nil {
		return nil, err
	}
	return s.GetPlatformOverrides(ctx)
}
