package provenance

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// Public provisioning metadata only. IdentityID resolves an API mTLS identity
// from trusted process inputs; signing private keys/native callbacks are absent.
type SigningAPIDeploymentDocument struct {
	Schema  string                     `json:"schema_version"`
	Tenants []SigningAPITenantDocument `json:"tenants"`
}
type SigningAPITenantDocument struct {
	TenantID           uuid.UUID                     `json:"tenant_id"`
	WorkerID           uuid.UUID                     `json:"worker_id"`
	QualificationSHA   string                        `json:"qualification_sha"`
	CertificateChain   string                        `json:"certificate_chain_pem"`
	TrustBundle        string                        `json:"trust_bundle_pem"`
	CertificateUsages  []int                         `json:"certificate_usages"`
	ChallengeOrigin    string                        `json:"challenge_origin"`
	ChallengeServerSHA string                        `json:"challenge_server_sha"`
	ChallengeRoots     string                        `json:"challenge_roots_pem"`
	IdentityID         string                        `json:"api_identity_id"`
	TimeoutSeconds     int                           `json:"timeout_seconds"`
	CRLMaxAgeSeconds   int                           `json:"crl_max_age_seconds"`
	CRLRoots           string                        `json:"crl_roots_pem"`
	CRLEndpoints       []SigningAPICRLDocument       `json:"crl_endpoints"`
	TimestampServices  []SigningAPITimestampDocument `json:"timestamp_services,omitempty"`
	Policies           []SigningAPIPolicyDocument    `json:"policies"`
	ResourceReport     json.RawMessage               `json:"resource_report"`
	ResourceReportSHA  string                        `json:"resource_report_sha"`
	NegativePolicies   json.RawMessage               `json:"negative_policies"`
}
type SigningAPICRLDocument struct {
	IssuerSHA string `json:"issuer_sha"`
	URL       string `json:"url"`
	ServerSHA string `json:"server_sha"`
}
type SigningAPITimestampDocument struct {
	ID               string                  `json:"id"`
	URL              string                  `json:"url"`
	ServerSHA        string                  `json:"server_sha"`
	HTTPRoots        string                  `json:"http_roots_pem"`
	TrustRoots       string                  `json:"trust_roots_pem"`
	PolicyOID        string                  `json:"policy_oid"`
	TimeoutSeconds   int                     `json:"timeout_seconds"`
	MaxSkewSeconds   int                     `json:"max_skew_seconds"`
	CRLRoots         string                  `json:"crl_roots_pem"`
	CRLMaxAgeSeconds int                     `json:"crl_max_age_seconds"`
	CRLEndpoints     []SigningAPICRLDocument `json:"crl_endpoints"`
}

func ParseRFC3161PolicyOID(value string) ([]int, error) {
	if len(value) < 3 || len(value) > 120 || strings.TrimSpace(value) != value {
		return nil, ErrInvalid
	}
	parts := strings.Split(value, ".")
	if len(parts) < 3 || len(parts) > 16 {
		return nil, ErrInvalid
	}
	oid := make([]int, len(parts))
	for i, part := range parts {
		parsed, err := strconv.ParseUint(part, 10, 31)
		if err != nil || strconv.FormatUint(parsed, 10) != part {
			return nil, ErrInvalid
		}
		oid[i] = int(parsed)
	}
	if oid[0] > 2 || oid[0] < 0 || oid[0] < 2 && oid[1] > 39 {
		return nil, ErrInvalid
	}
	return oid, nil
}

type SigningAPIPolicyDocument struct {
	Body      json.RawMessage `json:"body"`
	NotBefore string          `json:"not_before"`
	NotAfter  string          `json:"not_after"`
}

func validSigningDeploymentURL(value string, origin bool) bool {
	parsed, err := url.Parse(value)
	return err == nil && len(value) <= 2048 && parsed.Scheme == "https" && parsed.Hostname() != "" && parsed.User == nil && parsed.Opaque == "" && parsed.RawQuery == "" && !parsed.ForceQuery && parsed.Fragment == "" && parsed.RawPath == "" && (!origin || parsed.Path == "" || parsed.Path == "/")
}
func signingDeploymentRoots(raw string) error {
	certs, err := signingTrustCertificates([]byte(raw))
	if err != nil {
		return err
	}
	for _, cert := range certs {
		if !cert.IsCA || !cert.BasicConstraintsValid || cert.KeyUsage&x509.KeyUsageCertSign == 0 {
			return ErrInvalid
		}
	}
	return nil
}
func DecodeSigningAPIDeploymentDocument(ctx context.Context, raw []byte, expectedSHA string) (SigningAPIDeploymentDocument, error) {
	var zero, doc SigningAPIDeploymentDocument
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if len(raw) < 2 || len(raw) > 8<<20 || !digest(expectedSHA) || bodyDigest(raw) != expectedSHA || decode(raw, &doc, 8<<20) != nil || doc.Schema != "signing-api-deployment/v1" || len(doc.Tenants) < 1 || len(doc.Tenants) > 256 {
		return zero, ErrInvalid
	}
	tenants := map[uuid.UUID]bool{}
	for _, entry := range doc.Tenants {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		if entry.TenantID == uuid.Nil || entry.WorkerID == uuid.Nil || tenants[entry.TenantID] || !digest(entry.QualificationSHA) || !digest(entry.ChallengeServerSHA) || !text(entry.IdentityID, 120) || !validSigningDeploymentURL(entry.ChallengeOrigin, true) || entry.TimeoutSeconds < 1 || entry.TimeoutSeconds > 30 || len(entry.CertificateUsages) < 1 || len(entry.CertificateUsages) > 16 || len(entry.Policies) < 1 || len(entry.Policies) > MaxInstalledSigningPolicies || len(entry.CRLEndpoints) > 64 || len(entry.TimestampServices) > 64 {
			return zero, ErrInvalid
		}
		tenants[entry.TenantID] = true
		if _, err := signingTrustCertificates([]byte(entry.CertificateChain)); err != nil {
			return zero, err
		}
		if signingDeploymentRoots(entry.TrustBundle) != nil || signingDeploymentRoots(entry.ChallengeRoots) != nil {
			return zero, ErrInvalid
		}
		usages := map[int]bool{}
		for _, usage := range entry.CertificateUsages {
			if usage <= int(x509.ExtKeyUsageAny) || usage > int(x509.ExtKeyUsageMicrosoftKernelCodeSigning) || usages[usage] {
				return zero, ErrInvalid
			}
			usages[usage] = true
		}
		issuers := map[string]bool{}
		if len(entry.CRLEndpoints) > 0 {
			if entry.CRLMaxAgeSeconds < 60 || entry.CRLMaxAgeSeconds > 86400 || signingDeploymentRoots(entry.CRLRoots) != nil {
				return zero, ErrInvalid
			}
		} else if entry.CRLMaxAgeSeconds != 0 || entry.CRLRoots != "" {
			return zero, ErrInvalid
		}
		for _, endpoint := range entry.CRLEndpoints {
			if !digest(endpoint.IssuerSHA) || !digest(endpoint.ServerSHA) || issuers[endpoint.IssuerSHA] || !validSigningDeploymentURL(endpoint.URL, false) {
				return zero, ErrInvalid
			}
			issuers[endpoint.IssuerSHA] = true
		}
		ids := map[uuid.UUID]bool{}
		sdk := ""
		requiredTSA := map[string]bool{}
		for _, installed := range entry.Policies {
			var header SigningPolicy
			if decode(installed.Body, &header, 1<<20) != nil {
				return zero, ErrInvalid
			}
			ref := v2input.Ref{ArtifactID: header.ID, SHA256: bodyDigest(installed.Body)}
			policy, err := DecodeSigningPolicy(installed.Body, ref, header.ID)
			if err != nil {
				return zero, err
			}
			from, e1 := time.Parse(time.RFC3339Nano, installed.NotBefore)
			until, e2 := time.Parse(time.RFC3339Nano, installed.NotAfter)
			if e1 != nil || e2 != nil || from.IsZero() || !until.After(from) || ids[policy.ID] || policy.CertificateChainSHA != bodyDigest([]byte(entry.CertificateChain)) || policy.TrustBundleSHA != bodyDigest([]byte(entry.TrustBundle)) || (sdk != "" && sdk != policy.SDKLockSHA) || (policy.RevocationMode == "required" && len(issuers) == 0) {
				return zero, ErrInvalid
			}
			ids[policy.ID] = true
			sdk = policy.SDKLockSHA
			if policy.TimestampMode == "required" {
				requiredTSA[*policy.TimestampServiceID] = true
			}
		}
		declaredTSA := map[string]bool{}
		for _, service := range entry.TimestampServices {
			if !text(service.ID, 120) || declaredTSA[service.ID] || !requiredTSA[service.ID] || !validSigningDeploymentURL(service.URL, false) || !digest(service.ServerSHA) || signingDeploymentRoots(service.HTTPRoots) != nil || signingDeploymentRoots(service.TrustRoots) != nil || signingDeploymentRoots(service.CRLRoots) != nil || service.TimeoutSeconds < 1 || service.TimeoutSeconds > 30 || service.MaxSkewSeconds < 1 || service.MaxSkewSeconds > 600 || service.CRLMaxAgeSeconds < 60 || service.CRLMaxAgeSeconds > 86400 || len(service.CRLEndpoints) < 1 || len(service.CRLEndpoints) > 64 {
				return zero, ErrInvalid
			}
			if _, err := ParseRFC3161PolicyOID(service.PolicyOID); err != nil {
				return zero, ErrInvalid
			}
			declaredTSA[service.ID] = true
			issuers := map[string]bool{}
			for _, endpoint := range service.CRLEndpoints {
				if !digest(endpoint.IssuerSHA) || !digest(endpoint.ServerSHA) || issuers[endpoint.IssuerSHA] || !validSigningDeploymentURL(endpoint.URL, false) {
					return zero, ErrInvalid
				}
				issuers[endpoint.IssuerSHA] = true
			}
		}
		if len(declaredTSA) != len(requiredTSA) {
			return zero, ErrInvalid
		}
		var resources SigningResourceMeasurements
		if !digest(entry.ResourceReportSHA) || bodyDigest(entry.ResourceReport) != entry.ResourceReportSHA || decode(entry.ResourceReport, &resources, 8<<20) != nil || resources.Schema != "signing-resource-measurements/v1" || resources.SDK != sdk || !digest(resources.Runtime) || !digest(resources.Contracts) || !digest(resources.Hardware) {
			return zero, ErrInvalid
		}
		if string(entry.NegativePolicies) != "null" {
			if _, err := DecodeSigningNegativePolicyDocument(ctx, entry.NegativePolicies, bodyDigest(entry.NegativePolicies)); err != nil {
				return zero, err
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return doc, nil
}
