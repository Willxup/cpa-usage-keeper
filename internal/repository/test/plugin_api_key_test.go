package test

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/helper"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

func pluginKeyIdentities(count int) []entities.PluginAPIKeyIdentity {
	identities := make([]entities.PluginAPIKeyIdentity, 0, count)
	for i := 0; i < count; i++ {
		identities = append(identities, entities.PluginAPIKeyIdentity{APIGroupKey: helper.PluginAPIKeyGroupKey("plugin:test:auth", fmt.Sprintf("team-%d", i)), AccessProvider: "plugin:test:auth"})
	}
	return identities
}

func TestPluginAPIKeyRegistrationPreservesAliasesAndNativeSyncIsolation(t *testing.T) {
	db := openTestDatabase(t)
	identities := pluginKeyIdentities(2)
	result, err := repository.RegisterPluginAPIKeys(context.Background(), db, identities)
	if err != nil || result.Inserted != 2 || len(result.Remaining) != 0 {
		t.Fatalf("initial registration: %+v %v", result, err)
	}
	var plugin entities.CPAAPIKey
	if err := db.Where("api_key = ?", identities[0].APIGroupKey).Take(&plugin).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.UpdateCPAAPIKeyAlias(db, plugin.ID, "Team alias"); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.FindActiveCPAAPIKeyByValue(db, plugin.APIKey); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("plugin identity became a native credential: %v", err)
	}
	for _, keys := range [][]string{{"native-key"}, nil, {plugin.APIKey, "native-key"}} {
		if err := repository.SyncCPAAPIKeys(db, keys, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	result, err = repository.RegisterPluginAPIKeys(context.Background(), db, identities)
	if err != nil || result.Inserted != 0 {
		t.Fatalf("duplicate registration: %+v %v", result, err)
	}
	var stored entities.CPAAPIKey
	if err := db.First(&stored, plugin.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.SourceType != entities.CPAAPIKeySourcePlugin || stored.AccessProvider != identities[0].AccessProvider || stored.KeyAlias != "Team alias" || stored.IsDeleted || stored.LastSyncedAt != nil {
		t.Fatalf("metadata sync changed plugin record: %+v", stored)
	}
	conflict := pluginKeyIdentities(3)[2]
	if err := repository.SyncCPAAPIKeys(db, []string{conflict.APIGroupKey}, time.Now()); err != nil {
		t.Fatal(err)
	}
	result, err = repository.RegisterPluginAPIKeys(context.Background(), db, append(identities, conflict, pluginKeyIdentities(4)[3]))
	if err != nil || result.Conflicts != 1 || result.Inserted != 1 || len(result.Remaining) != 0 {
		t.Fatalf("source conflict: %+v %v", result, err)
	}
	var native entities.CPAAPIKey
	if err := db.Where("api_key = ?", conflict.APIGroupKey).Take(&native).Error; err != nil || native.SourceType != entities.CPAAPIKeySourceNative {
		t.Fatalf("native source overwritten: %+v %v", native, err)
	}
}

func TestPluginAPIKeyExistingPrecheckUsesReaderWithoutAcquiringWriter(t *testing.T) {
	db, writer, _ := openTestDatabasePools(t, "plugin-precheck.db")
	identities := pluginKeyIdentities(2)
	if _, err := repository.RegisterPluginAPIKeys(context.Background(), db, identities); err != nil {
		t.Fatal(err)
	}
	// 独占唯一 writer，重复通知仍应仅用 reader 完成，不能等待写事务。
	conn, err := writer.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	result, err := repository.RegisterPluginAPIKeys(ctx, db, identities)
	if err != nil || result.Inserted != 0 || len(result.Remaining) != 0 {
		t.Fatalf("existing keys acquired writer: %+v %v", result, err)
	}
}

func TestPluginAPIKeyRegistrationYieldsBeforeEveryWriteAndRetainsFailedBatch(t *testing.T) {
	db := openTestDatabase(t)
	identities := pluginKeyIdentities(1000)
	var injected atomic.Bool
	if err := db.Callback().Create().After("gorm:create").Register("test:enqueue-inbox-after-plugin-batch", func(tx *gorm.DB) {
		if tx.Statement.Table == "cpa_api_keys" && injected.CompareAndSwap(false, true) {
			inbox := entities.RedisUsageInbox{Source: "test", RawMessage: "{}", Status: repository.RedisUsageInboxStatusPending, PoppedAt: time.Now()}
			if err := tx.Session(&gorm.Session{NewDB: true}).Create(&inbox).Error; err != nil {
				tx.AddError(err)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	result, err := repository.RegisterPluginAPIKeys(context.Background(), db, identities)
	if err != nil || !result.DeferredForInbox || result.Inserted <= 0 || result.Inserted >= 1000 || int64(len(result.Remaining))+result.Inserted != 1000 {
		t.Fatalf("did not yield between short writes: inserted=%d remaining=%d deferred=%t err=%v", result.Inserted, len(result.Remaining), result.DeferredForInbox, err)
	}
	if err := db.Model(&entities.RedisUsageInbox{}).Where("status = ?", repository.RedisUsageInboxStatusPending).Update("status", repository.RedisUsageInboxStatusProcessed).Error; err != nil {
		t.Fatal(err)
	}
	var failOnce atomic.Bool
	if err := db.Callback().Create().Before("gorm:create").Register("test:fail-plugin-batch", func(tx *gorm.DB) {
		if tx.Statement.Table == "cpa_api_keys" && failOnce.CompareAndSwap(false, true) {
			tx.AddError(errors.New("injected plugin insert failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	retry, err := repository.RegisterPluginAPIKeys(context.Background(), db, result.Remaining)
	if err == nil || len(retry.Remaining) != len(result.Remaining) || retry.Inserted != 0 {
		t.Fatalf("failed batch lost identities: %+v %v", retry, err)
	}
	retry, err = repository.RegisterPluginAPIKeys(context.Background(), db, retry.Remaining)
	if err != nil || len(retry.Remaining) != 0 {
		t.Fatalf("retry failed: %+v %v", retry, err)
	}
	var count int64
	if err := db.Model(&entities.CPAAPIKey{}).Count(&count).Error; err != nil || count != 1000 {
		t.Fatalf("wrong final key count %d: %v", count, err)
	}
}

func TestPluginAPIKeyInboxCheckFailureCannotWrite(t *testing.T) {
	db := openTestDatabase(t)
	if err := db.Callback().Query().Before("gorm:query").Register("test:fail-plugin-inbox-check", func(tx *gorm.DB) {
		if tx.Statement.Table == "redis_usage_inboxes" {
			tx.AddError(errors.New("injected inbox check failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	// HasProcessableRedisUsageInbox 的 Scan 使用 Row 回调，覆盖这条真实查询路径。
	if err := db.Callback().Row().Before("gorm:row").Register("test:fail-plugin-inbox-scan", func(tx *gorm.DB) {
		if tx.Statement.Table == "redis_usage_inboxes" {
			tx.AddError(errors.New("injected inbox check failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	result, err := repository.RegisterPluginAPIKeys(context.Background(), db, pluginKeyIdentities(2))
	if err == nil || len(result.Remaining) != 2 {
		t.Fatalf("inbox failure allowed write: %+v %v", result, err)
	}
	var count int64
	if err := db.Model(&entities.CPAAPIKey{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("inbox failure wrote keys: %d %v", count, err)
	}
}
