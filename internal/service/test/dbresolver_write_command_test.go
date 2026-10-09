package test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"gorm.io/gorm"
)

func TestUsageIdentityAliasUpdateDoesNotDependOnReaderAvailability(t *testing.T) {
	// 准备：文件库保留唯一 writer，同时占满四条 reader。
	db, reader := openResolverServiceTestPools(t)
	identity := entities.UsageIdentity{
		Name:         "Writer Identity",
		AuthType:     entities.UsageIdentityAuthTypeAuthFile,
		AuthTypeName: "oauth",
		Identity:     "writer-auth-index",
		Type:         "codex",
	}
	if err := db.Create(&identity).Error; err != nil {
		t.Fatalf("seed usage identity: %v", err)
	}
	releaseReaders := holdResolverServiceTestReaders(t, reader)

	// 执行：alias UPDATE 及结果回读必须作为一个写命令固定在 writer。
	result := make(chan error, 1)
	go func() {
		_, err := service.NewUsageIdentityService(db).UpdateUsageIdentityAlias(context.Background(), identity.ID, "Primary")
		result <- err
	}()

	// 断言：即使 reader 全部被其它查询占用，写命令仍应立即完成。
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("update usage identity alias: %v", err)
		}
	case <-time.After(time.Second):
		releaseReaders()
		<-result
		t.Fatal("usage identity alias update waited for an occupied reader")
	}

}

func TestPricingUpdateDoesNotDependOnReaderAvailability(t *testing.T) {
	// 准备：定价 upsert 内部会先查旧记录再 Save，占满 reader 可验证整个写命令的路由。
	db, reader := openResolverServiceTestPools(t)
	releaseReaders := holdResolverServiceTestReaders(t, reader)

	// 执行：更新定价时的存在性查询和 Save 都必须使用 writer。
	result := make(chan error, 1)
	go func() {
		_, err := service.NewPricingService(db, emptyPricingCatalogForTest()).UpdatePricing(context.Background(), servicedto.UpdatePricingInput{
			Model:                "writer-pricing-model",
			PromptPricePer1M:     1,
			CompletionPricePer1M: 2,
		})
		result <- err
	}()

	// 断言：纯查询可以排队等 reader，但写命令不应依赖 reader 是否空闲。
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("update pricing: %v", err)
		}
	case <-time.After(time.Second):
		releaseReaders()
		<-result
		t.Fatal("pricing update waited for an occupied reader")
	}

}

func openResolverServiceTestPools(t *testing.T) (*gorm.DB, *gorm.DB) {
	t.Helper()
	db, reader, err := repository.OpenDatabasePools(config.Config{SQLitePath: filepath.Join(t.TempDir(), "app.db")})
	if err != nil {
		t.Fatalf("OpenDatabasePools returned error: %v", err)
	}
	writerSQL, err := db.DB()
	if err != nil {
		t.Fatalf("load writer sql db: %v", err)
	}
	readerSQL, err := reader.DB()
	if err != nil {
		t.Fatalf("load reader sql db: %v", err)
	}
	t.Cleanup(func() {
		if err := readerSQL.Close(); err != nil {
			t.Errorf("close reader sql db: %v", err)
		}
		if err := writerSQL.Close(); err != nil {
			t.Errorf("close writer sql db: %v", err)
		}
	})
	return db, reader
}

func holdResolverServiceTestReaders(t *testing.T, reader *gorm.DB) func() {
	t.Helper()
	readerSQL, err := reader.DB()
	if err != nil {
		t.Fatalf("load reader sql db: %v", err)
	}
	var releases []func()
	for index := 0; index < readerSQL.Stats().MaxOpenConnections; index++ {
		connection, err := readerSQL.Conn(context.Background())
		if err != nil {
			t.Fatalf("hold reader connection %d: %v", index, err)
		}
		release := sync.OnceFunc(func() {
			if err := connection.Close(); err != nil {
				t.Errorf("release reader connection: %v", err)
			}
		})
		t.Cleanup(release)
		releases = append(releases, release)
	}
	return func() {
		for _, release := range releases {
			release()
		}
	}
}
