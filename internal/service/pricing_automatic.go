package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricingmetadata"
	"cpa-usage-keeper/internal/repository"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/plugin/dbresolver"
)

func automaticPricingExclusionKey(model string) string {
	return "pricing.auto.excluded." + base64.RawURLEncoding.EncodeToString([]byte(model))
}

// AutomaticPricingRunner supplements missing settings using the selected metadata
// source. It shares the manual service's mutation lock and immutable catalog.
// No remote work runs in usage ingestion or in an HTTP request.
type AutomaticPricingRunner struct {
	service   *pricingService
	source    string
	mu        sync.Mutex
	metadata  pricingmetadata.Catalog
	fetchedAt time.Time
	fetch     func(context.Context, string) (pricingmetadata.Catalog, error)
}

func NewAutomaticPricingRunner(provider PricingProvider, source string) (*AutomaticPricingRunner, error) {
	s, ok := provider.(*pricingService)
	if !ok {
		return nil, fmt.Errorf("automatic pricing requires the shared pricing service")
	}
	src, err := pricingmetadata.SourceByID(source)
	if err != nil {
		return nil, err
	}
	return &AutomaticPricingRunner{service: s, source: src.ID, fetch: s.metadataClient.Fetch}, nil
}

func (r *AutomaticPricingRunner) Run(ctx context.Context) error {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		if _, err := r.SyncOnce(ctx); err != nil && ctx.Err() == nil {
			logrus.WithError(err).Error("automatic pricing failed; missing prices remain unavailable and will be retried")
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// SyncOnce backfills hot/archive history and CPA-only models, even without newly
// inserted usage. Remote catalogs are cached for one hour; failures are retried.
func (r *AutomaticPricingRunner) SyncOnce(ctx context.Context) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.service
	models, err := s.effectiveModels(ctx)
	if err != nil {
		return 0, err
	}
	// Resolve billable aliases, never manufacture direct prices for deployment IDs.
	// Include archived models, which can disappear from the hot usage table.
	type observedModel struct {
		Model      string
		ModelAlias string
	}
	var observed []observedModel
	err = s.db.WithContext(ctx).Raw(`SELECT DISTINCT model, COALESCE(model_alias, '') AS model_alias FROM usage_events
 UNION SELECT DISTINCT model, COALESCE(model_alias, '') AS model_alias FROM usage_events_archive`).Scan(&observed).Error
	if err != nil {
		return 0, fmt.Errorf("list historical pricing subjects: %w", err)
	}
	aliases := make(map[string]bool)
	for _, item := range observed {
		alias := strings.TrimSpace(item.ModelAlias)
		if alias != "" && alias != strings.TrimSpace(item.Model) {
			aliases[strings.TrimSpace(item.Model)] = true
			models = append(models, alias)
		} else {
			models = append(models, item.Model)
		}
	}
	candidates := make([]string, 0, len(models))
	configured := make(map[string]bool)
	for _, item := range s.catalog.Snapshot().ModelConfigs() {
		configured[item.Pricing.Model] = true
	}
	for _, model := range mergeModelNames(models) {
		if !aliases[model] && !configured[model] && !strings.EqualFold(model, "unknown") {
			candidates = append(candidates, model)
		}
	}
	if len(candidates) == 0 {
		return 0, nil
	}
	if r.fetchedAt.IsZero() || time.Since(r.fetchedAt) >= time.Hour {
		catalog, fetchErr := r.fetch(ctx, r.source)
		if fetchErr != nil {
			return 0, fetchErr
		}
		r.metadata, r.fetchedAt = catalog, time.Now()
	}
	index := buildPricingCatalogIndex(r.metadata.Entries)
	settings := make([]entities.ModelPriceSetting, 0, len(candidates))
	for _, model := range candidates {
		matches := matchPricingCatalogCandidates(model, index)
		if len(matches) == 0 {
			continue
		}
		candidate := matches[0]
		// Automation is stricter than the user-reviewed preview: exact IDs only,
		// no third-party substitute or inferred family/size/price baseline.
		if candidate.score != 100 || !strings.EqualFold(stripPricingModelPrefix(model), stripPricingModelPrefix(candidate.entry.Model.ID)) || pricingProviderRankForModel(model, candidate.entry.ProviderID) >= 100 || isPlanPricingProvider(candidate.entry.ProviderID) {
			continue
		}
		cost := candidate.entry.Model.Cost
		match, ok := buildPricingSyncMatchFromCandidates(model, matches[:1])
		if !ok {
			continue
		}
		if match.PromptPricePer1M > 0 && cost.CacheRead == nil {
			continue
		}
		if match.PricingStyle == entities.ModelPricingStyleClaude && match.PromptPricePer1M > 0 && (cost.CacheWrite == nil || match.CacheWritePricePer1M == 0 || match.CacheReadPricePer1M == 0) {
			continue
		}
		// Equal-priority entries with conflicting prices are ambiguous; leave unpriced.
		ambiguous := false
		for _, other := range matches[1:] {
			if other.score != candidate.score || pricingProviderRankForModel(model, other.entry.ProviderID) != pricingProviderRankForModel(model, candidate.entry.ProviderID) {
				continue
			}
			alt, valid := buildPricingSyncMatchFromCandidates(model, []pricingSyncCandidate{other})
			if !valid || alt.PricingStyle != match.PricingStyle || alt.PromptPricePer1M != match.PromptPricePer1M || alt.CompletionPricePer1M != match.CompletionPricePer1M || alt.CacheReadPricePer1M != match.CacheReadPricePer1M || alt.CacheWritePricePer1M != match.CacheWritePricePer1M {
				ambiguous = true
				break
			}
		}
		if ambiguous {
			continue
		}
		multiplier := 1.0
		settings = append(settings, entities.ModelPriceSetting{Model: model, PricingStyle: match.PricingStyle, PromptPricePer1M: match.PromptPricePer1M, CompletionPricePer1M: match.CompletionPricePer1M, CacheReadPricePer1M: match.CacheReadPricePer1M, CacheWritePricePer1M: match.CacheWritePricePer1M, PriceMultiplier: &multiplier})
	}
	if len(settings) == 0 {
		return 0, nil
	}
	inserted := 0
	_, err = s.mutatePricing(ctx, func(tx *gorm.DB) error {
		// Recheck exclusions inside the same serialized write transaction as manual
		// updates/deletes. INSERT DO NOTHING also protects against other writers.
		for _, setting := range settings {
			_, excluded, loadErr := repository.GetAppSetting(ctx, tx.Clauses(dbresolver.Write), automaticPricingExclusionKey(setting.Model))
			if loadErr != nil {
				return loadErr
			}
			if excluded {
				continue
			}
			var hasAlias bool
			if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM usage_events WHERE model = ? AND TRIM(COALESCE(model_alias, '')) <> '' AND TRIM(model_alias) <> TRIM(model)
                UNION ALL SELECT 1 FROM usage_events_archive WHERE model = ? AND TRIM(COALESCE(model_alias, '')) <> '' AND TRIM(model_alias) <> TRIM(model))`, setting.Model, setting.Model).Scan(&hasAlias).Error; err != nil {
				return err
			}
			if hasAlias {
				continue
			}
			result := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "model"}}, DoNothing: true}).Create(&setting)
			if result.Error != nil {
				return result.Error
			}
			inserted += int(result.RowsAffected)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	logrus.WithFields(logrus.Fields{"inserted": inserted, "source": r.source, "unresolved": len(candidates) - inserted}).Debug("automatic pricing discovery completed")
	return inserted, nil
}
