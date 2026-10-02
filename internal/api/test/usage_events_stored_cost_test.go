package test

import (
	"encoding/csv"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	. "cpa-usage-keeper/internal/api"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

func TestUsageEventListAndExportsKeepSameStoredAmounts(t *testing.T) {
	when := time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)
	provider := &usageEventsStub{events: []servicedto.UsageEventRecord{
		{ID: 41, Timestamp: when, Model: "model-a", TotalTokens: 100, CostUSD: 1.25, CostAvailable: true, PricingStyle: "openai"},
		{ID: 42, Timestamp: when.Add(time.Minute), Model: "model-a", TotalTokens: 50, CostUSD: 0, CostAvailable: false, PricingStyle: "openai"},
	}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
	query := "?range=24h&model=model-a"
	list := serveAPIGet(router, "/api/v1/usage/events"+query)
	if list.Code != http.StatusOK {
		t.Fatalf("列表失败: %d %s", list.Code, list.Body.String())
	}
	var listed struct {
		Events []struct {
			CostUSD       float64 `json:"cost_usd"`
			CostAvailable bool    `json:"cost_available"`
		} `json:"events"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &listed); err != nil || len(listed.Events) != 2 {
		t.Fatalf("解析列表费用: %+v %v", listed, err)
	}
	if listed.Events[0].CostUSD != 1.25 || !listed.Events[0].CostAvailable || listed.Events[1].CostUSD != 0 || listed.Events[1].CostAvailable {
		t.Fatalf("列表金额或缺价状态不符: %+v", listed.Events)
	}
	if provider.filterCalls != 1 || provider.lastFilter.Model != "model-a" {
		t.Fatalf("列表筛选未传递: %+v", provider.lastFilter)
	}
	jsonExport := serveAPIGet(router, "/api/v1/usage/events/export"+query+"&format=json")
	if jsonExport.Code != http.StatusOK {
		t.Fatalf("JSON 导出失败: %d %s", jsonExport.Code, jsonExport.Body.String())
	}
	var exported struct {
		Events []struct {
			CostUSD float64 `json:"cost_usd"`
		} `json:"events"`
		TotalCount int64 `json:"total_count"`
	}
	if err := json.Unmarshal(jsonExport.Body.Bytes(), &exported); err != nil || exported.TotalCount != 2 || len(exported.Events) != 2 {
		t.Fatalf("解析 JSON 导出费用: %+v %v", exported, err)
	}
	if exported.Events[0].CostUSD != listed.Events[0].CostUSD || exported.Events[1].CostUSD != listed.Events[1].CostUSD ||
		strings.Contains(jsonExport.Body.String(), "cost_available") || strings.Contains(jsonExport.Body.String(), "pricing_style") {
		t.Fatalf("JSON 导出金额或独立字段合同不符: %s", jsonExport.Body.String())
	}
	csvExport := serveAPIGet(router, "/api/v1/usage/events/export"+query+"&format=csv")
	if csvExport.Code != http.StatusOK {
		t.Fatalf("CSV 导出失败: %d %s", csvExport.Code, csvExport.Body.String())
	}
	records, err := csv.NewReader(strings.NewReader(csvExport.Body.String())).ReadAll()
	if err != nil || len(records) != 3 {
		t.Fatalf("解析 CSV 导出费用: rows=%d err=%v", len(records), err)
	}
	if records[0][len(records[0])-1] != "cost_usd" || strings.Contains(strings.Join(records[0], ","), "cost_available") {
		t.Fatalf("CSV 列顺序或独立字段合同变化: %+v", records[0])
	}
	for index := range listed.Events {
		got, err := strconv.ParseFloat(records[index+1][len(records[index+1])-1], 64)
		if err != nil || got != listed.Events[index].CostUSD {
			t.Fatalf("CSV 第%d行金额与列表不同: %q %v", index, records[index+1], err)
		}
	}
	if provider.exportCalls != 2 || provider.filterCalls != 1 || provider.lastFilter.Model != "model-a" {
		t.Fatalf("列表/导出筛选或流式调用次数变化: %+v", provider)
	}
}
