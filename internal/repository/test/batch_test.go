package test

import (
	_ "cpa-usage-keeper/internal/repository"
	"reflect"
	"sync"
	"testing"
	_ "unsafe"

	"cpa-usage-keeper/internal/entities"
)

func TestInsertBatchSizeUsesModelColumnCount(t *testing.T) {
	usageIdentityColumnCount := insertBatchColumnCount(entities.UsageIdentity{})
	usageIdentityBatchSize := insertBatchSize(entities.UsageIdentity{})
	if usageIdentityBatchSize >= 900 {
		t.Fatalf("expected wide usage identity model to reduce batch below %d, got %d", 900, usageIdentityBatchSize)
	}
	if usageIdentityBatchSize != 999/usageIdentityColumnCount {
		t.Fatalf("expected usage identity batch size to use %d insert columns, got %d", usageIdentityColumnCount, usageIdentityBatchSize)
	}

	narrowBatchSize := insertBatchSize(narrowInsertBatchModel{})
	if narrowBatchSize != 900 {
		t.Fatalf("expected narrow model to keep max batch size %d, got %d", 900, narrowBatchSize)
	}
}

type narrowInsertBatchModel struct {
	Name string
}

func TestInsertBatchSizeCachesModelColumnCount(t *testing.T) {
	insertBatchSize(entities.UsageIdentity{})
	if _, ok := insertBatchColumnCountCache.Load(reflect.TypeFor[entities.UsageIdentity]()); !ok {
		t.Fatal("expected insert batch size to cache the UsageIdentity column count")
	}
}

// 保留模型列数与缓存命中白盒覆盖；业务批量上限使用固定期望值。
//
//go:linkname insertBatchColumnCount cpa-usage-keeper/internal/repository.insertBatchColumnCount
func insertBatchColumnCount(model any) int

//go:linkname insertBatchSize cpa-usage-keeper/internal/repository.insertBatchSize
func insertBatchSize(model any) int

//go:linkname insertBatchColumnCountCache cpa-usage-keeper/internal/repository.insertBatchColumnCountCache
var insertBatchColumnCountCache sync.Map
