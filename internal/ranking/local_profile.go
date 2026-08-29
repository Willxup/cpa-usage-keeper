package ranking

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"cpa-usage-keeper/internal/helper"
	"cpa-usage-keeper/internal/repository"
)

const maxLocalRankingKeyAliasRunes = 128

var ErrInvalidLocalProfile = errors.New("invalid local ranking profile")

// LocalProfile 是本地排行可编辑的 API Key 展示资料，不包含完整 Key。
type LocalProfile struct {
	ParticipantID string `json:"participant_id"`
	KeyAlias      string `json:"key_alias"`
	DisplayName   string `json:"display_name"`
	AvatarID      uint8  `json:"avatar_id"`
}

// UpdateProfile 将别名和头像覆盖值绑定到本地 API Key 记录；历史榜单中的软删除 Key 仍可编辑。
func (s *LocalRankingService) UpdateProfile(ctx context.Context, apiKeyID int64, keyAlias string, avatarID uint8) (LocalProfile, error) {
	if s == nil || s.db == nil || apiKeyID <= 0 || avatarID < MinAvatarID || avatarID > MaxAvatarID {
		return LocalProfile{}, ErrInvalidLocalProfile
	}
	keyAlias = strings.TrimSpace(keyAlias)
	if utf8.RuneCountInString(keyAlias) > maxLocalRankingKeyAliasRunes {
		return LocalProfile{}, ErrInvalidLocalProfile
	}
	for _, char := range keyAlias {
		if unicode.IsControl(char) {
			return LocalProfile{}, ErrInvalidLocalProfile
		}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	row, err := repository.UpdateCPAAPIKeyLocalRankingProfile(s.db.WithContext(ctx), apiKeyID, keyAlias, avatarID)
	if err != nil {
		return LocalProfile{}, err
	}
	return LocalProfile{
		ParticipantID: strconv.FormatInt(row.ID, 10),
		KeyAlias:      strings.TrimSpace(row.KeyAlias),
		DisplayName:   helper.CPAAPIKeyDisplayName(row),
		AvatarID:      *row.LocalRankingAvatarID,
	}, nil
}
