package provenance

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"encoding/hex"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// InstallSigningTimestampServices resolves the pinned TSA catalogue against
// the photo_api SQL CRL checkpoint before exposing any callback. An issued
// timestamp proves live service trust for the exact installed policy/chain;
// signed export bytes still require independent worker C2PA verification.
func InstallSigningTimestampServices(ctx context.Context, entries []SigningAPITimestampDocument,
	api *pgxpool.Pool) (map[string]SigningTrustCheck, func(), error) {
	noop := func() {}
	if err := ctx.Err(); err != nil {
		return nil, noop, err
	}
	if api == nil || len(entries) < 1 || len(entries) > 64 {
		return nil, noop, ErrInvalid
	}
	checks := make(map[string]SigningTrustCheck, len(entries))
	clients := make([]*HTTPSigningTSAClient, 0, len(entries))
	sources := make([]*HTTPSigningCRLSource, 0, len(entries))
	closeAll := func() {
		for _, client := range clients {
			client.Close()
		}
		for _, source := range sources {
			source.Close()
		}
	}
	fail := func(err error) (map[string]SigningTrustCheck, func(), error) { closeAll(); return nil, noop, err }
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		if !text(entry.ID, 120) || checks[entry.ID] != nil || !validSigningDeploymentURL(entry.URL, false) || !digest(entry.ServerSHA) || entry.TimeoutSeconds < 1 || entry.TimeoutSeconds > 30 || entry.MaxSkewSeconds < 1 || entry.MaxSkewSeconds > 600 || entry.CRLMaxAgeSeconds < 60 || entry.CRLMaxAgeSeconds > 86400 || len(entry.CRLEndpoints) < 1 || len(entry.CRLEndpoints) > 64 || signingDeploymentRoots(entry.HTTPRoots) != nil || signingDeploymentRoots(entry.TrustRoots) != nil || signingDeploymentRoots(entry.CRLRoots) != nil {
			return fail(ErrInvalid)
		}
		parsedOID, err := ParseRFC3161PolicyOID(entry.PolicyOID)
		if err != nil {
			return fail(err)
		}
		endpointPins := make([]SigningCRLEndpoint, 0, len(entry.CRLEndpoints))
		issuers := make([]string, 0, len(entry.CRLEndpoints))
		seen := map[string]bool{}
		for _, endpoint := range entry.CRLEndpoints {
			if !digest(endpoint.IssuerSHA) || !digest(endpoint.ServerSHA) || seen[endpoint.IssuerSHA] || !validSigningDeploymentURL(endpoint.URL, false) {
				return fail(ErrInvalid)
			}
			seen[endpoint.IssuerSHA] = true
			endpointPins = append(endpointPins, SigningCRLEndpoint{IssuerSHA: endpoint.IssuerSHA, URL: endpoint.URL, ServerSHA: endpoint.ServerSHA})
			issuers = append(issuers, endpoint.IssuerSHA)
		}
		trustSHA := sha256.Sum256([]byte(entry.TrustRoots))
		checkpoint := SQLSigningCRLCheckpoint{DB: api, TrustBundleSHA: hex.EncodeToString(trustSHA[:])}
		bounded, cancel := context.WithTimeout(ctx, time.Duration(entry.TimeoutSeconds)*time.Second)
		err = checkpoint.CheckSigningCRLInstallation(bounded, checkpoint.TrustBundleSHA, issuers)
		cancel()
		if err != nil {
			return fail(err)
		}
		crlRoots := x509.NewCertPool()
		if !crlRoots.AppendCertsFromPEM([]byte(entry.CRLRoots)) {
			return fail(ErrInvalid)
		}
		source, err := NewHTTPSigningCRLSource(endpointPins, crlRoots, time.Duration(entry.TimeoutSeconds)*time.Second)
		if err != nil {
			return fail(err)
		}
		sources = append(sources, source)
		revocation, err := NewDurableRFC3161TSARevocationCheck([]byte(entry.TrustRoots), source.ReadCRL,
			time.Now, time.Duration(entry.CRLMaxAgeSeconds)*time.Second, checkpoint)
		if err != nil {
			return fail(err)
		}
		trust, err := NewRFC3161TSATrust([]byte(entry.TrustRoots), asn1.ObjectIdentifier(parsedOID),
			time.Now, time.Duration(entry.MaxSkewSeconds)*time.Second, revocation)
		if err != nil {
			return fail(err)
		}
		httpRoots := x509.NewCertPool()
		if !httpRoots.AppendCertsFromPEM([]byte(entry.HTTPRoots)) {
			return fail(ErrInvalid)
		}
		client, err := NewHTTPSigningTSAClient(entry.URL, httpRoots, entry.ServerSHA,
			time.Duration(entry.TimeoutSeconds)*time.Second, trust)
		if err != nil {
			return fail(err)
		}
		clients = append(clients, client)
		serviceID := entry.ID
		checks[serviceID] = func(ctx context.Context, policy SigningPolicy, chain []byte) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if policy.TimestampMode != "required" || policy.TimestampServiceID == nil || *policy.TimestampServiceID != serviceID || len(chain) < 1 || len(chain) > 1<<20 || bodyDigest(chain) != policy.CertificateChainSHA {
				return ErrInvalid
			}
			policyBody, err := canonical.Marshal(policy)
			if err != nil {
				return err
			}
			ref := v2input.Ref{ArtifactID: policy.ID, SHA256: bodyDigest(policyBody)}
			if _, err := DecodeSigningPolicy(policyBody, ref, policy.ID); err != nil {
				return err
			}
			payload, err := canonical.Marshal(struct {
				Domain    string `json:"domain"`
				PolicySHA string `json:"policy_sha256"`
				ChainSHA  string `json:"certificate_chain_sha256"`
			}{"photoeditor.tsa-installation/v1", ref.SHA256, policy.CertificateChainSHA})
			if err != nil {
				return err
			}
			if err := client.Timestamp(ctx, payload); err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if bodyDigest(chain) != policy.CertificateChainSHA {
				return errors.New("TSA certificate chain changed")
			}
			return nil
		}
	}
	return checks, closeAll, nil
}
