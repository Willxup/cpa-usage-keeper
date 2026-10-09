package service

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	cpamodels "cpa-usage-keeper/internal/cpa/dto/models"
	"cpa-usage-keeper/internal/cpa/dto/response"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/helper"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/pricingmetadata"
	"cpa-usage-keeper/internal/repository"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"gorm.io/gorm"
)

type autoModelsFetcher []string

func (m autoModelsFetcher) FetchModels(context.Context) (*response.ModelsResult, error) {
	data := make([]cpamodels.ModelInfo, 0, len(m))
	for _, id := range m {
		data = append(data, cpamodels.ModelInfo{ID: id})
	}
	return &response.ModelsResult{Payload: cpamodels.ModelsResponse{Data: data}}, nil
}
func autoFixture(t *testing.T, models ...string) (*gorm.DB, PricingProvider, *AutomaticPricingRunner, *pricing.Catalog) {
	t.Helper()
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "auto.db")})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	snapshot, err := repository.LoadPricingSnapshot(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	catalog := pricing.NewCatalog(snapshot)
	provider := NewPricingService(db, catalog, autoModelsFetcher(models))
	runner, err := NewAutomaticPricingRunner(provider, "models-dev")
	if err != nil {
		t.Fatal(err)
	}
	return db, provider, runner, catalog
}
func autoPrice(id, provider string, input, output, read, write float64) pricingmetadata.Entry {
	return pricingmetadata.Entry{ProviderID: provider, Model: pricingmetadata.Model{ID: id, Cost: pricingmetadata.Cost{Input: &input, Output: &output, CacheRead: &read, CacheWrite: &write}}}
}
func autoCatalog(r *AutomaticPricingRunner, entries ...pricingmetadata.Entry) {
	r.fetch = func(context.Context, string) (pricingmetadata.Catalog, error) {
		return pricingmetadata.Catalog{Entries: entries}, nil
	}
}
func autoSync(t *testing.T, r *AutomaticPricingRunner, want int) {
	t.Helper()
	n, err := r.SyncOnce(context.Background())
	if err != nil || n != want {
		t.Fatalf("SyncOnce = %d, %v; want %d", n, err, want)
	}
}

func TestAutomaticPricingExactMetadataTiersAndClaudeCosts(t *testing.T) {
	_, provider, r, catalog := autoFixture(t, "o1-mini", "o3-mini", "mistral-medium-2505", "grok-4-fast-reasoning", "grok-4-fast-non-reasoning", "claude-sonnet-4-5", "unlisted-mini", "unknown", "o1-super-mini")
	autoCatalog(r, autoPrice("o1-mini", "openai", 3, 12, 1.5, 0), autoPrice("o3-mini", "openai", 1.1, 4.4, .55, 0), autoPrice("mistral-medium-2505", "mistral", .4, 2, 0, 0), autoPrice("grok-4-fast-reasoning", "xai", .2, .5, .05, 0), autoPrice("grok-4-fast-non-reasoning", "xai", .2, .5, .05, 0), autoPrice("claude-sonnet-4-5", "anthropic", 3, 15, .3, 3.75))
	autoSync(t, r, 6)
	prices, err := provider.ListPricing(context.Background())
	if err != nil || len(prices) != 6 {
		t.Fatalf("prices %v, %v", prices, err)
	}
	for _, p := range prices {
		if p.Model == "o1-mini" && (p.PromptPricePer1M != 3 || p.CompletionPricePer1M != 12 || p.CacheReadPricePer1M != 1.5) {
			t.Fatalf("wrong o1 tier: %+v", p)
		}
	}
	result := catalog.NewResolver().Calculate(pricing.NewCostSubject(pricing.UsageDimensions{Model: "claude-sonnet-4-5"}, helper.UsageTokenCostInput{InputTokens: 3_000_000, OutputTokens: 1_000_000, CacheReadTokens: 1_000_000, CacheCreationTokens: 1_000_000}))
	if !result.Available || math.Abs(result.Cost.TotalCostUSD-22.05) > 1e-9 || result.Cost.CacheWriteCostUSD != 3.75 || result.Cost.CacheReadCostUSD != .3 {
		t.Fatalf("wrong cache costs: %+v", result)
	}
	autoSync(t, r, 0)
}
func TestAutomaticPricingRejectsIncompleteAmbiguousAndNameOnlyMetadata(t *testing.T) {
	_, _, r, c := autoFixture(t, "claude-missing-write", "claude-missing-read", "gpt-missing-read", "gpt-name-only", "gpt-conflict", "gpt-third-party", "glm-plan", "gpt-nan")
	write := autoPrice("claude-missing-write", "anthropic", 3, 15, .3, 0)
	write.Model.Cost.CacheWrite = nil
	read := autoPrice("claude-missing-read", "anthropic", 3, 15, 0, 3.75)
	read.Model.Cost.CacheRead = nil
	gpt := autoPrice("gpt-missing-read", "openai", 1, 2, 0, 0)
	gpt.Model.Cost.CacheRead = nil
	name := autoPrice("different-id", "openai", 1, 2, 0, 0)
	name.Model.Name = "gpt-name-only"
	autoCatalog(r, write, read, gpt, name, autoPrice("gpt-conflict", "openai", 1, 2, .1, 0), autoPrice("ns/gpt-conflict", "openai", 9, 9, 9, 0), autoPrice("gpt-third-party", "openrouter", 1, 2, .1, 0), autoPrice("glm-plan", "zai-coding-plan", 0, 0, 0, 0), autoPrice("gpt-nan", "openai", math.NaN(), 2, .1, 0))
	autoSync(t, r, 0)
	if len(c.Snapshot().ModelConfigs()) != 0 {
		t.Fatal("rejected model was priced")
	}
}
func TestAutomaticPricingBackfillsHistoricalAliasesArchiveAndCPAModels(t *testing.T) {
	db, _, r, c := autoFixture(t, "grok-4-fast-reasoning", "claude-sonnet-4-5")
	alias := "claude-sonnet-4-5"
	if err := db.Create(&entities.UsageEvent{Model: "deployment", ModelAlias: &alias, Timestamp: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entities.UsageEventArchive{ID: 100, Model: "o1-mini", Timestamp: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	autoCatalog(r, autoPrice(alias, "anthropic", 3, 15, .3, 3.75), autoPrice("deployment", "openai", 99, 99, 99, 99), autoPrice("o1-mini", "openai", 3, 12, 1.5, 0), autoPrice("grok-4-fast-reasoning", "xai", .2, .5, .05, 0))
	autoSync(t, r, 3)
	result := c.NewResolver().Calculate(pricing.NewCostSubject(pricing.UsageDimensions{Model: "deployment", ModelAlias: alias}, helper.UsageTokenCostInput{InputTokens: 1_000_000}))
	if !result.Available || result.MatchedBy != "model_alias" || result.Cost.TotalCostUSD != 3 {
		t.Fatalf("alias overridden: %+v", result)
	}
}
func TestAutomaticPricingConcurrentManualSaveAndLateAlias(t *testing.T) {
	for _, lateAlias := range []bool{false, true} {
		t.Run(map[bool]string{false: "manual", true: "alias"}[lateAlias], func(t *testing.T) {
			db, p, r, c := autoFixture(t, "gpt-test")
			started, release := make(chan struct{}), make(chan struct{})
			r.fetch = func(context.Context, string) (pricingmetadata.Catalog, error) {
				close(started)
				<-release
				return pricingmetadata.Catalog{Entries: []pricingmetadata.Entry{autoPrice("gpt-test", "openai", 1, 2, .1, 0)}}, nil
			}
			var wg sync.WaitGroup
			wg.Add(1)
			var n int
			var err error
			go func() { defer wg.Done(); n, err = r.SyncOnce(context.Background()) }()
			<-started
			if lateAlias {
				alias := "o1-mini"
				if err := db.Create(&entities.UsageEvent{Model: "gpt-test", ModelAlias: &alias, Timestamp: time.Now()}).Error; err != nil {
					t.Fatal(err)
				}
			} else {
				_, err := p.UpdatePricing(context.Background(), servicedto.UpdatePricingInput{Model: "gpt-test", PromptPricePer1M: 77})
				if err != nil {
					t.Fatal(err)
				}
			}
			close(release)
			wg.Wait()
			if err != nil || n != 0 {
				t.Fatalf("concurrent automatic insertion: %d, %v", n, err)
			}
			if !lateAlias {
				result := c.NewResolver().Calculate(pricing.NewCostSubject(pricing.UsageDimensions{Model: "gpt-test"}, helper.UsageTokenCostInput{InputTokens: 1_000_000}))
				if result.Cost.TotalCostUSD != 77 {
					t.Fatalf("manual overwritten: %+v", result)
				}
			}
		})
	}
}
func TestAutomaticPricingDeletionSurvivesRestartAndManualSaveClearsExclusion(t *testing.T) {
	_, p, r, c := autoFixture(t, "gpt-test")
	entry := autoPrice("gpt-test", "openai", 1, 2, .1, 0)
	autoCatalog(r, entry)
	autoSync(t, r, 1)
	if err := p.DeletePricing(context.Background(), "gpt-test"); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewAutomaticPricingRunner(p, "models-dev")
	if err != nil {
		t.Fatal(err)
	}
	autoCatalog(restarted, entry)
	autoSync(t, restarted, 0)
	if len(c.Snapshot().ModelConfigs()) != 0 {
		t.Fatal("deleted price restored")
	}
	if _, err := p.UpdatePricing(context.Background(), servicedto.UpdatePricingInput{Model: "gpt-test", PromptPricePer1M: 9}); err != nil {
		t.Fatal(err)
	}
	_, found, err := repository.GetAppSetting(context.Background(), r.service.db, automaticPricingExclusionKey("gpt-test"))
	if err != nil || found {
		t.Fatalf("exclusion remains: %v, %v", found, err)
	}
	autoSync(t, restarted, 0)
}
func TestAutomaticPricingFailuresRollbackAndRetry(t *testing.T) {
	db, _, r, c := autoFixture(t, "gpt-a", "gpt-b")
	calls := 0
	r.fetch = func(context.Context, string) (pricingmetadata.Catalog, error) {
		calls++
		if calls == 1 {
			return pricingmetadata.Catalog{}, errors.New("offline")
		}
		return pricingmetadata.Catalog{Entries: []pricingmetadata.Entry{autoPrice("gpt-a", "openai", 1, 2, .1, 0), autoPrice("gpt-b", "openai", 1, 2, .1, 0)}}, nil
	}
	if n, err := r.SyncOnce(context.Background()); n != 0 || err == nil {
		t.Fatal("download error swallowed")
	}
	if err := db.Exec(`CREATE TRIGGER reject_auto_price BEFORE INSERT ON model_price_settings WHEN NEW.model='gpt-b' BEGIN SELECT RAISE(ABORT, 'read only test'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if n, err := r.SyncOnce(context.Background()); n != 0 || err == nil {
		t.Fatal("write failure swallowed")
	}
	var count int64
	db.Model(&entities.ModelPriceSetting{}).Count(&count)
	if count != 0 || len(c.Snapshot().ModelConfigs()) != 0 {
		t.Fatal("partial write/catalog leaked")
	}
	if err := db.Exec("DROP TRIGGER reject_auto_price").Error; err != nil {
		t.Fatal(err)
	}
	autoSync(t, r, 2)
	if calls != 2 {
		t.Fatalf("catalog not cached: %d", calls)
	}
}
func TestAutomaticPricingCancelledRunnerStops(t *testing.T) {
	_, p, _, _ := autoFixture(t)
	r, err := NewAutomaticPricingRunner(p, "litellm")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := r.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := NewAutomaticPricingRunner(p, "invalid"); err == nil {
		t.Fatal("invalid source accepted")
	}
}

func TestAutomaticPricingRefreshesCatalogWithoutRepricingExistingSettings(t *testing.T) {
	_, p, r, _ := autoFixture(t, "gpt-a")
	calls := 0
	r.fetch = func(context.Context, string) (pricingmetadata.Catalog, error) {
		calls++
		return pricingmetadata.Catalog{Entries: []pricingmetadata.Entry{autoPrice("gpt-a", "openai", float64(calls), 2, .1, 0), autoPrice("gpt-b", "openai", 4, 8, .4, 0)}}, nil
	}
	autoSync(t, r, 1)
	r.service.modelsFetcher = autoModelsFetcher{"gpt-a", "gpt-b"}
	r.fetchedAt = time.Now().Add(-2 * time.Hour)
	autoSync(t, r, 1)
	prices, err := p.ListPricing(context.Background())
	if err != nil || calls != 2 || len(prices) != 2 || prices[0].PromptPricePer1M != 1 || prices[1].PromptPricePer1M != 4 {
		t.Fatalf("refresh overwrote existing price: %+v, %v, calls=%d", prices, err, calls)
	}
}

func TestAutomaticPricingMultipleDiscoveriesNeverReplaceManualMultiplier(t *testing.T) {
	_, p, r, _ := autoFixture(t, "gpt-test")
	other, err := NewAutomaticPricingRunner(p, "litellm")
	if err != nil {
		t.Fatal(err)
	}
	entry := autoPrice("gpt-test", "openai", 1, 2, .1, 0)
	autoCatalog(r, entry)
	autoCatalog(other, entry)
	multiplier := 2.5
	start := make(chan struct{})
	errs := make(chan error, 3)
	for _, runner := range []*AutomaticPricingRunner{r, other} {
		go func(runner *AutomaticPricingRunner) {
			<-start
			_, err := runner.SyncOnce(context.Background())
			errs <- err
		}(runner)
	}
	go func() {
		<-start
		_, err := p.UpdatePricing(context.Background(), servicedto.UpdatePricingInput{Model: "gpt-test", PromptPricePer1M: 77, PriceMultiplier: &multiplier})
		errs <- err
	}()
	close(start)
	for i := 0; i < 3; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	prices, err := p.ListPricing(context.Background())
	if err != nil || len(prices) != 1 || prices[0].PromptPricePer1M != 77 || *prices[0].PriceMultiplier != 2.5 {
		t.Fatalf("manual price/multiplier replaced: %+v, %v", prices, err)
	}
}
