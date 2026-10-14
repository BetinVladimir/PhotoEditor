package provenance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"
)

type HTTPSigningTSAClient struct {
	client   *http.Client
	endpoint string
	verify   func(tls.ConnectionState) error
	trust    *RFC3161TSATrust
	lifetime context.Context
	cancel   context.CancelFunc
	closed   atomic.Bool
}

// The HTTP peer is pinned separately from the TSA signing certificate. TLS
// alone cannot approve a timestamp; every reply passes RFC3161TSATrust.
func NewHTTPSigningTSAClient(endpoint string, roots *x509.CertPool, serverSHA string,
	timeout time.Duration, trust *RFC3161TSATrust) (*HTTPSigningTSAClient, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || len(endpoint) > 2048 || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.Opaque != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.RawPath != "" || roots == nil || !digest(serverSHA) || timeout < time.Second || timeout > 30*time.Second || trust == nil || trust.roots == nil {
		return nil, ErrInvalid
	}
	verify := func(state tls.ConnectionState) error {
		if state.Version < tls.VersionTLS13 || len(state.PeerCertificates) == 0 || len(state.VerifiedChains) == 0 {
			return ErrInvalid
		}
		sum := sha256.Sum256(state.PeerCertificates[0].Raw)
		if hex.EncodeToString(sum[:]) != serverSHA {
			return ErrInvalid
		}
		now := time.Now()
		for _, cert := range state.VerifiedChains[0] {
			if now.Before(cert.NotBefore) || !now.Before(cert.NotAfter) {
				return ErrInvalid
			}
		}
		return nil
	}
	transport := &http.Transport{Proxy: nil, DisableCompression: true,
		DialContext:         (&net.Dialer{Timeout: timeout}).DialContext,
		TLSClientConfig:     &tls.Config{RootCAs: roots.Clone(), MinVersion: tls.VersionTLS13, VerifyConnection: verify},
		TLSHandshakeTimeout: timeout, ResponseHeaderTimeout: timeout, IdleConnTimeout: 30 * time.Second,
		MaxIdleConns: 1, MaxIdleConnsPerHost: 1, MaxConnsPerHost: 2, MaxResponseHeaderBytes: 8192}
	client := &http.Client{Transport: transport, Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return ErrInvalid }}
	lifetime, cancel := context.WithCancel(context.Background())
	return &HTTPSigningTSAClient{client: client, endpoint: parsed.String(), verify: verify,
		trust: trust, lifetime: lifetime, cancel: cancel}, nil
}

func (s *HTTPSigningTSAClient) Close() {
	if s == nil || s.closed.Swap(true) {
		return
	}
	if s.cancel != nil {
		s.cancel()
	}
	if s.client != nil {
		s.client.CloseIdleConnections()
	}
}

func (s *HTTPSigningTSAClient) Timestamp(ctx context.Context, payload []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	request, err := NewRFC3161Request(payload)
	if err != nil {
		return err
	}
	return s.exchange(ctx, request)
}

func (s *HTTPSigningTSAClient) exchange(ctx context.Context, request RFC3161Request) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || s.closed.Load() || s.client == nil || s.trust == nil || s.verify == nil || s.lifetime == nil || len(request.DER) < 16 || len(request.DER) > 4096 {
		return ErrInvalid
	}
	scoped, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(s.lifetime, cancel)
	defer stop()
	if s.closed.Load() {
		return ErrInvalid
	}
	httpRequest, err := http.NewRequestWithContext(scoped, http.MethodPost, s.endpoint, bytes.NewReader(request.DER))
	if err != nil {
		return err
	}
	httpRequest.Header.Set("Content-Type", "application/timestamp-query")
	httpRequest.Header.Set("Accept", "application/timestamp-reply")
	response, err := s.client.Do(httpRequest)
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.TLS == nil || s.verify(*response.TLS) != nil || len(response.Header.Values("Content-Type")) != 1 || response.Header.Get("Content-Type") != "application/timestamp-reply" || len(response.Header.Values("Content-Encoding")) != 0 || response.ContentLength > 2<<20 {
		return ErrInvalid
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	if err != nil {
		return err
	}
	if err := scoped.Err(); err != nil {
		return err
	}
	if len(raw) < 16 || len(raw) > 2<<20 || s.closed.Load() {
		return ErrInvalid
	}
	if err := s.trust.Verify(scoped, raw, request); err != nil {
		return err
	}
	if s.closed.Load() {
		return ErrInvalid
	}
	return scoped.Err()
}
