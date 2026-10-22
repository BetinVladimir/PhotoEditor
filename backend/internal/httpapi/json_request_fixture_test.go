package httpapi

import (
	"io"
	"net/http"
	"net/http/httptest"
)

// JSON handler fixtures must exercise the body contract past media admission.
func newJSONTestRequest(method, target string, body io.Reader) *http.Request {
	request := httptest.NewRequest(method, target, body)
	request.Header.Set("Content-Type", "application/json")
	return request
}
