package provenance

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

func TestRemoteNativeTrustClosedPolicyAndActualProof(t *testing.T) {
	for _, mode := range []string{"valid", "policy", "chain", "signature", "trust", "recursive", "closed", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			signer, chain, policy, now := operatorChallengeFixture(t, "ED25519")
			raw, err := canonical.Marshal(policy)
			if err != nil {
				t.Fatal(err)
			}
			serverCertificate, identity, roots, pin := operatorMTLSFixture(t)
			var calls atomic.Int32
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var request struct {
					Schema    int    `json:"schema_version"`
					Handle    string `json:"key_handle"`
					Algorithm string `json:"algorithm"`
					Payload   string `json:"payload_hex"`
				}
				if json.NewDecoder(r.Body).Decode(&request) != nil {
					w.WriteHeader(400)
					return
				}
				payload, err := hex.DecodeString(request.Payload)
				if err != nil {
					w.WriteHeader(400)
					return
				}
				signature := ed25519.Sign(signer.(ed25519.PrivateKey), payload)
				if mode == "signature" {
					signature[0] ^= 1
				}
				body, err := canonical.Marshal(map[string]any{"schema_version": 1, "signature_hex": hex.EncodeToString(signature)})
				if err != nil {
					t.Error(err)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(body)
			}))
			server.TLS = &tls.Config{Certificates: []tls.Certificate{serverCertificate}, ClientCAs: roots, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS13}
			server.StartTLS()
			defer server.Close()
			trustCalls := 0
			var native *RemoteSigningNativeTrust
			trust := operatorTrustFunc(func(ctx context.Context, p SigningPolicy, c []byte) error {
				trustCalls++
				if mode == "trust" {
					return ErrInvalid
				}
				if mode == "recursive" {
					return native.Check(ctx, p, c)
				}
				return nil
			})
			native, err = NewRemoteSigningNativeTrust(RemoteSigningNativeInstallation{Policies: [][]byte{raw}, CertificateChain: chain, Origin: server.URL, ServerSHA: pin, Roots: roots, Identity: identity, Timeout: time.Second, IndependentTrust: trust, Clock: func() time.Time { return now }})
			if err != nil {
				t.Fatal(err)
			}
			defer native.Close()
			raw[0] = '!' // Constructor owns installed policy bytes.
			if mode == "policy" {
				policy.ID = uuid.New()
			}
			candidate := append([]byte(nil), chain...)
			if mode == "chain" {
				candidate[0] = '!'
			}
			if mode == "closed" {
				native.Close()
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			err = native.Check(ctx, policy, candidate)
			if (err == nil) != (mode == "valid") {
				t.Fatalf("mode=%s err=%v", mode, err)
			}
			if mode == "recursive" && trustCalls != 1 {
				t.Fatal("recursive authority was entered twice")
			}
			if mode == "valid" && (calls.Load() != 1 || trustCalls != 2) {
				t.Fatal("proof/trust not repeated")
			}
			if (mode == "policy" || mode == "chain" || mode == "trust" || mode == "recursive" || mode == "closed" || mode == "cancelled") && calls.Load() != 0 {
				t.Fatal("unqualified request reached key")
			}
		})
	}
}

func TestRemoteNativeTrustRejectsIndirectCycleAndDepthBeforeProof(t *testing.T) {
	for _, mode := range []string{"indirect", "depth"} {
		t.Run(mode, func(t *testing.T) {
			native := &RemoteSigningNativeTrust{chain: []byte("chain"), proof: &ChallengeSigningOperatorAuthority{}}
			var parent *remoteSigningTrustInvocation
			if mode == "indirect" {
				parent = &remoteSigningTrustInvocation{authority: native}
				parent = &remoteSigningTrustInvocation{authority: &RemoteSigningNativeTrust{}, parent: parent}
			} else {
				for i := 0; i < 16; i++ {
					parent = &remoteSigningTrustInvocation{authority: &RemoteSigningNativeTrust{}, parent: parent}
				}
			}
			ctx := context.WithValue(context.Background(), remoteSigningTrustContextKey{}, parent)
			if err := native.Check(ctx, SigningPolicy{}, []byte("chain")); err != ErrInvalid {
				t.Fatalf("cycle/depth err=%v", err)
			}
		})
	}
}
