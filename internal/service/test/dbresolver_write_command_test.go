package test

import (
	"context"
	"database/sql"
	"path/filepath"
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
	heldReaders, releaseReaders := holdResolverServiceTestReaders(t, reader)
	readersHeld := true
	defer func() {
		if readersHeld {
			releaseReaders(heldReaders)
		}
	}()

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
		releaseReaders(heldReaders)
		readersHeld = false
		<-result
		t.Fatal("usage identity alias update waited for an occupied reader")
	}

	releaseReaders(heldReaders)
	readersHeld = false
}

func TestPricingUpdateDoesNotDependOnReaderAvailability(t *testing.T) {
	// 准备：定价 upsert 内部会先查旧记录再 Save，占满 reader 可验证整个写命令的路由。
	db, reader := openResolverServiceTestPools(t)
	heldReaders, releaseReaders := holdResolverServiceTestReaders(t, reader)
	readersHeld := true
	defer func() {
		if readersHeld {
			releaseReaders(heldReaders)
		}
	}()

	// 执行：更新定价时的存在性查询和 Save 都必须使用 writer。
	result := make(chan error, 1)
	go func() {
		_, err := service.NewPricingService(db).UpdatePricing(context.Background(), servicedto.UpdatePricingInput{
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
		releaseReaders(heldReaders)
		readersHeld = false
		<-result
		t.Fatal("pricing update waited for an occupied reader")
	}

	releaseReaders(heldReaders)
	readersHeld = false
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

func holdResolverServiceTestReaders(t *testing.T, reader *gorm.DB) ([]*sql.Conn, func([]*sql.Conn)) {
	t.Helper()
	readerSQL, err := reader.DB()
	if err != nil {
		t.Fatalf("load reader sql db: %v", err)
	}
	heldReaders := make([]*sql.Conn, 0, 4)
	for index := 0; index < 4; index++ {
		connection, err := readerSQL.Conn(context.Background())
		if err != nil {
			for _, held := range heldReaders {
				_ = held.Close()
			}
			t.Fatalf("hold reader connection %d: %v", index, err)
		}
		heldReaders = append(heldReaders, connection)
	}
	release := func(connections []*sql.Conn) {
		for _, connection := range connections {
			if err := connection.Close(); err != nil {
				t.Errorf("release reader connection: %v", err)
			}
		}
	}
	return heldReaders, release
}
