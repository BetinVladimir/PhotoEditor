package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckRequiresLoopbackAndSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	if err := check(server.URL, "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := check("https://example.com/health/ready", "", "", "", ""); err == nil {
		t.Fatal("remote health probe accepted")
	}
}

func TestCheckRejectsFailureAndPartialMTLS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	defer server.Close()
	if err := check(server.URL, "", "", "", ""); err == nil {
		t.Fatal("unhealthy response accepted")
	}
	if err := check("https://127.0.0.1:1/health/ready", "ca", "", "", "object-gateway"); err == nil {
		t.Fatal("partial mTLS configuration accepted")
	}
}
