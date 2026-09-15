package service

import (
	"fmt"
	"strings"
	"time"

	"cpa-usage-keeper/internal/cpa/dto/response"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

// syncManagementAPIKeys 只 reconcile 本轮成功返回的来源；失败来源不参与 stale 判断。
func syncManagementAPIKeys(db *gorm.DB, nativeResult *response.ManagementAPIKeysResult, nativeFetchErr error, pluginResult *response.CPAKeyPolicyKeysResult, pluginFetchErr error, now time.Time) (error, error) {
	// nil 数据库仍返回与旧实现一致的配置错误。
	if db == nil {
		// 不尝试任何持久化。
		return fmt.Errorf("database is nil"), nil
	}

	snapshots := make([]repository.CPAAPIKeySourceSnapshot, 0, 2)
	var nativeErr error
	if nativeFetchErr != nil {
		nativeErr = fmt.Errorf("fetch management api keys: %w", nativeFetchErr)
	} else if nativeResult == nil {
		nativeErr = fmt.Errorf("fetch management api keys: empty response")
	} else {
		keys := make([]repository.CPAAPIKeyMetadata, 0, len(nativeResult.Payload.APIKeys))
		for _, key := range nativeResult.Payload.APIKeys {
			keys = append(keys, repository.CPAAPIKeyMetadata{APIKey: key})
		}
		snapshots = append(snapshots, repository.CPAAPIKeySourceSnapshot{Source: entities.CPAAPIKeySourceNative, Keys: keys})
	}

	var pluginWarning error
	if pluginFetchErr != nil {
		pluginWarning = fmt.Errorf("fetch cpa-key-policy keys: %w", pluginFetchErr)
	} else if pluginResult == nil || pluginResult.Payload.Keys == nil {
		pluginWarning = fmt.Errorf("fetch cpa-key-policy keys: empty response")
	} else {
		keys := make([]repository.CPAAPIKeyMetadata, 0, len(pluginResult.Payload.Keys))
		for _, key := range pluginResult.Payload.Keys {
			id := strings.TrimSpace(key.ID)
			name := strings.TrimSpace(key.Name)
			if name == "" {
				name = id
			}
			keys = append(keys, repository.CPAAPIKeyMetadata{APIKey: id, DisplayKey: key.KeyPreview, KeyAlias: name})
		}
		snapshots = append(snapshots, repository.CPAAPIKeySourceSnapshot{Source: entities.CPAAPIKeySourceCPAKeyPolicy, Keys: keys})
	}

	var syncErr error
	if len(snapshots) > 0 {
		if err := repository.SyncCPAAPIKeySnapshots(db, snapshots, now); err != nil {
			syncErr = fmt.Errorf("sync management api keys: %w", err)
		}
	}
	return joinErrors(nativeErr, syncErr), pluginWarning
}
