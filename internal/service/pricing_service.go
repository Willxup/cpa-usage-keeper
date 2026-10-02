package service

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"

	"cpa-usage-keeper/internal/cpa/dto/response"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/pricingmetadata"
	"cpa-usage-keeper/internal/repository"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

var ErrInvalidPricingInput = errors.New("invalid pricing input")
var ErrPricingModelNotFound = errors.New("pricing model not found")

type PricingProvider interface {
	// GetPricingRecalculationOptions 返回按部署时区和当前热表计算的可选小时及配置修订。
	GetPricingRecalculationOptions(context.Context) (servicedto.RecalculationOptions, error)
	// StartPricingRecalculation 原子受理唯一内存任务；运行中重复请求返回现行任务。
	StartPricingRecalculation(context.Context, servicedto.StartRecalculationRequest) (servicedto.StartRecalculationResponse, error)
	// CurrentPricingRecalculation 返回当前进程唯一任务状态，进程重启后为空。
	CurrentPricingRecalculation(context.Context) (*servicedto.RecalculationTask, error)
	// WaitPricingRecalculation 在 App 取消生命周期后等后台重算退出，再关闭数据库。
	WaitPricingRecalculation()
	// ListPricingModels 同一配置临界区返回已发布完整配置与对应修订号。
	ListPricingModels(context.Context) (servicedto.PricingModelsResponse, error)
	// SavePricingModel 同事务替换一个模型的基础价、分支和规则；不等待已取得旧快照的事件批次。
	SavePricingModel(context.Context, pricing.ModelPricingConfig) (servicedto.SavePricingModelResponse, error)
	// DeletePricingModel 只删除当前配置及规则并推进修订，已存事件费用保持不变。
	DeletePricingModel(context.Context, string) (servicedto.DeletePricingModelResponse, error)
	// FetchPricingSync 读取选定来源并沿现有匹配规则只返回待审核的基础价。
	FetchPricingSync(context.Context, string) (servicedto.PricingSyncFetchResponse, error)
	// ApplyPricingSync 将选中项的默认单价作为一个配置事务提交。
	ApplyPricingSync(context.Context, servicedto.PricingSyncApplyRequest) (servicedto.PricingSyncApplyResponse, error)
	ListUsedModels(context.Context) ([]string, error)
}

type ModelsFetcher interface {
	FetchModels(context.Context) (*response.ModelsResult, error)
}

type pricingService struct {
	db                   *gorm.DB
	modelsFetcher        ModelsFetcher
	catalog              *pricing.Catalog
	mutationMu           sync.Mutex
	metadataClient       *pricingmetadata.Client
	recalculation        PricingRecalculationDependencies
	recalculationRunning bool
	recalculationTask    *servicedto.RecalculationTask
	recalculationWG      sync.WaitGroup
}

func requirePricingCatalog(catalog *pricing.Catalog) *pricing.Catalog {
	if catalog == nil {
		panic("pricing catalog is required")
	}
	return catalog
}

// ListUsedModels 返回本地事件模型与 CPA 可用模型的合并结果，供完整价格编辑的模型选择使用。
func (s *pricingService) ListUsedModels(ctx context.Context) ([]string, error) {
	return s.effectiveModels(ctx)
}

// effectiveModels 以本地已用模型为可靠基线；CPA 模型列表获取失败时保留本地结果。
func (s *pricingService) effectiveModels(ctx context.Context) ([]string, error) {
	localModels, err := repository.ListUsedModels(s.db)
	if err != nil {
		return nil, err
	}
	if s.modelsFetcher == nil {
		return localModels, nil
	}

	result, err := s.modelsFetcher.FetchModels(ctx)
	if err != nil {
		logrus.WithError(err).Error("pricing model listing falling back to local usage aggregation")
		return localModels, nil
	}

	logrus.Debug("pricing model listing using CPA models endpoint")
	return mergeModelNames(localModels, extractCPAModelIDs(result)), nil
}

func extractCPAModelIDs(result *response.ModelsResult) []string {
	if result == nil {
		return []string{}
	}
	models := make([]string, 0, len(result.Payload.Data))
	for _, model := range result.Payload.Data {
		models = append(models, model.ID)
	}
	return models
}

func mergeModelNames(modelLists ...[]string) []string {
	total := 0
	for _, list := range modelLists {
		total += len(list)
	}
	seen := make(map[string]struct{}, total)
	models := make([]string, 0, total)
	for _, list := range modelLists {
		for _, model := range list {
			id := strings.TrimSpace(model)
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			models = append(models, id)
		}
	}
	sort.Strings(models)
	return models
}
