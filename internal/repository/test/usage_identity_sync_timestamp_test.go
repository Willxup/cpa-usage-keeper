package test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
)

// timestampReplaceCase 描述两个公开 Replace 入口共用的时间合同测试参数。
type timestampReplaceCase struct {
	// name 区分 Auth File 与 AI Provider 子测试。
	name string
	// authType 决定 identity 的持久化范围。
	authType entities.UsageIdentityAuthType
	// authTypeName 保留对应 usage event 的认证类型名称。
	authTypeName string
	// identityType 让 provider replace 可以按成功来源精确 stale。
	identityType string
	// providerTypes 只在 AI Provider replace 中声明本轮成功来源。
	providerTypes []string
}

// TestUsageIdentitySyncTimestampContract 验证两类 metadata replace 的全部时间路径使用同一轮 now。
func TestUsageIdentitySyncTimestampContract(t *testing.T) {
	// cases 对两个公开入口执行完全相同的新建、刷新、恢复与 stale 断言。
	cases := []timestampReplaceCase{
		// Auth File replace 以完整 auth type 范围计算 stale。
		{name: "auth_file", authType: entities.UsageIdentityAuthTypeAuthFile, authTypeName: "oauth", identityType: "codex"},
		// AI Provider replace 只在本轮成功的 codex provider type 内计算 stale。
		{name: "ai_provider", authType: entities.UsageIdentityAuthTypeAIProvider, authTypeName: "apikey", identityType: "codex", providerTypes: []string{"codex"}},
	}
	// 每个入口使用独立数据库，避免行状态在子测试间互相影响。
	for _, testCase := range cases {
		// 固定当前 case，确保子测试闭包读取对应范围。
		testCase := testCase
		// 子测试名称直接表明正在验证的 metadata 类型。
		t.Run(testCase.name, func(t *testing.T) {
			// db 使用真实 repository migration 与 SQLite serializer。
			db := openUsageIdentityTimestampDatabase(t, testCase.name+".db")
			// ctx 与生产 replace 一样贯穿事务。
			ctx := context.Background()
			// createdAt 代表既有行最初创建时间，刷新和恢复必须保留。
			createdAt := time.Date(2026, 7, 10, 9, 0, 0, 0, time.UTC)
			// oldUpdatedAt 代表本轮同步前的 metadata 更新时间。
			oldUpdatedAt := time.Date(2026, 7, 11, 9, 0, 0, 0, time.UTC)
			// deletedAt 代表待恢复行和 already-deleted 行的历史删除时间。
			deletedAt := time.Date(2026, 7, 12, 9, 0, 0, 0, time.UTC)
			// alreadyDeletedUpdatedAt 用独立值证明未再次出现的 deleted row 不被触碰。
			alreadyDeletedUpdatedAt := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
			// nowInput 带不同 offset，证明入口先规范化为项目存储时区再统一写入。
			nowInput := time.Date(2026, 7, 15, 2, 30, 0, 0, time.FixedZone("source", -7*60*60))
			// wantNow 使用生产 timeutil 计算期望 storage instant 与 location。
			wantNow := timeutil.NormalizeStorageTime(nowInput)
			// alias 是 Keeper-only 字段，metadata refresh 不能覆盖。
			alias := "Local Alias"
			// firstUsedAt 是统计历史的一部分，必须在 metadata refresh 后保留。
			firstUsedAt := createdAt.Add(time.Hour)
			// lastUsedAt 是统计历史的一部分，必须在 metadata refresh 后保留。
			lastUsedAt := createdAt.Add(2 * time.Hour)
			// statsUpdatedAt 单独记录统计更新时间，不能被 metadata now 替换。
			statsUpdatedAt := createdAt.Add(3 * time.Hour)
			// seed 包含刷新、恢复、stale 和 already-deleted 四种既有状态。
			seed := []entities.UsageIdentity{
				// refreshed 会以新 metadata 命中，但必须保留本地 alias 与全部统计字段。
				{
					// Name 在本轮会刷新，用于证明 metadata update 确实发生。
					Name: "Old Refreshed",
					// Alias 只由 Keeper 管理，不能被 CPA 输入清空。
					Alias: &alias,
					// AuthType 把行限制在当前 Replace 入口的同步范围。
					AuthType: testCase.authType,
					// AuthTypeName 保留该范围的 oauth/apikey 文案。
					AuthTypeName: testCase.authTypeName,
					// Identity 是 metadata replace 的稳定匹配键。
					Identity: "refresh-auth-index",
					// Type 让 provider scope 精确包含该行。
					Type: testCase.identityType,
					// Provider 是可刷新的上游展示 metadata。
					Provider: "Old Provider",
					// TotalRequests 验证请求统计不因 metadata refresh 清零。
					TotalRequests: 11,
					// SuccessCount 验证成功统计保留。
					SuccessCount: 8,
					// FailureCount 验证失败统计保留。
					FailureCount: 3,
					// InputTokens 验证输入 token 统计保留。
					InputTokens: 101,
					// OutputTokens 验证输出 token 统计保留。
					OutputTokens: 102,
					// ReasoningTokens 验证推理 token 统计保留。
					ReasoningTokens: 103,
					// CachedTokens 验证缓存 token 统计保留。
					CachedTokens: 104,
					// CacheReadTokens 验证规范缓存读取统计保留。
					CacheReadTokens: 105,
					// TotalTokens 验证总 token 统计保留。
					TotalTokens: 515,
					// LastAggregatedUsageEventID 是历史补算游标，metadata refresh 不能回退。
					LastAggregatedUsageEventID: 99,
					// FirstUsedAt 保留首次使用时间。
					FirstUsedAt: &firstUsedAt,
					// LastUsedAt 保留末次使用时间。
					LastUsedAt: &lastUsedAt,
					// StatsUpdatedAt 保留统计专属更新时间。
					StatsUpdatedAt: &statsUpdatedAt,
					// CreatedAt 是刷新后必须保持不变的原创建时间。
					CreatedAt: createdAt,
					// UpdatedAt 是本轮应被 wantNow 替换的旧 metadata 时间。
					UpdatedAt: oldUpdatedAt,
				},
				// restored 会再次出现在输入中，因此必须清除 deleted 状态并刷新 updated_at。
				{Name: "Old Restored", AuthType: testCase.authType, AuthTypeName: testCase.authTypeName, Identity: "restore-auth-index", Type: testCase.identityType, Provider: "Old Provider", IsDeleted: true, CreatedAt: createdAt, UpdatedAt: oldUpdatedAt, DeletedAt: &deletedAt},
				// stale 本轮不再出现，必须同时写 deleted_at 与 updated_at。
				{Name: "Old Stale", AuthType: testCase.authType, AuthTypeName: testCase.authTypeName, Identity: "stale-auth-index", Type: testCase.identityType, Provider: "Old Provider", CreatedAt: createdAt, UpdatedAt: oldUpdatedAt},
				// already-deleted 本轮仍不出现，历史 deleted_at 与 updated_at 必须原样保留。
				{Name: "Already Deleted", AuthType: testCase.authType, AuthTypeName: testCase.authTypeName, Identity: "already-deleted-auth-index", Type: testCase.identityType, Provider: "Old Provider", IsDeleted: true, CreatedAt: createdAt, UpdatedAt: alreadyDeletedUpdatedAt, DeletedAt: &deletedAt},
			}
			// 先写入完整既有状态，后续只通过公开 Replace 入口改变数据。
			if err := db.Create(&seed).Error; err != nil {
				// seed 失败代表测试环境错误，不应误判为时间合同红灯。
				t.Fatalf("seed usage identities: %v", err)
			}
			// incoming 同时覆盖刷新、恢复与新建，遗漏的 active 行进入 stale 路径。
			incoming := []entities.UsageIdentity{
				// refresh 输入只提供上游 metadata，不携带本地 alias 与统计。
				{Name: "New Refreshed", AuthTypeName: testCase.authTypeName, Identity: "refresh-auth-index", Type: testCase.identityType, Provider: "New Provider"},
				// restore 输入命中历史 deleted identity。
				{Name: "New Restored", AuthTypeName: testCase.authTypeName, Identity: "restore-auth-index", Type: testCase.identityType, Provider: "New Provider"},
				// fresh 输入必须获得本轮统一的 created_at 与 updated_at。
				{Name: "Fresh", AuthTypeName: testCase.authTypeName, Identity: "fresh-auth-index", Type: testCase.identityType, Provider: "New Provider"},
			}
			// 通过当前 case 对应的公开入口执行一轮 metadata replace。
			replaceUsageIdentityTimestampScope(t, ctx, db, testCase, incoming, nowInput)
			// rows 读取当前 auth type 的全部状态，包括自定义 deleted 行。
			rows := loadUsageIdentityTimestampRows(t, db, testCase.authType)
			// fresh 必须由调用方 now 明确控制两个时间字段。
			assertUsageIdentityTime(t, rows["fresh-auth-index"].CreatedAt, wantNow, "fresh created_at")
			// fresh updated_at 不能依赖 GORM 当前系统时间。
			assertUsageIdentityTime(t, rows["fresh-auth-index"].UpdatedAt, wantNow, "fresh updated_at")
			// refreshed 保留最初 created_at。
			assertUsageIdentityTime(t, rows["refresh-auth-index"].CreatedAt, createdAt, "refreshed created_at")
			// refreshed 显式采用本轮统一 now，不能再写输入实体的零值。
			assertUsageIdentityTime(t, rows["refresh-auth-index"].UpdatedAt, wantNow, "refreshed updated_at")
			// refreshed metadata 必须正常更新，证明时间修复没有跳过业务写入。
			if rows["refresh-auth-index"].Name != "New Refreshed" || rows["refresh-auth-index"].Provider != "New Provider" {
				// 输出整行帮助定位 metadata 与时间更新是否分离。
				t.Fatalf("refreshed metadata = %+v", rows["refresh-auth-index"])
			}
			// refreshed 的 Keeper-only alias 与全部统计字段必须保持旧值。
			assertUsageIdentityStatsPreserved(t, rows["refresh-auth-index"], alias, firstUsedAt, lastUsedAt, statsUpdatedAt)
			// restored 仍保留历史 created_at。
			assertUsageIdentityTime(t, rows["restore-auth-index"].CreatedAt, createdAt, "restored created_at")
			// restored 使用本轮 now 刷新 metadata 时间。
			assertUsageIdentityTime(t, rows["restore-auth-index"].UpdatedAt, wantNow, "restored updated_at")
			// restored 必须恢复 active 并清除历史 deleted_at。
			if rows["restore-auth-index"].IsDeleted || rows["restore-auth-index"].DeletedAt != nil {
				// 恢复状态错误时输出完整行。
				t.Fatalf("restored identity = %+v", rows["restore-auth-index"])
			}
			// stale 必须记录本轮统一 deleted_at。
			assertUsageIdentityTimePointer(t, rows["stale-auth-index"].DeletedAt, wantNow, "stale deleted_at")
			// stale updated_at 必须与 deleted_at 使用完全相同的一轮 now。
			assertUsageIdentityTime(t, rows["stale-auth-index"].UpdatedAt, wantNow, "stale updated_at")
			// stale 状态必须被置为 deleted。
			if !rows["stale-auth-index"].IsDeleted {
				// active 表示 stale 计算没有生效。
				t.Fatalf("stale identity remained active: %+v", rows["stale-auth-index"])
			}
			// already-deleted 保持最初删除时间，不被本轮重复 stale。
			assertUsageIdentityTimePointer(t, rows["already-deleted-auth-index"].DeletedAt, deletedAt, "already-deleted deleted_at")
			// already-deleted 保持删除时的 updated_at。
			assertUsageIdentityTime(t, rows["already-deleted-auth-index"].UpdatedAt, alreadyDeletedUpdatedAt, "already-deleted updated_at")
		})
	}
}

// TestUsageIdentitySyncTimestampRejectsZeroNow 验证两个 Replace 入口都拒绝零时间且事务外不产生修改。
func TestUsageIdentitySyncTimestampRejectsZeroNow(t *testing.T) {
	// cases 复用两个公开 Replace 范围。
	cases := []timestampReplaceCase{
		// Auth File 零时间不能继续传播到全范围替换。
		{name: "auth_file", authType: entities.UsageIdentityAuthTypeAuthFile, authTypeName: "oauth", identityType: "codex"},
		// AI Provider 零时间不能继续传播到 provider scope 替换。
		{name: "ai_provider", authType: entities.UsageIdentityAuthTypeAIProvider, authTypeName: "apikey", identityType: "codex", providerTypes: []string{"codex"}},
	}
	// 每个入口独立验证错误与数据库不变性。
	for _, testCase := range cases {
		// 固定当前 case 供子测试闭包使用。
		testCase := testCase
		// 子测试名称区分具体公开入口。
		t.Run(testCase.name, func(t *testing.T) {
			// db 使用真实 SQLite 事务证明零时间在任何写入前被拒绝。
			db := openUsageIdentityTimestampDatabase(t, "zero-"+testCase.name+".db")
			// ctx 传递给公开 Replace 入口。
			ctx := context.Background()
			// originalUpdatedAt 用于证明既有行没有被刷新或 stale。
			originalUpdatedAt := time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC)
			// existing 是零时间调用前唯一一行。
			existing := entities.UsageIdentity{Name: "Before", AuthType: testCase.authType, AuthTypeName: testCase.authTypeName, Identity: "existing-auth-index", Type: testCase.identityType, Provider: "Before", CreatedAt: originalUpdatedAt, UpdatedAt: originalUpdatedAt}
			// 写入基线行供错误后比对。
			if err := db.Create(&existing).Error; err != nil {
				// seed 失败不属于待测合同。
				t.Fatalf("seed zero-time identity: %v", err)
			}
			// incoming 同时尝试刷新既有行和创建新行，确保拒绝发生在任何数据修改前。
			incoming := []entities.UsageIdentity{{Name: "After", AuthTypeName: testCase.authTypeName, Identity: "existing-auth-index", Type: testCase.identityType, Provider: "After"}, {Name: "New", AuthTypeName: testCase.authTypeName, Identity: "new-auth-index", Type: testCase.identityType, Provider: "After"}}
			// err 接收零值 now 的明确拒绝结果。
			err := replaceUsageIdentityTimestampScopeError(ctx, db, testCase, incoming, time.Time{})
			// 错误必须清楚表明 sync time 为零，不能静默使用系统时间。
			if err == nil || !strings.Contains(err.Error(), "sync time is zero") {
				// 输出真实 error 方便发现错误分类漂移。
				t.Fatalf("zero now error = %v", err)
			}
			// rows 读取错误后的完整 auth type 范围。
			rows := loadUsageIdentityTimestampRows(t, db, testCase.authType)
			// 零时间调用不能创建第二行。
			if len(rows) != 1 {
				// 输出行集合证明事务前校验是否生效。
				t.Fatalf("rows after zero now = %+v", rows)
			}
			// 既有 metadata 必须保持调用前值。
			if rows["existing-auth-index"].Name != "Before" || rows["existing-auth-index"].Provider != "Before" || rows["existing-auth-index"].IsDeleted {
				// 任何字段改变都表示零时间错误发生得太晚。
				t.Fatalf("existing row changed after zero now: %+v", rows["existing-auth-index"])
			}
			// 既有 updated_at 也必须保持原值。
			assertUsageIdentityTime(t, rows["existing-auth-index"].UpdatedAt, originalUpdatedAt, "zero-now existing updated_at")
		})
	}
}

// replaceUsageIdentityTimestampScope 运行对应公开入口，并把非预期错误直接归因到当前测试。
func replaceUsageIdentityTimestampScope(t *testing.T, ctx context.Context, db *gorm.DB, testCase timestampReplaceCase, identities []entities.UsageIdentity, now time.Time) {
	// helper 失败应报告调用测试位置。
	t.Helper()
	// err 由不吞错误的底层 helper 返回。
	err := replaceUsageIdentityTimestampScopeError(ctx, db, testCase, identities, now)
	// 正常时间 replace 必须成功。
	if err != nil {
		// 输出范围名与错误便于定位具体入口。
		t.Fatalf("replace %s identities: %v", testCase.name, err)
	}
}

// replaceUsageIdentityTimestampScopeError 在两个公开 Replace 入口之间做测试侧分派。
func replaceUsageIdentityTimestampScopeError(ctx context.Context, db *gorm.DB, testCase timestampReplaceCase, identities []entities.UsageIdentity, now time.Time) error {
	// Auth File 使用完整 auth type replace。
	if testCase.authType == entities.UsageIdentityAuthTypeAuthFile {
		// 返回公开 Auth File replace 的真实错误。
		return repository.ReplaceUsageIdentitiesForAuthType(ctx, db, identities, testCase.authType, now)
	}
	// AI Provider 使用本轮成功 provider types 的 scoped replace。
	return repository.ReplaceUsageIdentitiesForProviderTypes(ctx, db, identities, testCase.providerTypes, now)
}

// loadUsageIdentityTimestampRows 按 identity 建立断言 map，同时保留 active 与自定义 deleted 行。
func loadUsageIdentityTimestampRows(t *testing.T, db *gorm.DB, authType entities.UsageIdentityAuthType) map[string]entities.UsageIdentity {
	// helper 失败应报告调用测试位置。
	t.Helper()
	// rows 接收当前 auth type 全部持久化字段。
	var rows []entities.UsageIdentity
	// 自定义 is_deleted 不是 GORM soft delete，因此普通查询即可读取完整历史。
	if err := db.Where("auth_type = ?", authType).Order("id ASC").Find(&rows).Error; err != nil {
		// 查询失败表示测试环境或 schema 错误。
		t.Fatalf("load usage identity timestamp rows: %v", err)
	}
	// byIdentity 让每条时间路径按稳定 auth-index 定位。
	byIdentity := make(map[string]entities.UsageIdentity, len(rows))
	// 把查询结果按精确 identity 建立索引。
	for _, row := range rows {
		// 每个 auth-index 在表内唯一，因此可直接赋值。
		byIdentity[row.Identity] = row
	}
	// 返回完整行 map 供时间与业务字段共同断言。
	return byIdentity
}

// assertUsageIdentityTime 使用 instant 比较，允许 serializer 把 location 规范化到项目时区。
func assertUsageIdentityTime(t *testing.T, got time.Time, want time.Time, field string) {
	// helper 失败应报告具体调用位置。
	t.Helper()
	// 时间必须非零且表示与调用方 now 相同的 instant。
	if got.IsZero() || !got.Equal(want) {
		// 输出 RFC3339Nano 便于识别零值、offset 或精度差异。
		t.Fatalf("%s = %s, want %s", field, got.Format(time.RFC3339Nano), want.Format(time.RFC3339Nano))
	}
}

// assertUsageIdentityTimePointer 验证 nullable 时间存在并等于期望 instant。
func assertUsageIdentityTimePointer(t *testing.T, got *time.Time, want time.Time, field string) {
	// helper 失败应报告具体调用位置。
	t.Helper()
	// nullable 字段必须存在且保持期望时间。
	if got == nil || got.IsZero() || !got.Equal(want) {
		// nil 单独输出，避免测试失败时解引用 panic。
		if got == nil {
			// 明确报告缺失字段。
			t.Fatalf("%s is nil, want %s", field, want.Format(time.RFC3339Nano))
		}
		// 非 nil 错误值输出精确时间。
		t.Fatalf("%s = %s, want %s", field, got.Format(time.RFC3339Nano), want.Format(time.RFC3339Nano))
	}
}

// assertUsageIdentityStatsPreserved 锁定 metadata refresh 不修改 Keeper-only alias、统计和游标。
func assertUsageIdentityStatsPreserved(t *testing.T, row entities.UsageIdentity, alias string, firstUsedAt time.Time, lastUsedAt time.Time, statsUpdatedAt time.Time) {
	// helper 失败应报告调用断言位置。
	t.Helper()
	// Alias 必须仍指向原 Keeper-only 展示覆盖。
	if row.Alias == nil || *row.Alias != alias {
		// 输出实际 alias 便于识别被清空或覆盖。
		t.Fatalf("refreshed alias = %+v, want %q", row.Alias, alias)
	}
	// 所有累计字段与补算游标必须保持 seed 值。
	if row.TotalRequests != 11 || row.SuccessCount != 8 || row.FailureCount != 3 || row.InputTokens != 101 || row.OutputTokens != 102 || row.ReasoningTokens != 103 || row.CachedTokens != 104 || row.CacheReadTokens != 105 || row.TotalTokens != 515 || row.LastAggregatedUsageEventID != 99 {
		// 输出完整行便于定位被 metadata update 误伤的字段。
		t.Fatalf("refreshed stats changed: %+v", row)
	}
	// FirstUsedAt 必须保留历史值。
	assertUsageIdentityTimePointer(t, row.FirstUsedAt, firstUsedAt, "refreshed first_used_at")
	// LastUsedAt 必须保留历史值。
	assertUsageIdentityTimePointer(t, row.LastUsedAt, lastUsedAt, "refreshed last_used_at")
	// StatsUpdatedAt 必须保留统计自己的更新时间。
	assertUsageIdentityTimePointer(t, row.StatsUpdatedAt, statsUpdatedAt, "refreshed stats_updated_at")
}

// openUsageIdentityTimestampDatabase 创建真实迁移后的 SQLite，并在测试结束时关闭连接。
func openUsageIdentityTimestampDatabase(t *testing.T, name string) *gorm.DB {
	// helper 失败应报告调用测试位置。
	t.Helper()
	// dbPath 放在测试临时目录，避免污染项目数据。
	dbPath := filepath.Join(t.TempDir(), name)
	// db 通过生产 OpenDatabase 创建以覆盖 serializer 与 schema 行为。
	db, err := repository.OpenDatabase(config.Config{SQLitePath: dbPath})
	// 数据库初始化必须成功才能继续时间合同测试。
	if err != nil {
		// 初始化失败属于测试环境错误。
		t.Fatalf("open usage identity timestamp database: %v", err)
	}
	// sqlDB 用于注册可靠的连接清理。
	sqlDB, err := db.DB()
	// 无法取得底层连接表示数据库未正确初始化。
	if err != nil {
		// 报告底层连接错误。
		t.Fatalf("load usage identity timestamp sql database: %v", err)
	}
	// 测试结束时关闭当前临时数据库连接。
	t.Cleanup(func() {
		// 关闭失败说明资源清理异常。
		if err := sqlDB.Close(); err != nil {
			// 清理错误同样应使测试失败。
			t.Fatalf("close usage identity timestamp database: %v", err)
		}
	})
	// 返回生产配置的 GORM 连接。
	return db
}
