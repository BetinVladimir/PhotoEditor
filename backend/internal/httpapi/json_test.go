package httpapi

import (
	"net/http"
	"net/http/httptest"
	"photoeditor/backend/internal/domain"
	"strings"
	"testing"
)

func TestStrictJSON(t *testing.T) {
	for _, body := range []string{`{"name":"a","name":"b"}`, `{"name":"a"} {}`, `{"other":1}`, `{"name":"\ud800"}`, `{"name":"\udc00"}`, "{\"name\":\"\xff\"}", `{"name":NaN}`} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		var dst struct {
			Name string `json:"name"`
		}
		if err := decodeJSON(r, &dst); err == nil {
			t.Fatalf("accepted %q", body)
		}
	}
	for _, body := range []string{`{"name":"ok"}`, `{"name":"\ud83d\ude00"}`, `{"name":"\\ud800"}`} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json; charset=utf-8")
		var dst struct {
			Name string `json:"name"`
		}
		if err := decodeJSON(r, &dst); err != nil {
			t.Fatalf("rejected %q: %v", body, err)
		}
	}
}
func TestJSONBodyLimit(t *testing.T) {
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"long"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.Body = http.MaxBytesReader(w, r.Body, 3)
	var dst any
	err := decodeJSON(r, &dst)
	if domain.AsError(err).HTTPStatus != 413 {
		t.Fatal(err)
	}
}
