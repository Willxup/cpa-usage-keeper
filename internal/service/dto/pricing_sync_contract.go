package dto

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"cpa-usage-keeper/internal/pricing"
)

// PricingSyncFetchResponse 仅返回来源标识、匹配结果和未匹配模型，供用户审核后选择应用。
type PricingSyncFetchResponse struct {
	Source          string                  `json:"source"`
	Matches         []PricingSyncFetchMatch `json:"matches"`
	UnmatchedModels []string                `json:"unmatched_models"`
}

type PricingSyncFetchMatch struct {
	Model        string             `json:"model"`
	MatchedModel string             `json:"matched_model"`
	Provider     string             `json:"provider"`
	PricingStyle string             `json:"pricing_style"`
	BasePrices   pricing.BasePrices `json:"base_prices"`
}

type PricingSyncApplyRequest struct {
	Source string                 `json:"source"`
	Items  []PricingSyncApplyItem `json:"items"`
}

// UnmarshalJSON 区分缺失数组与空批次，逐项保留基础价缺字段的准确路径。
func (request *PricingSyncApplyRequest) UnmarshalJSON(data []byte) error {
	var wire struct {
		Source *string         `json:"source"`
		Items  json.RawMessage `json:"items"`
	}
	if err := decodePricingSyncObject(data, &wire); err != nil {
		return err
	}
	if wire.Source == nil {
		return &pricing.ValidationError{Path: "source", Code: "required", Reason: "source is required"}
	}
	if len(wire.Items) == 0 || bytes.Equal(bytes.TrimSpace(wire.Items), []byte("null")) {
		return &pricing.ValidationError{Path: "items", Code: "required", Reason: "items is required"}
	}
	var rawItems []json.RawMessage
	if err := json.Unmarshal(wire.Items, &rawItems); err != nil {
		return err
	}
	if len(rawItems) == 0 {
		return &pricing.ValidationError{Path: "items", Code: "invalid", Reason: "items must not be empty"}
	}
	items := make([]PricingSyncApplyItem, len(rawItems))
	for index, raw := range rawItems {
		if err := json.Unmarshal(raw, &items[index]); err != nil {
			return pricingSyncFieldError(fmt.Sprintf("items[%d]", index), err)
		}
	}
	*request = PricingSyncApplyRequest{Source: *wire.Source, Items: items}
	return nil
}

type PricingSyncApplyItem struct {
	Model        string             `json:"model"`
	BasePrices   pricing.BasePrices `json:"base_prices"`
	PricingStyle string             `json:"pricing_style"`
}

// UnmarshalJSON 要求来源项显式给出模型、风格和四项价格，零价不能被误判为缺失。
func (item *PricingSyncApplyItem) UnmarshalJSON(data []byte) error {
	var wire struct {
		Model        *string         `json:"model"`
		BasePrices   json.RawMessage `json:"base_prices"`
		PricingStyle *string         `json:"pricing_style"`
	}
	if err := decodePricingSyncObject(data, &wire); err != nil {
		return err
	}
	if wire.Model == nil {
		return &pricing.ValidationError{Path: "model", Code: "required", Reason: "model is required"}
	}
	if len(wire.BasePrices) == 0 || bytes.Equal(bytes.TrimSpace(wire.BasePrices), []byte("null")) {
		return &pricing.ValidationError{Path: "base_prices", Code: "required", Reason: "base_prices is required"}
	}
	if wire.PricingStyle == nil {
		return &pricing.ValidationError{Path: "pricing_style", Code: "required", Reason: "pricing_style is required"}
	}
	var prices pricing.BasePrices
	if err := json.Unmarshal(wire.BasePrices, &prices); err != nil {
		return pricingSyncFieldError("base_prices", err)
	}
	*item = PricingSyncApplyItem{Model: *wire.Model, BasePrices: prices, PricingStyle: *wire.PricingStyle}
	return nil
}

type PricingSyncApplyResponse struct {
	Models         []pricing.ModelPricingConfig `json:"models"`
	ConfigRevision int64                        `json:"config_revision"`
}

func decodePricingSyncObject(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func pricingSyncFieldError(prefix string, err error) error {
	var field *pricing.ValidationError
	if !errors.As(err, &field) {
		return err
	}
	path := prefix
	if field.Path != "" {
		path += "." + field.Path
	}
	return &pricing.ValidationError{Path: path, Code: field.Code, Reason: field.Reason}
}
