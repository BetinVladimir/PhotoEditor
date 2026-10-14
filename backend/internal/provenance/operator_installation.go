package provenance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"sync/atomic"
	"time"
)

// All fields originate in trusted process provisioning. Certificate profile,
// live native/root trust and TSA checks cannot be selected by HTTP/job payloads.
type SigningOperatorInstallation struct {
	CertificateChain, TrustBundle       []byte
	CertificateUsages                   []x509.ExtKeyUsage
	ChallengeOrigin, ChallengeServerSHA string
	ChallengeRoots                      *x509.CertPool
	ChallengeIdentity                   tls.Certificate
	CRLEndpoints                        []SigningCRLEndpoint
	CRLRoots                            *x509.CertPool
	CRLCheckpoint                       SigningCRLCheckpoint
	CRLMaxAge, Timeout                  time.Duration
	Registration                        *SQLSigningRegistrationTrust
	Native, Timestamp                   SigningTrustCheck
	Clock                               func() time.Time
}

type InstalledChallengeSigningAuthority struct {
	authority *ChallengeSigningOperatorAuthority
	signer    *HTTPSigningChallengeSigner
	crls      *HTTPSigningCRLSource
	lifetime  context.Context
	cancel    context.CancelFunc
	closed    atomic.Bool
}

func NewInstalledChallengeSigningAuthority(config SigningOperatorInstallation) (*InstalledChallengeSigningAuthority, error) {
	return NewInstalledChallengeSigningAuthorityContext(context.Background(), config)
}
func NewInstalledChallengeSigningAuthorityContext(ctx context.Context, config SigningOperatorInstallation) (*InstalledChallengeSigningAuthority, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if config.Native == nil || config.Clock == nil {
		return nil, ErrInvalid
	}
	signer, err := NewHTTPSigningChallengeSigner(config.ChallengeOrigin, config.ChallengeRoots, config.ChallengeIdentity, config.ChallengeServerSHA, config.Timeout)
	if err != nil {
		return nil, err
	}
	installed := &InstalledChallengeSigningAuthority{signer: signer}
	installed.lifetime, installed.cancel = context.WithCancel(context.Background())
	success := false
	defer func() {
		if !success {
			installed.Close()
		}
	}()
	var revocation SigningTrustCheck
	if len(config.CRLEndpoints) > 0 {
		installed.crls, err = NewHTTPSigningCRLSource(config.CRLEndpoints, config.CRLRoots, config.Timeout)
		if err != nil {
			return nil, err
		}
		startup, ok := config.CRLCheckpoint.(SigningCRLInstallationCheckpoint)
		if !ok {
			return nil, ErrInvalid
		}
		trustSHA := sha256.Sum256(config.TrustBundle)
		issuers := make([]string, len(config.CRLEndpoints))
		for i, endpoint := range config.CRLEndpoints {
			issuers[i] = endpoint.IssuerSHA
		}
		startupContext, cancel := context.WithTimeout(ctx, config.Timeout)
		err = startup.CheckSigningCRLInstallation(startupContext, hex.EncodeToString(trustSHA[:]), issuers)
		cancel()
		if err != nil {
			return nil, err
		}
		revocation, err = NewDurableSigningCRLCheck(config.TrustBundle, installed.crls.ReadCRL, config.Clock, config.CRLMaxAge, config.CRLCheckpoint)
		if err != nil {
			return nil, err
		}
	}
	trust, err := NewCertificateSigningTrustAuthority(config.TrustBundle, config.CertificateUsages, config.Native, revocation, config.Timestamp, config.Clock)
	if err != nil {
		return nil, err
	}
	installed.authority, err = NewChallengeSigningOperatorAuthority(config.CertificateChain, signer, trust, config.Clock)
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	success = true
	return installed, nil
}

func (a *InstalledChallengeSigningAuthority) Close() {
	if a == nil || a.closed.Swap(true) {
		return
	}
	if a.cancel != nil {
		a.cancel()
	}
	if a.signer != nil {
		a.signer.Close()
	}
	if a.crls != nil {
		a.crls.Close()
	}
}
func (a *InstalledChallengeSigningAuthority) CheckSigningOperator(ctx context.Context, policy SigningPolicy) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if a == nil || a.closed.Load() || a.authority == nil || a.lifetime == nil {
		return ErrInvalid
	}
	scoped, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(a.lifetime, cancel)
	defer stop()
	if a.closed.Load() {
		return ErrInvalid
	}
	if err := a.authority.CheckSigningOperator(scoped, policy); err != nil {
		return err
	}
	if a.closed.Load() {
		return ErrInvalid
	}
	return scoped.Err()
}

var _ SigningOperatorAuthority = (*InstalledChallengeSigningAuthority)(nil)

// Owned service closes operator transports when service construction fails and
// exposes the same explicit lifecycle to the API process on successful install.
type InstalledSQLSigningService struct {
	*SQLSignExportService
	Operator *InstalledChallengeSigningAuthority
	TenantID uuid.UUID
}

func NewInstalledSQLSigningService(service PresentationSigningServiceInstallation, operator SigningOperatorInstallation) (*InstalledSQLSigningService, error) {
	return NewInstalledSQLSigningServiceContext(context.Background(), service, operator)
}
func NewInstalledSQLSigningServiceContext(ctx context.Context, service PresentationSigningServiceInstallation, operator SigningOperatorInstallation) (*InstalledSQLSigningService, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if service.Authority != nil || operator.Registration == nil || operator.Native == nil || operator.Timeout < time.Second || operator.Timeout > 30*time.Second {
		return nil, ErrInvalid
	}
	registration := *operator.Registration
	if registration.DB != service.API || registration.LiveTrust != nil {
		return nil, ErrInvalid
	}
	registration.LiveTrust = operator.Native
	startupContext, cancel := context.WithTimeout(ctx, operator.Timeout)
	err := registration.CheckInstallation(startupContext)
	cancel()
	if err != nil {
		return nil, err
	}
	operator.Native = registration.Check
	authority, err := NewInstalledChallengeSigningAuthorityContext(ctx, operator)
	if err != nil {
		return nil, err
	}
	if err = validateSigningOperatorPolicies(service.Policies, operator, authority); err != nil {
		authority.Close()
		return nil, err
	}
	if err = preflightActiveSigningPolicies(ctx, service.Policies, operator.Clock(), operator.Timeout, authority); err != nil {
		authority.Close()
		return nil, err
	}
	service.Authority = authority
	built, err := NewSQLRenderPresentationSigningService(service)
	if err != nil {
		authority.Close()
		return nil, err
	}
	if err = bindInstalledSigningServiceTenant(built, registration.TenantID); err != nil {
		authority.Close()
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		authority.Close()
		return nil, err
	}
	return &InstalledSQLSigningService{SQLSignExportService: built, Operator: authority, TenantID: registration.TenantID}, nil
}
func (s *InstalledSQLSigningService) Close() {
	if s != nil && s.Operator != nil {
		s.Operator.Close()
	}
}

// Structural coverage checks perform no network or native invocation. Live
// authority remains mandatory on every use, including after external key IO.
func validateSigningOperatorPolicies(policies SigningInstallation, operator SigningOperatorInstallation, authority SigningOperatorAuthority) error {
	if _, err := NewSigningPolicyRegistry(policies, authority); err != nil {
		return err
	}
	chainSum, trustSum := sha256.Sum256(operator.CertificateChain), sha256.Sum256(operator.TrustBundle)
	if policies.CertificateChainSHA != hex.EncodeToString(chainSum[:]) || policies.TrustBundleSHA != hex.EncodeToString(trustSum[:]) {
		return ErrInvalid
	}
	certificates, err := signingTrustCertificates(operator.CertificateChain)
	if err != nil {
		return err
	}
	roots, err := signingTrustCertificates(operator.TrustBundle)
	if err != nil {
		return err
	}
	endpoints := map[string]bool{}
	for _, endpoint := range operator.CRLEndpoints {
		if endpoints[endpoint.IssuerSHA] {
			return ErrInvalid
		}
		endpoints[endpoint.IssuerSHA] = true
	}
	requiresRevocation := false
	for _, installed := range policies.Policies {
		policy, err := DecodeSigningPolicy(installed.Body, installed.Ref, installed.Ref.ArtifactID)
		if err != nil {
			return err
		}
		if policy.TimestampMode == "required" && operator.Timestamp == nil {
			return ErrInvalid
		}
		if policy.RevocationMode == "required" {
			requiresRevocation = true
		}
	}
	if !requiresRevocation {
		return nil
	}
	if len(endpoints) == 0 {
		return ErrInvalid
	}
	candidates := append(append([]*x509.Certificate(nil), certificates...), roots...)
	for _, cert := range certificates {
		anchor := false
		for _, root := range roots {
			if bytes.Equal(root.Raw, cert.Raw) && bytes.Equal(cert.RawIssuer, cert.RawSubject) && cert.CheckSignatureFrom(cert) == nil {
				anchor = true
				break
			}
		}
		if anchor {
			continue
		}
		covered := false
		for _, issuer := range candidates {
			if bytes.Equal(cert.RawIssuer, issuer.RawSubject) && cert.CheckSignatureFrom(issuer) == nil && issuer.KeyUsage&x509.KeyUsageCRLSign != 0 {
				sum := sha256.Sum256(issuer.Raw)
				if endpoints[hex.EncodeToString(sum[:])] {
					covered = true
					break
				}
			}
		}
		if !covered {
			return ErrInvalid
		}
	}
	return nil
}

// Use this wrapper at the API boundary; never expose a fixed tenant's operator
// through the embedded unscoped service to a different tenant.
func (s *InstalledSQLSigningService) SignExport(ctx context.Context, who auth.Identity, tenant, export, policy uuid.UUID, key string) (v2input.JobTicket, error) {
	if err := ctx.Err(); err != nil {
		return v2input.JobTicket{}, err
	}
	if s == nil || s.SQLSignExportService == nil || s.TenantID == uuid.Nil || tenant != s.TenantID {
		return v2input.JobTicket{}, ErrInvalid
	}
	return s.SQLSignExportService.SignExport(ctx, who, tenant, export, policy, key)
}
func (s *InstalledSQLSigningService) AvailableSigningPolicies(ctx context.Context, who auth.Identity, tenant, export uuid.UUID) ([]SigningPolicyChoice, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil || s.SQLSignExportService == nil || s.TenantID == uuid.Nil || tenant != s.TenantID {
		return nil, ErrInvalid
	}
	return s.SQLSignExportService.AvailableSigningPolicies(ctx, who, tenant, export)
}

func bindInstalledSigningServiceTenant(service *SQLSignExportService, tenant uuid.UUID) error {
	if service == nil || tenant == uuid.Nil || service.Preparation.Registry == nil {
		return ErrInvalid
	}
	measured, ok := service.Preparation.Resources.(*MeasuredSigningResourceQualifier)
	if !ok || measured == nil || measured.registry != service.Preparation.Registry {
		return ErrInvalid
	}
	registry, err := service.Preparation.Registry.ForTenant(tenant)
	if err != nil {
		return err
	}
	owned := *measured
	owned.registry = registry
	service.Preparation.Registry = registry
	service.Preparation.Resources = &owned
	return nil
}
