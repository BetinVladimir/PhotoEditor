package gateway

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestObjectRequestAuthorization(t *testing.T) {
	signer, id, cert, now := fixture(t)
	raw, err := signer.Issue(id, cert, now.Add(time.Minute), now.Add(time.Hour), []string{ReadObjects})
	if err != nil {
		t.Fatal(err)
	}
	revoked := false
	h := HTTP{Signer: signer, Tasks: resolverFunc(func(context.Context, uuid.UUID, int, int64, uuid.UUID, *x509.Certificate) (TaskContext, error) {
		if revoked {
			return TaskContext{}, ErrCapability
		}
		return TaskContext{Identity: id}, nil
	})}
	request := func() *http.Request {
		r := httptest.NewRequest("GET", "/", nil)
		r.SetPathValue("task", id.TaskID.String())
		r.Header.Set("X-Photo-Worker", id.WorkerID.String())
		r.Header.Set("X-Photo-Attempt", "2")
		r.Header.Set("X-Photo-Fence", "9")
		r.Header.Set("Authorization", "Bearer "+raw)
		r.TLS = &tls.ConnectionState{HandshakeComplete: true, PeerCertificates: []*x509.Certificate{cert}, VerifiedChains: [][]*x509.Certificate{{cert}}}
		return r
	}
	if got, err := h.AuthorizeRequest(request(), ReadObjects); err != nil || got.Identity != id {
		t.Fatal("valid request rejected", err)
	}
	if _, err := h.AuthorizeRequest(request(), WriteOutputs); err == nil {
		t.Fatal("read token grants writes")
	}
	for _, mutate := range []func(*http.Request){
		func(r *http.Request) { r.Header.Add("Authorization", "Bearer "+raw) },
		func(r *http.Request) { r.Header.Set("X-Photo-Fence", "10") },
		func(r *http.Request) { r.Header.Set("X-Photo-Attempt", "02") },
		func(r *http.Request) { r.Header.Set("X-Photo-Worker", uuid.NewString()) },
		func(r *http.Request) { r.SetPathValue("task", uuid.NewString()) },
		func(r *http.Request) { r.TLS = nil },
	} {
		r := request()
		mutate(r)
		if _, err := h.AuthorizeRequest(r, ReadObjects); err == nil {
			t.Fatal("spoofed identity accepted")
		}
	}
	revoked = true
	if _, err := h.AuthorizeRequest(request(), ReadObjects); err == nil {
		t.Fatal("signed token bypassed live revocation")
	}
}
