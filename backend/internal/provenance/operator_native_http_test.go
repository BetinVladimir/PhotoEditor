package provenance

import (
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

func TestNativeCheckRequiresPinnedMTLSAndExactResponse(t *testing.T) {
	for _, mode := range []string{"valid", "pin", "chain", "status", "response", "cancelled", "closed"} {
		t.Run(mode, func(t *testing.T) {
			serverCert, clientCert, roots, pin := operatorMTLSFixture(t)
			chain := []byte("unit signing chain")
			policy := SigningPolicy{Schema: 1, ID: uuid.New(), Revision: "1",
				SDKLockSHA: bodyDigest([]byte("sdk")), KeyHandle: "key",
				CertificateChainSHA: bodyDigest(chain), TrustBundleSHA: bodyDigest([]byte("trust")),
				AllowedMIME: []string{"image/png"}, TimestampMode: "off", RevocationMode: "off"}
			policyRaw, err := canonical.Marshal(policy)
			if err != nil {
				t.Fatal(err)
			}
			want, err := canonical.Marshal(struct {
				Schema    int    `json:"schema_version"`
				PolicySHA string `json:"policy_sha256"`
				ChainSHA  string `json:"certificate_chain_sha256"`
			}{1, bodyDigest(policyRaw), bodyDigest(chain)})
			if err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				body, readErr := io.ReadAll(io.LimitReader(r.Body, 4097))
				if readErr != nil || r.TLS == nil || len(r.TLS.VerifiedChains) == 0 ||
					r.Method != http.MethodPost || r.URL.Path != "/internal/v1/operator-native-check" ||
					!bytes.Equal(body, want) {
					t.Error("native check lost exact mTLS request", readErr)
				}
				if mode == "status" {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if mode == "response" {
					_, _ = w.Write(append(append([]byte(nil), want...), ' '))
					return
				}
				_, _ = w.Write(want)
			}))
			server.TLS = &tls.Config{Certificates: []tls.Certificate{serverCert}, ClientCAs: roots,
				ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS13}
			server.StartTLS()
			defer server.Close()
			if mode == "pin" {
				pin = strings.Repeat("f", 64)
			}
			client, err := NewHTTPSigningNativeCheck(server.URL, roots, clientCert, pin, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			if mode == "closed" {
				client.Close()
			}
			if mode == "chain" {
				chain = []byte("foreign chain")
			}
			err = client.Check(ctx, policy, chain)
			if mode == "valid" {
				if err != nil || calls.Load() != 1 {
					t.Fatal("native check refused", err)
				}
			} else if err == nil || ((mode == "pin" || mode == "chain" || mode == "cancelled" || mode == "closed") && calls.Load() != 0) {
				t.Fatal("invalid native check accepted or reached signer", mode, err)
			}
		})
	}
}

func TestNativeCheckCloseCancelsInflightRequest(t *testing.T) {
	serverCert, clientCert, roots, pin := operatorMTLSFixture(t)
	chain := []byte("unit signing chain")
	policy := SigningPolicy{Schema: 1, ID: uuid.New(), Revision: "1",
		SDKLockSHA: bodyDigest([]byte("sdk")), KeyHandle: "key",
		CertificateChainSHA: bodyDigest(chain), TrustBundleSHA: bodyDigest([]byte("trust")),
		AllowedMIME: []string{"image/png"}, TimestampMode: "off", RevocationMode: "off"}
	started := make(chan struct{}, 1)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-r.Context().Done()
	}))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{serverCert}, ClientCAs: roots,
		ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS13}
	server.StartTLS()
	defer server.Close()
	client, err := NewHTTPSigningNativeCheck(server.URL, roots, clientCert, pin, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	result := make(chan error, 1)
	go func() { result <- client.Check(context.Background(), policy, chain) }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("native check never reached signer")
	}
	client.Close()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("closed native check succeeded")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("close did not cancel native check")
	}
}
