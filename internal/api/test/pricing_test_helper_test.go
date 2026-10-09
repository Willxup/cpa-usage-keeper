package test

import (
	"net/http"
	"net/http/httptest"
	"strings"

	"cpa-usage-keeper/internal/pricing"
)

func emptyPricingCatalogForTest() *pricing.Catalog {
	return pricing.NewCatalog(pricing.EmptySnapshot())
}

func newPricingRequest(method, target, body string) *http.Request {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	if method != http.MethodGet {
		request.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
		request.Header.Set("Content-Type", "application/json")
	}
	return request
}
