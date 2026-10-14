package provenance

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"sync/atomic"
	"time"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// IndependentTrust must verify actual chain/root/revocation/TSA state and must
// not call back into this adapter (which would form a recursive trust cycle).
// Native inspection occurs inside the process-pinned remote signer endpoint
// before/after key invocation. This is not hardware remote attestation.
type RemoteSigningNativeInstallation struct {
	Policies          [][]byte
	CertificateChain  []byte
	Origin, ServerSHA string
	Roots             *x509.CertPool
	Identity          tls.Certificate
	Timeout           time.Duration
	IndependentTrust  SigningLiveTrustAuthority
	Clock             func() time.Time
}
type RemoteSigningNativeTrust struct {
	policies  map[string][]byte
	chain     []byte
	proof     *ChallengeSigningOperatorAuthority
	transport *HTTPSigningChallengeSigner
	lifetime  context.Context
	cancel    context.CancelFunc
	closed    atomic.Bool
}

// The context-local stack rejects direct and indirect authority cycles without
// sharing mutable invocation state between concurrent requests.
type remoteSigningTrustContextKey struct{}
type remoteSigningTrustInvocation struct {
	authority *RemoteSigningNativeTrust
	parent    *remoteSigningTrustInvocation
}

func NewRemoteSigningNativeTrust(config RemoteSigningNativeInstallation) (*RemoteSigningNativeTrust, error) {
	if len(config.Policies) < 1 || len(config.Policies) > MaxInstalledSigningPolicies || config.IndependentTrust == nil || config.Clock == nil || len(config.CertificateChain) < 1 || len(config.CertificateChain) > 1<<20 {
		return nil, ErrInvalid
	}
	policies := map[string][]byte{}
	ids := map[string]bool{}
	total := 0
	sdk := ""
	trust := ""
	chain := append([]byte(nil), config.CertificateChain...)
	for _, raw := range config.Policies {
		if len(raw) < 2 || len(raw) > 1<<20 || len(raw) > (8<<20)-total {
			return nil, ErrInvalid
		}
		total += len(raw)
		var header SigningPolicy
		if decode(raw, &header, 1<<20) != nil {
			return nil, ErrInvalid
		}
		ref := v2input.Ref{ArtifactID: header.ID, SHA256: bodyDigest(raw)}
		policy, err := DecodeSigningPolicy(raw, ref, header.ID)
		if err != nil {
			return nil, err
		}
		if ids[policy.ID.String()] || policy.CertificateChainSHA != bodyDigest(chain) || (sdk != "" && sdk != policy.SDKLockSHA) || (trust != "" && trust != policy.TrustBundleSHA) {
			return nil, ErrInvalid
		}
		ids[policy.ID.String()] = true
		sdk = policy.SDKLockSHA
		trust = policy.TrustBundleSHA
		policies[ref.SHA256] = append([]byte(nil), raw...)
	}
	transport, err := NewHTTPSigningChallengeSigner(config.Origin, config.Roots, config.Identity, config.ServerSHA, config.Timeout)
	if err != nil {
		return nil, err
	}
	proof, err := NewChallengeSigningOperatorAuthority(chain, transport, config.IndependentTrust, config.Clock)
	if err != nil {
		transport.Close()
		return nil, err
	}
	lifetime, cancel := context.WithCancel(context.Background())
	return &RemoteSigningNativeTrust{policies: policies, chain: chain, proof: proof, transport: transport, lifetime: lifetime, cancel: cancel}, nil
}
func (s *RemoteSigningNativeTrust) Check(ctx context.Context, policy SigningPolicy, chain []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || s.closed.Load() || s.proof == nil || !bytes.Equal(s.chain, chain) {
		return ErrInvalid
	}
	parent, _ := ctx.Value(remoteSigningTrustContextKey{}).(*remoteSigningTrustInvocation)
	depth := 0
	for call := parent; call != nil; call = call.parent {
		depth++
		if call.authority == s || depth >= 16 {
			return ErrInvalid
		}
	}
	ctx = context.WithValue(ctx, remoteSigningTrustContextKey{}, &remoteSigningTrustInvocation{authority: s, parent: parent})
	raw, err := canonical.Marshal(policy)
	if err != nil {
		return err
	}
	expected, ok := s.policies[bodyDigest(raw)]
	if !ok || !bytes.Equal(expected, raw) {
		return ErrInvalid
	}
	scoped, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(s.lifetime, cancel)
	defer stop()
	if s.closed.Load() {
		return ErrInvalid
	}
	if err = s.proof.CheckSigningOperator(scoped, policy); err != nil {
		return err
	}
	if s.closed.Load() {
		return ErrInvalid
	}
	return scoped.Err()
}
func (s *RemoteSigningNativeTrust) Close() {
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
