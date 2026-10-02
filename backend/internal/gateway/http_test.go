package gateway

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type resolverFunc func(context.Context, uuid.UUID, int, int64, uuid.UUID, *x509.Certificate) (TaskContext, error)

func (f resolverFunc) Resolve(c context.Context, t uuid.UUID, a int, n int64, w uuid.UUID, p *x509.Certificate) (TaskContext, error) {
	return f(c, t, a, n, w, p)
}

func TestCredentialBodyStrict(t *testing.T) {
	for _, raw := range []string{`null`, `[]`, `{}`, `{"attempt":1,"fence":3}`, `{"attempt":1,"fence":"01"}`, `{"attempt":1,"fence":"9223372036854775808"}`, `{"attempt":0,"fence":"1"}`, `{"attempt":7,"fence":"1"}`, `{"attempt":1,"fence":"1","attempt":2}`, `{"attempt":1,"fence":"1","tenant_id":"x"}`, `{"attempt":1,"fence":"1"} {}`} {
		if _, _, err := credentialBody(strings.NewReader(raw)); err == nil {
			t.Fatalf("accepted invalid credential body %s", raw)
		}
	}
	if a, f, e := credentialBody(strings.NewReader(`{"fence":"9223372036854775807","attempt":6}`)); e != nil || a != 6 || f != 9223372036854775807 {
		t.Fatal("int64 fence lost", e)
	}
}

func TestCredentialsHTTPRequiresLiveLeaseAndVerifiedTLS(t *testing.T) {
	signer, id, cert, now := fixture(t)
	revoked := false
	calls := 0
	h := HTTP{Signer: signer, Tasks: resolverFunc(func(_ context.Context, task uuid.UUID, attempt int, fence int64, worker uuid.UUID, peer *x509.Certificate) (TaskContext, error) {
		calls++
		if revoked || task != id.TaskID || attempt != id.Attempt || fence != id.Fence || worker != id.WorkerID || certificateHash(peer) != certificateHash(cert) {
			return TaskContext{}, ErrCapability
		}
		return TaskContext{Identity: id, LeaseUntil: now.Add(30 * time.Second), Deadline: now.Add(time.Hour)}, nil
	})}
	makeRequest := func(verified bool) *http.Request {
		r := httptest.NewRequest("POST", "/internal/v1/tasks/"+id.TaskID.String()+"/credentials", strings.NewReader(`{"attempt":2,"fence":"9"}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Photo-Worker", id.WorkerID.String())
		r.Header.Set("X-Forwarded-Client-Cert", "forged")
		if verified {
			r.TLS = &tls.ConnectionState{HandshakeComplete: true, PeerCertificates: []*x509.Certificate{cert}, VerifiedChains: [][]*x509.Certificate{{cert}}}
		}
		return r
	}
	w := httptest.NewRecorder()
	h.Handler().ServeHTTP(w, makeRequest(false))
	if w.Code != 401 || calls != 0 {
		t.Fatal("proxy header bypassed mTLS", w.Code, calls)
	}
	w = httptest.NewRecorder()
	h.Handler().ServeHTTP(w, makeRequest(true))
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("credentials failed", w.Code)
	}
	var result credentialsResponse
	if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Identity != id || result.TokenType != "Bearer" || !result.ExpiresAt.Equal(now.Add(30*time.Second)) {
		t.Fatal("credential contract mismatch")
	}
	if _, err := signer.Verify(result.Token, cert, id, WriteOutputs); err != nil {
		t.Fatal(err)
	}
	revoked = true
	w = httptest.NewRecorder()
	h.Handler().ServeHTTP(w, makeRequest(true))
	if w.Code != 403 || calls != 2 {
		t.Fatal("revoked worker received token", w.Code, calls)
	}
	h.Tasks = resolverFunc(func(context.Context, uuid.UUID, int, int64, uuid.UUID, *x509.Certificate) (TaskContext, error) {
		return TaskContext{}, errors.New("private database connection details")
	})
	w = httptest.NewRecorder()
	h.Handler().ServeHTTP(w, makeRequest(true))
	if w.Code != 503 || strings.Contains(w.Body.String(), "private") {
		t.Fatal("DB error leaked")
	}
}

// This uses real TLS sockets; SQL authorization is separately covered against
// PostgreSQL by test_task_leases.py. No claim of an integrated object gateway.
func TestCredentialsRealMTLS(t *testing.T) {
	now := time.Now().UTC()
	caPub, caKey, _ := ed25519.GenerateKey(rand.Reader)
	caTemplate := &x509.Certificate{SerialNumber: big.NewInt(1), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour)}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, caPub, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	clientPub, clientKey, _ := ed25519.GenerateKey(rand.Reader)
	template := &x509.Certificate{SerialNumber: big.NewInt(2), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, template, ca, clientPub, caKey)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	signer, id, _, _ := fixture(t)
	signer.Now = nil
	h := HTTP{Signer: signer, Tasks: resolverFunc(func(_ context.Context, task uuid.UUID, attempt int, fence int64, worker uuid.UUID, peer *x509.Certificate) (TaskContext, error) {
		if task != id.TaskID || worker != id.WorkerID || attempt != id.Attempt || fence != id.Fence || certificateHash(peer) != certificateHash(certificate) {
			return TaskContext{}, ErrCapability
		}
		return TaskContext{Identity: id, LeaseUntil: time.Now().Add(45 * time.Second), Deadline: time.Now().Add(time.Hour)}, nil
	})}
	server := httptest.NewUnstartedServer(h.Handler())
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS12, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: roots}
	server.StartTLS()
	defer server.Close()
	client := server.Client()
	transport := client.Transport.(*http.Transport).Clone()
	transport.TLSClientConfig = transport.TLSClientConfig.Clone()
	transport.TLSClientConfig.Certificates = []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: clientKey}}
	client = &http.Client{Transport: transport, Timeout: 5 * time.Second}
	defer transport.CloseIdleConnections()
	request, _ := http.NewRequest("POST", server.URL+"/internal/v1/tasks/"+id.TaskID.String()+"/credentials", strings.NewReader(`{"attempt":2,"fence":"9"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Photo-Worker", id.WorkerID.String())
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != 200 {
		t.Fatal("mTLS credentials failed", response.StatusCode, err)
	}
	var result credentialsResponse
	if json.Unmarshal(body, &result) != nil {
		t.Fatal("invalid response")
	}
	if _, err := signer.Verify(result.Token, certificate, id, ReadObjects); err != nil {
		t.Fatal("unusable issued capability", err)
	}
	without := server.Client()
	if response, err = without.Get(server.URL + "/internal/v1/tasks/" + id.TaskID.String() + "/credentials"); err == nil {
		response.Body.Close()
		t.Fatal("TLS accepted missing client certificate")
	}
}
