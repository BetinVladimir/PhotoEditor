package provenance

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/asn1"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPSigningTSAClientPinnedTransportAndVerifiedReply(t *testing.T) {
	for _, mode := range []string{"valid", "pin", "status", "redirect", "encoding", "mime", "oversize", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			rootPEM, reply, request, now := tsaTrustFixture(t, true)
			calls := 0
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/timestamp-query" || r.Header.Get("Accept") != "application/timestamp-reply" {
					t.Error("timestamp request envelope")
				}
				if mode == "redirect" {
					w.Header().Set("Location", "https://example.invalid/")
					w.WriteHeader(http.StatusFound)
					return
				}
				if mode == "status" {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				w.Header().Set("Content-Type", "application/timestamp-reply")
				if mode == "mime" {
					w.Header().Set("Content-Type", "text/plain")
				}
				if mode == "encoding" {
					w.Header().Set("Content-Encoding", "gzip")
				}
				if mode == "oversize" {
					_, _ = w.Write(make([]byte, (2<<20)+1))
					return
				}
				_, _ = w.Write(reply)
			}))
			server.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
			server.StartTLS()
			defer server.Close()
			roots := x509.NewCertPool()
			roots.AddCert(server.Certificate())
			pinSum := sha256.Sum256(server.Certificate().Raw)
			pin := hex.EncodeToString(pinSum[:])
			if mode == "pin" {
				pin = hex.EncodeToString(make([]byte, 32))
			}
			trust, err := NewRFC3161TSATrust(rootPEM, asn1.ObjectIdentifier{1, 2, 3, 4},
				func() time.Time { return now }, time.Minute,
				func(context.Context, [][]byte) error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			client, err := NewHTTPSigningTSAClient(server.URL, roots, pin, 2*time.Second, trust)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			err = client.exchange(ctx, request)
			if (err == nil) != (mode == "valid") {
				t.Fatalf("mode=%s err=%v", mode, err)
			}
			if mode == "cancelled" && calls != 0 {
				t.Fatal("cancelled request performed IO")
			}
		})
	}
}

func TestHTTPSigningTSAClientCloseRevokesFurtherUse(t *testing.T) {
	var nilClient *HTTPSigningTSAClient
	nilClient.Close()
	if nilClient.Timestamp(context.Background(), []byte("x")) == nil {
		t.Fatal("nil client accepted")
	}
	client := &HTTPSigningTSAClient{}
	client.Close()
	if client.Timestamp(context.Background(), []byte("x")) == nil {
		t.Fatal("closed client accepted")
	}
}
