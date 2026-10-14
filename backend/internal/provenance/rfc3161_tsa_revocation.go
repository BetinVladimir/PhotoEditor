package provenance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"time"
)

// NewDurableRFC3161TSARevocationCheck adapts the existing complete signed-CRL
// verifier and durable issuer checkpoint to a verified TSA certificate path.
// The source must be process-pinned; response or job data never supplies URLs.
func NewDurableRFC3161TSARevocationCheck(rootPEM []byte, source SigningCRLSource,
	clock func() time.Time, maxAge time.Duration, checkpoint SigningCRLCheckpoint) (RFC3161TSARevocation, error) {
	if source == nil || checkpoint == nil {
		return nil, ErrInvalid
	}
	ownedRoots := append([]byte(nil), rootPEM...)
	anchors, err := signingTrustCertificates(ownedRoots)
	if err != nil {
		return nil, err
	}
	for _, anchor := range anchors {
		if !anchor.IsCA || !anchor.BasicConstraintsValid || anchor.KeyUsage&x509.KeyUsageCertSign == 0 || !bytes.Equal(anchor.RawIssuer, anchor.RawSubject) || anchor.CheckSignatureFrom(anchor) != nil {
			return nil, ErrInvalid
		}
	}
	check, err := NewDurableSigningCRLCheck(ownedRoots, source, clock, maxAge, checkpoint)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, path [][]byte) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(path) < 2 || len(path) > 16 {
			return ErrInvalid
		}
		certs := make([]*x509.Certificate, len(path))
		chain := make([]byte, 0, 16<<10)
		for i, raw := range path {
			if len(raw) < 1 || len(raw) > 1<<20 || len(chain)+len(raw) > 1<<20 {
				return ErrInvalid
			}
			cert, err := x509.ParseCertificate(append([]byte(nil), raw...))
			if err != nil {
				return ErrInvalid
			}
			certs[i] = cert
			chain = append(chain, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})...)
		}
		root := certs[len(certs)-1]
		pinned := false
		for _, anchor := range anchors {
			if bytes.Equal(anchor.Raw, root.Raw) {
				pinned = true
				break
			}
		}
		if !pinned {
			return ErrInvalid
		}
		for i := 0; i+1 < len(certs); i++ {
			if !bytes.Equal(certs[i].RawIssuer, certs[i+1].RawSubject) || certs[i].CheckSignatureFrom(certs[i+1]) != nil {
				return ErrInvalid
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		sum := sha256.Sum256(chain)
		policy := SigningPolicy{RevocationMode: "required", CertificateChainSHA: hex.EncodeToString(sum[:])}
		return check(ctx, policy, chain)
	}, nil
}
