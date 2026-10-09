package test

import (
	"net/http"
	"net/http/httptest"
	"strings"
)

func serveCredentialMutation(router http.Handler, method, target, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
	request.Header.Set("Content-Type", "application/json")
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}
