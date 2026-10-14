package provenance

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"sort"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

type SigningAPITenantRuntime struct {
	WorkerID                     uuid.UUID
	QualificationSHA, IdentityID string
	Identity                     tls.Certificate
	Native, Timestamp            SigningTrustCheck
	// Optional exact service catalogue; mutually exclusive with Timestamp.
	TimestampServices map[string]SigningTrustCheck
}
type SigningAPIDeploymentRuntime struct {
	// Only shared SQL/object/bucket/color/clock dependencies belong in Base.
	Base    PresentationSigningServiceInstallation
	Tenants map[uuid.UUID]SigningAPITenantRuntime
}

func InstallSigningAPIDeployment(ctx context.Context, raw []byte, expectedSHA string, runtime SigningAPIDeploymentRuntime) (*TenantSigningRouter, error) {
	configs, err := prepareSigningAPIDeployment(ctx, raw, expectedSHA, runtime)
	if err != nil {
		return nil, err
	}
	return NewInstalledTenantSigningRouter(ctx, configs)
}
func InstallSigningAPIDeploymentFile(ctx context.Context, path, expectedSHA string, runtime SigningAPIDeploymentRuntime) (*TenantSigningRouter, error) {
	raw, err := readPinnedSigningDeployment(ctx, path, expectedSHA)
	if err != nil {
		return nil, err
	}
	return InstallSigningAPIDeployment(ctx, raw, expectedSHA, runtime)
}
func prepareSigningAPIDeployment(ctx context.Context, raw []byte, expectedSHA string, runtime SigningAPIDeploymentRuntime) ([]TenantSigningInstallation, error) {
	doc, err := DecodeSigningAPIDeploymentDocument(ctx, raw, expectedSHA)
	if err != nil {
		return nil, err
	}
	base := runtime.Base
	if base.API == nil || base.Scheduler == nil || base.API == base.Scheduler || base.Reads == nil || base.Writes == nil || base.Clock == nil || !digest(base.WorkICCSHA) || !text(base.Bucket, 63) || len(base.Bucket) < 3 || base.Authority != nil || base.Policies.SDKLockSHA != "" || len(base.Policies.Policies) != 0 || len(base.ResourceReport) != 0 || base.NegativePolicies != nil || base.Resources != (SigningResourceInstallation{}) {
		return nil, ErrInvalid
	}
	// Resolve every trusted runtime declaration before constructing any listener,
	// database authority or network client. No JSON-selected callback/import hook.
	handles := make(map[uuid.UUID]SigningAPITenantRuntime, len(doc.Tenants))
	for _, entry := range doc.Tenants {
		installed, ok := runtime.Tenants[entry.TenantID]
		if !ok || installed.WorkerID != entry.WorkerID || installed.QualificationSHA != entry.QualificationSHA || installed.IdentityID != entry.IdentityID || installed.Native == nil || installed.Identity.PrivateKey == nil || len(installed.Identity.Certificate) == 0 {
			return nil, ErrInvalid
		}
		if installed.TimestampServices != nil {
			if installed.Timestamp != nil {
				return nil, ErrInvalid
			}
			required := [][]byte{}
			for _, wire := range entry.Policies {
				var policy SigningPolicy
				if decode(wire.Body, &policy, 1<<20) != nil {
					return nil, ErrInvalid
				}
				if policy.TimestampMode == "required" {
					required = append(required, wire.Body)
				}
			}
			router, err := NewSigningTimestampRouter(required, installed.TimestampServices)
			if err != nil {
				return nil, err
			}
			installed.Timestamp = router.Check
			installed.TimestampServices = nil
		}
		handles[entry.TenantID] = installed
	}
	configs := make([]TenantSigningInstallation, 0, len(doc.Tenants))
	for _, entry := range doc.Tenants {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		installed := handles[entry.TenantID]
		service := base
		keys, services := map[string]bool{}, map[string]bool{}
		policies := SigningInstallation{CertificateChainSHA: bodyDigest([]byte(entry.CertificateChain)), TrustBundleSHA: bodyDigest([]byte(entry.TrustBundle))}
		for _, wire := range entry.Policies {
			var policy SigningPolicy
			if decode(wire.Body, &policy, 1<<20) != nil {
				return nil, ErrInvalid
			}
			from, _ := time.Parse(time.RFC3339Nano, wire.NotBefore)
			until, _ := time.Parse(time.RFC3339Nano, wire.NotAfter)
			policies.SDKLockSHA = policy.SDKLockSHA
			keys[policy.KeyHandle] = true
			if policy.TimestampServiceID != nil {
				if installed.Timestamp == nil {
					return nil, ErrInvalid
				}
				services[*policy.TimestampServiceID] = true
			}
			policies.Policies = append(policies.Policies, InstalledSigningPolicy{Ref: v2input.Ref{ArtifactID: policy.ID, SHA256: bodyDigest(wire.Body)}, Body: append([]byte(nil), wire.Body...), NotBefore: from, NotAfter: until})
		}
		for key := range keys {
			policies.KeyHandles = append(policies.KeyHandles, key)
		}
		sort.Strings(policies.KeyHandles)
		for id := range services {
			policies.TimestampServices = append(policies.TimestampServices, id)
		}
		sort.Strings(policies.TimestampServices)
		service.Policies = policies
		service.ResourceReport = append([]byte(nil), entry.ResourceReport...)
		var report SigningResourceMeasurements
		if decode(entry.ResourceReport, &report, 8<<20) != nil {
			return nil, ErrInvalid
		}
		service.Resources = SigningResourceInstallation{ReportSHA: entry.ResourceReportSHA, RuntimeSHA: report.Runtime, SDKLockSHA: report.SDK, ContractsSHA: report.Contracts, HardwareSHA: report.Hardware}
		if string(entry.NegativePolicies) != "null" {
			var negative SigningNegativePolicyDocument
			if decode(entry.NegativePolicies, &negative, 8<<20) != nil {
				return nil, ErrInvalid
			}
			service.NegativePolicies = make([]InstalledSigningNegativePolicy, len(negative.Policies))
			for i, policy := range negative.Policies {
				service.NegativePolicies[i] = InstalledSigningNegativePolicy{PolicyID: policy.PolicyID, PolicySHA: policy.PolicySHA, Locks: ownSigningNegativeLocks(policy.Locks)}
			}
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM([]byte(entry.ChallengeRoots)) {
			return nil, ErrInvalid
		}
		operator := SigningOperatorInstallation{CertificateChain: []byte(entry.CertificateChain), TrustBundle: []byte(entry.TrustBundle), ChallengeOrigin: entry.ChallengeOrigin, ChallengeServerSHA: entry.ChallengeServerSHA, ChallengeRoots: roots, ChallengeIdentity: installed.Identity, Timeout: time.Duration(entry.TimeoutSeconds) * time.Second, CRLMaxAge: time.Duration(entry.CRLMaxAgeSeconds) * time.Second, Native: installed.Native, Timestamp: installed.Timestamp, Clock: base.Clock, Registration: &SQLSigningRegistrationTrust{DB: base.API, TenantID: entry.TenantID, WorkerID: entry.WorkerID, QualificationSHA: entry.QualificationSHA}}
		for _, usage := range entry.CertificateUsages {
			operator.CertificateUsages = append(operator.CertificateUsages, x509.ExtKeyUsage(usage))
		}
		if len(entry.CRLEndpoints) > 0 {
			operator.CRLRoots = x509.NewCertPool()
			if !operator.CRLRoots.AppendCertsFromPEM([]byte(entry.CRLRoots)) {
				return nil, ErrInvalid
			}
			operator.CRLCheckpoint = SQLSigningCRLCheckpoint{DB: base.API, TrustBundleSHA: policies.TrustBundleSHA}
			for _, endpoint := range entry.CRLEndpoints {
				operator.CRLEndpoints = append(operator.CRLEndpoints, SigningCRLEndpoint{IssuerSHA: endpoint.IssuerSHA, URL: endpoint.URL, ServerSHA: endpoint.ServerSHA})
			}
		}
		configs = append(configs, TenantSigningInstallation{Service: service, Operator: operator})
	}
	return freezeTenantSigningInstallations(configs)
}
