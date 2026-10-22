package provenance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"encoding/hex"
	"math/big"
	"sync"
	"time"
)

// Source returns a complete DER CRL for the exact issuer certificate supplied.
// It must use operator-installed locations, never job-provided distribution URLs.
type SigningCRLSource func(context.Context, []byte) ([]byte, error)

type SigningCRLInstallationCheckpoint interface {
	SigningCRLCheckpoint
	CheckSigningCRLInstallation(context.Context, string, []string) error
}
type SigningCRLCheckpoint interface {
	ObserveSigningCRL(context.Context, string, string, time.Time, string) error
}

func NewSigningCRLCheck(trustedIssuers []byte, source SigningCRLSource, clock func() time.Time, maxAge time.Duration) (SigningTrustCheck, error) {
	return newSigningCRLCheck(trustedIssuers, source, clock, maxAge, nil)
}
func NewDurableSigningCRLCheck(trustedIssuers []byte, source SigningCRLSource, clock func() time.Time, maxAge time.Duration, checkpoint SigningCRLCheckpoint) (SigningTrustCheck, error) {
	if checkpoint == nil {
		return nil, ErrInvalid
	}
	return newSigningCRLCheck(trustedIssuers, source, clock, maxAge, checkpoint)
}
func newSigningCRLCheck(trustedIssuers []byte, source SigningCRLSource, clock func() time.Time, maxAge time.Duration, checkpoint SigningCRLCheckpoint) (SigningTrustCheck, error) {
	if source == nil || clock == nil || maxAge < time.Minute || maxAge > 24*time.Hour {
		return nil, ErrInvalid
	}
	trusted, err := signingTrustCertificates(append([]byte(nil), trustedIssuers...))
	if err != nil {
		return nil, err
	}
	for _, cert := range trusted {
		if !cert.IsCA || cert.KeyUsage&x509.KeyUsageCertSign == 0 {
			return nil, ErrInvalid
		}
	}
	type observedCheckpoint struct {
		number     *big.Int
		thisUpdate time.Time
		sha        [32]byte
	}
	checkpoints := map[[32]byte]observedCheckpoint{}
	var mutex sync.Mutex
	remember := func(issuer *x509.Certificate, crl *x509.RevocationList) error {
		issuerSHA, crlSHA := sha256.Sum256(issuer.Raw), sha256.Sum256(crl.Raw)
		mutex.Lock()
		defer mutex.Unlock()
		previous, exists := checkpoints[issuerSHA]
		if exists {
			compared := crl.Number.Cmp(previous.number)
			if compared < 0 || crl.ThisUpdate.Before(previous.thisUpdate) || (compared == 0 && crlSHA != previous.sha) {
				return ErrInvalid
			}
		} else if len(checkpoints) >= 8192 {
			return ErrInvalid
		}
		checkpoints[issuerSHA] = observedCheckpoint{new(big.Int).Set(crl.Number), crl.ThisUpdate, crlSHA}
		return nil
	}
	return func(ctx context.Context, policy SigningPolicy, chain []byte) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if policy.RevocationMode != "required" {
			return ErrInvalid
		}
		owned := append([]byte(nil), chain...)
		sum := sha256.Sum256(owned)
		if hex.EncodeToString(sum[:]) != policy.CertificateChainSHA {
			return ErrInvalid
		}
		certs, err := signingTrustCertificates(owned)
		if err != nil {
			return err
		}
		issuers := append(append([]*x509.Certificate(nil), certs...), trusted...)
		for _, cert := range certs {
			if err := ctx.Err(); err != nil {
				return err
			}
			anchor := false
			for _, root := range trusted {
				if bytes.Equal(root.Raw, cert.Raw) && bytes.Equal(cert.RawIssuer, cert.RawSubject) && cert.CheckSignatureFrom(cert) == nil {
					anchor = true
					break
				}
			}
			if anchor {
				continue
			} // Root trust removal is an independent live trust action.
			var issuer *x509.Certificate
			for _, candidate := range issuers {
				if bytes.Equal(cert.RawIssuer, candidate.RawSubject) && cert.CheckSignatureFrom(candidate) == nil {
					issuer = candidate
					break
				}
			}
			if issuer == nil || issuer.KeyUsage&x509.KeyUsageCRLSign == 0 {
				return ErrInvalid
			}
			raw, err := source(ctx, append([]byte(nil), issuer.Raw...))
			if err != nil {
				return err
			}
			if err = ctx.Err(); err != nil {
				return err
			}
			if len(raw) < 1 || len(raw) > 2<<20 {
				return ErrInvalid
			}
			crl, err := x509.ParseRevocationList(append([]byte(nil), raw...))
			if err != nil {
				return ErrInvalid
			}
			if !bytes.Equal(crl.RawIssuer, issuer.RawSubject) || crl.CheckSignatureFrom(issuer) != nil || crl.Number == nil || crl.Number.Sign() < 0 || crl.Number.BitLen() > 160 {
				return ErrInvalid
			}
			// Delta, indirect and partitioned CRLs cannot establish complete coverage.
			for _, extension := range crl.Extensions {
				if extension.Critical || extension.Id.Equal(asn1.ObjectIdentifier{2, 5, 29, 27}) || extension.Id.Equal(asn1.ObjectIdentifier{2, 5, 29, 28}) {
					return ErrInvalid
				}
			}
			now := clock()
			if now.IsZero() || crl.ThisUpdate.IsZero() || now.Before(crl.ThisUpdate) || now.Sub(crl.ThisUpdate) > maxAge || crl.NextUpdate.IsZero() || !now.Before(crl.NextUpdate) || !crl.ThisUpdate.Before(crl.NextUpdate) {
				return ErrInvalid
			}
			if now.Before(issuer.NotBefore) || !now.Before(issuer.NotAfter) {
				return ErrInvalid
			}
			revoked := false
			for _, entry := range crl.RevokedCertificateEntries {
				for _, extension := range entry.Extensions {
					if extension.Critical || extension.Id.Equal(asn1.ObjectIdentifier{2, 5, 29, 29}) {
						return ErrInvalid
					}
				}
				if entry.SerialNumber == nil {
					return ErrInvalid
				}
				if entry.SerialNumber.Cmp(cert.SerialNumber) == 0 {
					revoked = true
				}
			}
			if err = ctx.Err(); err != nil {
				return err
			}
			// Remember a valid newer list even when it revokes this certificate: an
			// older clean list must never rescue the same issuer after that observation.
			if checkpoint != nil {
				issuerSHA, crlSHA := sha256.Sum256(issuer.Raw), sha256.Sum256(crl.Raw)
				if err = checkpoint.ObserveSigningCRL(ctx, hex.EncodeToString(issuerSHA[:]), crl.Number.String(), crl.ThisUpdate, hex.EncodeToString(crlSHA[:])); err != nil {
					return err
				}
			}
			if err = remember(issuer, crl); err != nil {
				return err
			}
			if revoked {
				return ErrInvalid
			}
		}
		return ctx.Err()
	}, nil
}
