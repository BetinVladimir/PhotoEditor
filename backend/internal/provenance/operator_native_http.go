package provenance

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// HTTPSigningNativeCheck asks the process-pinned signer to inspect its live
// SDK/key installation. Certificate chain, revocation and TSA checks remain
// independent requirements of CertificateSigningTrustAuthority.
type HTTPSigningNativeCheck struct {
	transport *HTTPSigningChallengeSigner
	endpoint  string
	lifetime  context.Context
	cancel    context.CancelFunc
	closed    atomic.Bool
}

func NewHTTPSigningNativeCheck(origin string, roots *x509.CertPool, identity tls.Certificate,
	serverSHA string, timeout time.Duration) (*HTTPSigningNativeCheck, error) {
	transport, err := NewHTTPSigningChallengeSigner(origin, roots, identity, serverSHA, timeout)
	if err != nil {
		return nil, err
	}
	endpoint, err := url.Parse(transport.endpoint)
	if err != nil {
		transport.Close()
		return nil, ErrInvalid
	}
	endpoint.Path = "/internal/v1/operator-native-check"
	lifetime, cancel := context.WithCancel(context.Background())
	return &HTTPSigningNativeCheck{transport: transport, endpoint: endpoint.String(),
		lifetime: lifetime, cancel: cancel}, nil
}

func (s *HTTPSigningNativeCheck) Close() {
	if s == nil || s.closed.Swap(true) {
		return
	}
	if s.cancel != nil {
		s.cancel()
	}
	if s.transport != nil {
		s.transport.Close()
	}
}

func (s *HTTPSigningNativeCheck) Check(ctx context.Context, policy SigningPolicy, chain []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || s.closed.Load() || s.lifetime == nil || s.transport == nil || s.transport.client == nil ||
		s.transport.verify == nil || len(chain) < 1 || len(chain) > 1<<20 {
		return ErrInvalid
	}
	scoped, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(s.lifetime, cancel)
	defer stop()
	if s.closed.Load() {
		return ErrInvalid
	}
	raw, err := canonical.Marshal(policy)
	if err != nil {
		return err
	}
	policySHA := bodyDigest(raw)
	if _, err := DecodeSigningPolicy(raw,
		v2input.Ref{ArtifactID: policy.ID, SHA256: policySHA}, policy.ID); err != nil {
		return err
	}
	if bodyDigest(chain) != policy.CertificateChainSHA {
		return ErrInvalid
	}
	body, err := canonical.Marshal(struct {
		Schema    int    `json:"schema_version"`
		PolicySHA string `json:"policy_sha256"`
		ChainSHA  string `json:"certificate_chain_sha256"`
	}{1, policySHA, policy.CertificateChainSHA})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(scoped, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := s.transport.client.Do(request)
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.TLS == nil ||
		s.transport.verify(*response.TLS) != nil ||
		response.Header.Get("Content-Type") != "application/json" ||
		response.Header.Get("Content-Encoding") != "" || response.ContentLength > 4096 {
		return ErrInvalid
	}
	actual, err := io.ReadAll(io.LimitReader(response.Body, 4097))
	if err != nil {
		return err
	}
	if s.closed.Load() {
		return ErrInvalid
	}
	if err := scoped.Err(); err != nil {
		return err
	}
	if !bytes.Equal(actual, body) {
		return ErrInvalid
	}
	return nil
}

var _ SigningTrustCheck = (*HTTPSigningNativeCheck)(nil).Check
