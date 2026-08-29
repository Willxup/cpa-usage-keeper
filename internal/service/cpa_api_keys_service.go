package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

var ErrInvalidID = errors.New("invalid id")

// managementAPIKeyAdder 是生成 CPA API Key 需要依赖的最小 CPA 写接口，测试可以用它替换真实 client。
type managementAPIKeyAdder interface {
	AddManagementAPIKey(ctx context.Context, apiKey string) error
}

type CPAAPIKeyProvider interface {
	ListCPAAPIKeys(ctx context.Context) ([]entities.CPAAPIKey, error)
	FindActiveCPAAPIKeyByValue(ctx context.Context, apiKey string) (entities.CPAAPIKey, error)
	FindActiveCPAAPIKeyByID(ctx context.Context, id int64) (entities.CPAAPIKey, error)
	UpdateCPAAPIKeyAlias(ctx context.Context, id int64, keyAlias string) (entities.CPAAPIKey, error)
}

// CPAAPIKeyGenerator 是可选的上游生成能力，路由层通过类型断言按需使用，避免强制所有实现都具备写 CPA 的能力。
type CPAAPIKeyGenerator interface {
	GenerateCPAAPIKey(ctx context.Context, keyAlias string) (entities.CPAAPIKey, error)
}

type cpaAPIKeyService struct {
	db     *gorm.DB
	client managementAPIKeyAdder
}

func NewCPAAPIKeyService(db *gorm.DB, client managementAPIKeyAdder) CPAAPIKeyProvider {
	return &cpaAPIKeyService{db: db, client: client}
}

func (s *cpaAPIKeyService) ListCPAAPIKeys(context.Context) ([]entities.CPAAPIKey, error) {
	return repository.ListActiveCPAAPIKeys(s.db)
}

func (s *cpaAPIKeyService) FindActiveCPAAPIKeyByValue(_ context.Context, apiKey string) (entities.CPAAPIKey, error) {
	trimmed := strings.TrimSpace(apiKey)
	if trimmed == "" {
		return entities.CPAAPIKey{}, gorm.ErrRecordNotFound
	}
	return repository.FindActiveCPAAPIKeyByValue(s.db, trimmed)
}

func (s *cpaAPIKeyService) FindActiveCPAAPIKeyByID(_ context.Context, id int64) (entities.CPAAPIKey, error) {
	if id <= 0 {
		return entities.CPAAPIKey{}, gorm.ErrRecordNotFound
	}
	return repository.FindActiveCPAAPIKeyByID(s.db, id)
}

func (s *cpaAPIKeyService) UpdateCPAAPIKeyAlias(_ context.Context, id int64, keyAlias string) (entities.CPAAPIKey, error) {
	if id <= 0 {
		return entities.CPAAPIKey{}, ErrInvalidID
	}
	// UPDATE 由 dbresolver 自动路由 writer；结果回读再用官方 Write clause 固定到同一物理池。
	if err := repository.UpdateCPAAPIKeyAlias(s.db, id, keyAlias); err != nil {
		return entities.CPAAPIKey{}, err
	}
	return repository.FindActiveCPAAPIKeyByID(s.db.Clauses(dbresolver.Write), id)
}

// GenerateCPAAPIKey 在 CPA 上游生成一个新 API Key，并把记录和别名写入本地库。
// 生成成功后才本地落库，避免 CPA 未接受时留下孤立记录。
func (s *cpaAPIKeyService) GenerateCPAAPIKey(ctx context.Context, keyAlias string) (entities.CPAAPIKey, error) {
	if s.db == nil {
		return entities.CPAAPIKey{}, errors.New("api key database is nil")
	}
	if s.client == nil {
		return entities.CPAAPIKey{}, errors.New("cpa client is not configured")
	}
	apiKey, err := generateRandomAPIKey()
	if err != nil {
		return entities.CPAAPIKey{}, err
	}
	if err := s.client.AddManagementAPIKey(ctx, apiKey); err != nil {
		return entities.CPAAPIKey{}, err
	}
	now := timeutil.NormalizeStorageTime(time.Now())
	return repository.UpsertActiveCPAAPIKey(s.db, apiKey, keyAlias, now)
}

// generateRandomAPIKey 生成一个带固定前缀的随机 API Key，避免与真实 provider key 混淆。
func generateRandomAPIKey() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "kpr_" + hex.EncodeToString(buf), nil
}
