package objectstore

import (
	"bytes"
	"context"
	"errors"
	madmin "github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestCORSServerReadbackRequiresCompleteEffectiveAllowlist(t *testing.T) {
	expected := []string{"https://app.example", "https://portal.example"}
	for _, tc := range []struct {
		name, config string
		valid        bool
	}{
		{"exact", "api cors_allow_origin=https://app.example,https://portal.example", true},
		{"effective env", "# MINIO_API_CORS_ALLOW_ORIGIN=https://portal.example,https://app.example\napi cors_allow_origin=*", true},
		{"effective wildcard", "# MINIO_API_CORS_ALLOW_ORIGIN=*\napi cors_allow_origin=https://app.example,https://portal.example", false},
		{"duplicate config key", "api cors_allow_origin=* cors_allow_origin=https://app.example,https://portal.example", false},
		{"dangling env", "api cors_allow_origin=https://app.example,https://portal.example\n# MINIO_API_CORS_ALLOW_ORIGIN=*", false},
		{"missing", "api cors_allow_origin=https://app.example", false},
		{"extra", "api cors_allow_origin=https://app.example,https://portal.example,https://extra.example", false},
		{"duplicate", "api cors_allow_origin=https://app.example,https://app.example", false},
		{"empty entry", "api cors_allow_origin=https://app.example,,https://portal.example", false},
		{"target", "api:other cors_allow_origin=https://app.example,https://portal.example", false},
		{"wrong subsystem", "site cors_allow_origin=https://app.example,https://portal.example", false},
		{"two subsystems", "api cors_allow_origin=https://app.example,https://portal.example\nsite name=test", false},
		{"broken quote", "api cors_allow_origin=\"https://app.example", false},
		{"oversized", strings.Repeat("x", maxCORSAdminResponse+1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := corsServerOriginsMatch([]byte(tc.config), expected, false); got != tc.valid {
				t.Fatalf("accepted=%v expected=%v", got, tc.valid)
			}
		})
	}
}

func TestCORSServerPreflightExactPublicPolicy(t *testing.T) {
	for _, mode := range []string{"exact", "wildcard", "foreign allowed", "missing header", "missing method", "redirect", "unavailable"} {
		t.Run(mode, func(t *testing.T) {
			origins := []string{"https://app.example", "https://portal.example"}
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "OPTIONS" || r.URL.Path != "/photo-test/photo-cors-bootstrap-probe" {
					t.Error("incorrect probe")
				}
				if mode == "redirect" {
					http.Redirect(w, r, "https://other.invalid", http.StatusTemporaryRedirect)
					return
				}
				if mode == "unavailable" {
					w.WriteHeader(503)
					return
				}
				origin := r.Header.Get("Origin")
				if origin == origins[0] || origin == origins[1] || mode == "foreign allowed" {
					if mode == "wildcard" {
						origin = "*"
					}
					w.Header().Set("Access-Control-Allow-Origin", origin)
					if mode != "missing method" {
						w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, PUT")
					}
					if mode != "missing header" {
						w.Header().Set("Access-Control-Allow-Headers", r.Header.Get("Access-Control-Request-Headers"))
					}
				}
				w.WriteHeader(204)
			}))
			defer server.Close()
			client := server.Client()
			client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
			err := verifyCORSServerPreflight(context.Background(), client, server.URL, "photo-test", origins)
			if mode == "exact" {
				if err != nil || calls != 9 {
					t.Fatalf("err=%v calls=%d", err, calls)
				}
			} else if err == nil {
				t.Fatal("inconsistent policy accepted")
			}
		})
	}
}
func TestCORSServerPreflightCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := verifyCORSServerPreflight(ctx, &http.Client{}, "http://localhost:1", "photo-test", []string{"https://app.example"}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestCORSAdminBodyBoundsUnknownLength(t *testing.T) {
	for _, size := range []int{maxCORSAdminResponse - 1, maxCORSAdminResponse, maxCORSAdminResponse + 1} {
		body := &boundedAdminBody{ReadCloser: io.NopCloser(strings.NewReader(strings.Repeat("a", size))), remaining: maxCORSAdminResponse}
		data, err := io.ReadAll(body)
		if size > maxCORSAdminResponse {
			if !errors.Is(err, ErrIntegrity) || len(data) > maxCORSAdminResponse {
				t.Fatal(len(data), err)
			}
		} else if err != nil || len(data) != size {
			t.Fatal(len(data), err)
		}
	}
}

func TestCORSStrategyIsExplicitAndValidated(t *testing.T) {
	for _, strategy := range []string{"", "bucket", "minio-server", "auto", "MINIO-SERVER"} {
		config := Config{Endpoint: "http://localhost:59030", Bucket: "photo-test", AccessKey: "test", SecretKey: "test-secret", Development: true, CORSStrategy: strategy, CORSOrigins: []string{"http://localhost:19106"}}
		store, err := New(config)
		valid := strategy == "" || strategy == "bucket" || strategy == "minio-server"
		if valid {
			if err != nil {
				t.Fatal(strategy, err)
			}
			if strategy == "" && store.corsStrategy != "bucket" {
				t.Fatal("default changed")
			}
		} else if err != ErrInvalid {
			t.Fatal("unknown strategy accepted", strategy, err)
		}
	}
	config := Config{Endpoint: "http://localhost:59030", Bucket: "photo-test", AccessKey: "test", SecretKey: "test-secret", Development: true, CORSStrategy: "minio-server"}
	if _, err := New(config); err != ErrInvalid {
		t.Fatal("empty server allowlist accepted", err)
	}
	t.Setenv("S3_CORS_STRATEGY", "minio-server")
	if ConfigFromEnv(true).CORSStrategy != "minio-server" {
		t.Fatal("strategy environment ignored")
	}
}

type corsRoundTripFunc func(*http.Request) (*http.Response, error)

func (f corsRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type corsCloseErrorBody struct {
	io.Reader
	closed   int
	closeErr error
}

func (b *corsCloseErrorBody) Close() error { b.closed++; return b.closeErr }
func TestCORSAdminResponseBoundAndClose(t *testing.T) {
	for _, tc := range []struct {
		name   string
		length int64
		status int
	}{
		{"known oversized", maxCORSAdminResponse + 1, 200}, {"redirect", 0, 307}, {"bounded normal", 4, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &corsCloseErrorBody{Reader: strings.NewReader("data")}
			transport := boundedAdminTransport{base: corsRoundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, ContentLength: tc.length, Body: body}, nil
			})}
			req, _ := http.NewRequest("GET", "http://localhost:59030", nil)
			response, err := transport.RoundTrip(req)
			if tc.name == "bounded normal" {
				if err != nil {
					t.Fatal(err)
				}
				response.Body.Close()
			} else if err != ErrIntegrity || response != nil {
				t.Fatal("unsafe response accepted", err)
			}
			if body.closed != 1 {
				t.Fatal("response not closed exactly once", body.closed)
			}
		})
	}
}
func TestCORSAdminReaderRejectsCloseFailureAndDenial(t *testing.T) {
	secret := "fixture-secret-for-cors-admin-reader"
	payload, err := madmin.EncryptData(secret, []byte("api cors_allow_origin=https://app.example"))
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"exact", "close error", "denied", "invalid ciphertext"} {
		t.Run(mode, func(t *testing.T) {
			data := payload
			status := 200
			var closeErr error
			if mode == "close error" {
				closeErr = errors.New("private close detail")
			}
			if mode == "denied" {
				status = 403
				data = []byte(`{"Code":"AccessDenied"}`)
			}
			if mode == "invalid ciphertext" {
				data = []byte("invalid encrypted data")
			}
			body := &corsCloseErrorBody{Reader: bytes.NewReader(data), closeErr: closeErr}
			base := corsRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.Method != "GET" || req.URL.Path != "/minio/admin/v3/get-config-kv" || req.URL.Query().Get("key") != "api" {
					t.Error("unexpected administrative request")
				}
				return &http.Response{StatusCode: status, ContentLength: int64(len(data)), Body: body, Header: make(http.Header)}, nil
			})
			endpoint, _ := url.Parse("http://localhost:59030")
			reader, err := corsServerReader(endpoint, credentials.NewStaticV4("fixture-access", secret, ""), base)
			if err != nil {
				t.Fatal(err)
			}
			actual, err := reader(context.Background())
			if mode == "exact" {
				if err != nil || string(actual) != "api cors_allow_origin=https://app.example" {
					t.Fatal("effective config unreadable", err)
				}
			} else if err != ErrUnavailable {
				t.Fatal("unsafe administrative result accepted", err)
			}
			if body.closed != 1 {
				t.Fatal("administrative response close count", body.closed)
			}
		})
	}
}
