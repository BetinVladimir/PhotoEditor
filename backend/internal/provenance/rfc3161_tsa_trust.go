package provenance

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/asn1"
	"time"
)

type RFC3161TSARevocation func(context.Context, [][]byte) error

type RFC3161TSATrust struct {
	roots      *x509.CertPool
	policy     asn1.ObjectIdentifier
	clock      func() time.Time
	maxSkew    time.Duration
	revocation RFC3161TSARevocation
}

// NewRFC3161TSATrust installs pinned TSA roots and an independent live
// revocation authority. Passing a nil source never enables timestamp trust.
func NewRFC3161TSATrust(rootPEM []byte, policy asn1.ObjectIdentifier,
	clock func() time.Time, maxSkew time.Duration, revocation RFC3161TSARevocation) (*RFC3161TSATrust, error) {
	if len(policy) < 2 || clock == nil || maxSkew < time.Second || maxSkew > 10*time.Minute || revocation == nil {
		return nil, ErrInvalid
	}
	certs, err := signingTrustCertificates(append([]byte(nil), rootPEM...))
	if err != nil {
		return nil, err
	}
	roots := x509.NewCertPool()
	for _, cert := range certs {
		if !cert.IsCA || !cert.BasicConstraintsValid || cert.KeyUsage&x509.KeyUsageCertSign == 0 || !bytes.Equal(cert.RawIssuer, cert.RawSubject) || cert.CheckSignatureFrom(cert) != nil {
			return nil, ErrInvalid
		}
		roots.AddCert(cert)
	}
	return &RFC3161TSATrust{roots: roots, policy: append(asn1.ObjectIdentifier(nil), policy...), clock: clock, maxSkew: maxSkew, revocation: revocation}, nil
}

func (v *RFC3161TSATrust) Verify(ctx context.Context, response []byte, request RFC3161Request) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if v == nil || v.roots == nil || v.clock == nil || v.revocation == nil {
		return ErrInvalid
	}
	token, err := parseRFC3161GrantedResponse(response)
	if err != nil {
		return err
	}
	parts, err := parseRFC3161CMSParts(token)
	if err != nil {
		return err
	}
	signer, err := verifyRFC3161CMSSignature(parts)
	if err != nil {
		return err
	}
	binding, err := parseRFC3161TSTInfo(parts.TSTInfo, request)
	if err != nil || !binding.Policy.Equal(v.policy) {
		return ErrInvalid
	}
	checkTime := func() error {
		now := v.clock()
		if now.IsZero() || binding.Generated.Before(now.Add(-v.maxSkew)) || binding.Generated.After(now.Add(v.maxSkew)) {
			return ErrInvalid
		}
		return ctx.Err()
	}
	if err := checkTime(); err != nil {
		return err
	}
	verifyPath := func() ([][]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		leaf, err := x509.ParseCertificate(append([]byte(nil), signer.Raw...))
		if err != nil || leaf.IsCA || leaf.KeyUsage&x509.KeyUsageDigitalSignature == 0 || len(leaf.ExtKeyUsage) != 1 || leaf.ExtKeyUsage[0] != x509.ExtKeyUsageTimeStamping || len(leaf.UnknownExtKeyUsage) != 0 {
			return nil, ErrInvalid
		}
		criticalEKU := false
		for _, ext := range leaf.Extensions {
			if ext.Id.Equal(asn1.ObjectIdentifier{2, 5, 29, 37}) {
				if criticalEKU || !ext.Critical {
					return nil, ErrInvalid
				}
				criticalEKU = true
			}
		}
		if !criticalEKU {
			return nil, ErrInvalid
		}
		intermediates := x509.NewCertPool()
		for _, raw := range parts.Certificates {
			if bytes.Equal(raw, leaf.Raw) {
				continue
			}
			cert, err := x509.ParseCertificate(append([]byte(nil), raw...))
			if err != nil || !cert.IsCA || !cert.BasicConstraintsValid || cert.KeyUsage&x509.KeyUsageCertSign == 0 {
				return nil, ErrInvalid
			}
			intermediates.AddCert(cert)
		}
		chains, err := leaf.Verify(x509.VerifyOptions{Roots: v.roots, Intermediates: intermediates,
			CurrentTime: v.clock(), KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageTimeStamping}})
		if err != nil || len(chains) == 0 {
			return nil, ErrInvalid
		}
		path := make([][]byte, len(chains[0]))
		for i, cert := range chains[0] {
			path[i] = append([]byte(nil), cert.Raw...)
		}
		return path, nil
	}
	path, err := verifyPath()
	if err != nil {
		return err
	}
	if err := v.revocation(ctx, path); err != nil {
		return err
	}
	if err := checkTime(); err != nil {
		return err
	}
	_, err = verifyPath()
	return err
}
