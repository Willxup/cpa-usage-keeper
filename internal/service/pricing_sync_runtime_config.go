package service

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
)

const defaultPricingSyncOpenAIOfficialUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/137.0.0.0 Safari/537.36"

type PricingSyncRuntimeConfig struct {
	ModelAliases   map[string][]string
	OpenAIOfficial PricingSyncOpenAIOfficialRuntimeConfig
}

type PricingSyncOpenAIOfficialRuntimeConfig struct {
	UserAgent string
}

type PricingSyncRuntimeConfigSource struct {
	EnvFilePath             string
	ModelAliasesJSON        string
	OpenAIOfficialUserAgent string
}

type PricingSyncRuntimeConfigProvider interface {
	Current() PricingSyncRuntimeConfig
}

type staticPricingSyncRuntimeConfigProvider struct {
	config PricingSyncRuntimeConfig
}

type pricingSyncRuntimeConfigSnapshot struct {
	config  PricingSyncRuntimeConfig
	modTime time.Time
	size    int64
}

type envFilePricingSyncRuntimeConfigProvider struct {
	path string

	mu       sync.RWMutex
	snapshot pricingSyncRuntimeConfigSnapshot
}

func NewPricingSyncRuntimeConfigProvider(source PricingSyncRuntimeConfigSource) (PricingSyncRuntimeConfigProvider, error) {
	initialConfig, err := loadPricingSyncRuntimeConfigFromSource(source)
	if err != nil {
		return nil, err
	}

	envFilePath := strings.TrimSpace(source.EnvFilePath)
	if envFilePath == "" {
		return staticPricingSyncRuntimeConfigProvider{config: initialConfig}, nil
	}

	info, err := os.Stat(envFilePath)
	if err != nil {
		return nil, fmt.Errorf("stat pricing sync env file: %w", err)
	}

	return &envFilePricingSyncRuntimeConfigProvider{
		path: envFilePath,
		snapshot: pricingSyncRuntimeConfigSnapshot{
			config:  initialConfig,
			modTime: info.ModTime(),
			size:    info.Size(),
		},
	}, nil
}

func (p staticPricingSyncRuntimeConfigProvider) Current() PricingSyncRuntimeConfig {
	return clonePricingSyncRuntimeConfig(p.config)
}

func (p *envFilePricingSyncRuntimeConfigProvider) Current() PricingSyncRuntimeConfig {
	p.mu.RLock()
	snapshot := p.snapshot
	p.mu.RUnlock()

	info, err := os.Stat(p.path)
	if err != nil {
		logrus.WithError(err).WithField("env_file", p.path).Warn("pricing sync runtime config reload skipped; using last good config")
		return clonePricingSyncRuntimeConfig(snapshot.config)
	}
	if info.Size() == snapshot.size && info.ModTime().Equal(snapshot.modTime) {
		return clonePricingSyncRuntimeConfig(snapshot.config)
	}

	envMap, err := godotenv.Read(p.path)
	if err != nil {
		logrus.WithError(err).WithField("env_file", p.path).Warn("pricing sync runtime config reload failed; using last good config")
		return clonePricingSyncRuntimeConfig(snapshot.config)
	}
	nextConfig, err := loadPricingSyncRuntimeConfigFromEnvMap(envMap)
	if err != nil {
		logrus.WithError(err).WithField("env_file", p.path).Warn("pricing sync runtime config reload failed; using last good config")
		return clonePricingSyncRuntimeConfig(snapshot.config)
	}

	nextSnapshot := pricingSyncRuntimeConfigSnapshot{
		config:  nextConfig,
		modTime: info.ModTime(),
		size:    info.Size(),
	}

	p.mu.Lock()
	p.snapshot = nextSnapshot
	p.mu.Unlock()

	logrus.WithFields(logrus.Fields{
		"env_file":                          p.path,
		"alias_models":                      len(nextConfig.ModelAliases),
		"openai_official_custom_user_agent": nextConfig.OpenAIOfficial.UserAgent != defaultPricingSyncOpenAIOfficialUserAgent,
	}).Info("reloaded pricing sync runtime config")

	return clonePricingSyncRuntimeConfig(nextConfig)
}

func loadPricingSyncRuntimeConfigFromSource(source PricingSyncRuntimeConfigSource) (PricingSyncRuntimeConfig, error) {
	envFilePath := strings.TrimSpace(source.EnvFilePath)
	if envFilePath != "" {
		envMap, err := godotenv.Read(envFilePath)
		if err != nil {
			return PricingSyncRuntimeConfig{}, fmt.Errorf("read pricing sync env file: %w", err)
		}
		return loadPricingSyncRuntimeConfigFromEnvMap(envMap)
	}

	return loadPricingSyncRuntimeConfigFromEnvMap(map[string]string{
		"PRICING_SYNC_MODEL_ALIASES_JSON":         source.ModelAliasesJSON,
		"PRICING_SYNC_OPENAI_OFFICIAL_USER_AGENT": source.OpenAIOfficialUserAgent,
	})
}

func loadPricingSyncRuntimeConfigFromEnvMap(envMap map[string]string) (PricingSyncRuntimeConfig, error) {
	config := defaultPricingSyncRuntimeConfig()

	if rawAliases := strings.TrimSpace(envMap["PRICING_SYNC_MODEL_ALIASES_JSON"]); rawAliases != "" {
		aliases, err := decodePricingSyncModelAliases([]byte(rawAliases))
		if err != nil {
			return PricingSyncRuntimeConfig{}, fmt.Errorf("PRICING_SYNC_MODEL_ALIASES_JSON: %w", err)
		}
		config.ModelAliases = aliases
	}

	if userAgent := strings.TrimSpace(envMap["PRICING_SYNC_OPENAI_OFFICIAL_USER_AGENT"]); userAgent != "" {
		config.OpenAIOfficial.UserAgent = userAgent
	}

	return normalizePricingSyncRuntimeConfig(config), nil
}

func defaultPricingSyncRuntimeConfig() PricingSyncRuntimeConfig {
	return normalizePricingSyncRuntimeConfig(PricingSyncRuntimeConfig{})
}

func normalizePricingSyncRuntimeConfig(config PricingSyncRuntimeConfig) PricingSyncRuntimeConfig {
	config.ModelAliases = normalizePricingSyncModelAliases(config.ModelAliases)
	config.OpenAIOfficial.UserAgent = strings.TrimSpace(config.OpenAIOfficial.UserAgent)
	if config.OpenAIOfficial.UserAgent == "" {
		config.OpenAIOfficial.UserAgent = defaultPricingSyncOpenAIOfficialUserAgent
	}
	return config
}

func clonePricingSyncRuntimeConfig(config PricingSyncRuntimeConfig) PricingSyncRuntimeConfig {
	return PricingSyncRuntimeConfig{
		ModelAliases:   clonePricingSyncModelAliases(config.ModelAliases),
		OpenAIOfficial: config.OpenAIOfficial,
	}
}
