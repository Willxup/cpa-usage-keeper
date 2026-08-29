package quota

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

type AutoRefreshScheduleUnit string

const (
	AutoRefreshScheduleUnitMinute AutoRefreshScheduleUnit = "minute"
	AutoRefreshScheduleUnitHour   AutoRefreshScheduleUnit = "hour"
	AutoRefreshScheduleUnitDay    AutoRefreshScheduleUnit = "day"
	AutoRefreshScheduleUnitWeek   AutoRefreshScheduleUnit = "week"

	autoRefreshEnabledSettingKey  = "quota.auto_refresh.enabled"
	autoRefreshScheduleSettingKey = "quota.auto_refresh.schedule"
)

type AutoRefreshSchedule struct {
	Unit  AutoRefreshScheduleUnit `json:"unit"`
	Value int                     `json:"value"`
}

type AutoRefreshSettings struct {
	Enabled  bool                 `json:"enabled"`
	Schedule *AutoRefreshSchedule `json:"schedule"`
}

func (s *Service) GetAutoRefreshSettings(ctx context.Context) (AutoRefreshSettings, error) {
	if s == nil || s.db == nil {
		return AutoRefreshSettings{}, nil
	}
	settings := AutoRefreshSettings{}
	storedSettings, err := repository.GetAppSettings(ctx, s.db, []string{
		autoRefreshEnabledSettingKey,
		autoRefreshScheduleSettingKey,
	})
	if err != nil {
		return AutoRefreshSettings{}, err
	}
	enabledSetting, found := storedSettings[autoRefreshEnabledSettingKey]
	if found && enabledSetting.Value != nil {
		enabled, err := strconv.ParseBool(strings.TrimSpace(*enabledSetting.Value))
		if err != nil {
			return AutoRefreshSettings{}, fmt.Errorf("%w: quota auto refresh enabled must be true or false", ErrValidation)
		}
		settings.Enabled = enabled
	}

	scheduleSetting, found := storedSettings[autoRefreshScheduleSettingKey]
	if !found || scheduleSetting.Value == nil || strings.TrimSpace(*scheduleSetting.Value) == "" {
		return settings, nil
	}
	var schedule AutoRefreshSchedule
	if err := json.Unmarshal([]byte(*scheduleSetting.Value), &schedule); err != nil {
		return AutoRefreshSettings{}, fmt.Errorf("%w: quota auto refresh schedule is invalid JSON", ErrValidation)
	}
	normalized, err := normalizeAutoRefreshSchedule(&schedule)
	if err != nil {
		return AutoRefreshSettings{}, err
	}
	settings.Schedule = normalized
	return settings, nil
}

func (s *Service) UpdateAutoRefreshSettings(ctx context.Context, settings AutoRefreshSettings) (AutoRefreshSettings, error) {
	if s == nil || s.db == nil {
		return AutoRefreshSettings{}, nil
	}
	normalizedSchedule, err := normalizeAutoRefreshSchedule(settings.Schedule)
	if err != nil {
		return AutoRefreshSettings{}, err
	}
	enabledValue := strconv.FormatBool(settings.Enabled)
	var scheduleValue *string
	if normalizedSchedule != nil {
		payload, err := json.Marshal(normalizedSchedule)
		if err != nil {
			return AutoRefreshSettings{}, fmt.Errorf("marshal quota auto refresh schedule: %w", err)
		}
		value := string(payload)
		scheduleValue = &value
	}

	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := repository.UpsertAppSetting(ctx, tx, entities.AppSetting{
			SettingKey: autoRefreshEnabledSettingKey,
			Value:      &enabledValue,
			ValueType:  entities.AppSettingValueTypeBool,
		}); err != nil {
			return err
		}
		if _, err := repository.UpsertAppSetting(ctx, tx, entities.AppSetting{
			SettingKey: autoRefreshScheduleSettingKey,
			Value:      scheduleValue,
			ValueType:  entities.AppSettingValueTypeJSON,
		}); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return AutoRefreshSettings{}, err
	}
	s.resetAutoRefreshScheduleAnchor()
	s.notifyAutoRefreshSettingsChanged()

	return AutoRefreshSettings{Enabled: settings.Enabled, Schedule: normalizedSchedule}, nil
}

func normalizeAutoRefreshSchedule(schedule *AutoRefreshSchedule) (*AutoRefreshSchedule, error) {
	if schedule == nil {
		return nil, nil
	}
	unit := AutoRefreshScheduleUnit(strings.ToLower(strings.TrimSpace(string(schedule.Unit))))
	value := schedule.Value
	var max int
	switch unit {
	case AutoRefreshScheduleUnitMinute:
		max = 60
	case AutoRefreshScheduleUnitHour:
		max = 24
	case AutoRefreshScheduleUnitDay:
		max = 30
	case AutoRefreshScheduleUnitWeek:
		max = 7
	default:
		return nil, fmt.Errorf("%w: quota auto refresh schedule unit must be minute, hour, day, or week", ErrValidation)
	}
	if value < 1 || value > max {
		return nil, fmt.Errorf("%w: quota auto refresh %s value must be between 1 and %d", ErrValidation, unit, max)
	}
	return &AutoRefreshSchedule{Unit: unit, Value: value}, nil
}
