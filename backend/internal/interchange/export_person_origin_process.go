package interchange

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// ExportPersonOriginProcessInstallation holds a trusted signing handle and
// the pinned person policy. The private key is never loaded from the archive.
type ExportPersonOriginProcessInstallation struct {
	signer   *ProcessPersonOriginSigner
	issuerID uuid.UUID
	public   ed25519.PublicKey
	expected v2qualification.Expected
}

func (s ExportStyleDeploymentAuthority) InstallPersonOriginProcess(
	key crypto.Signer, issuerID uuid.UUID,
) (*ExportPersonOriginProcessInstallation, error) {
	if s.pinnedPolicies == nil || key == nil || issuerID == uuid.Nil ||
		issuerID != s.personOriginIssuer || len(s.personOriginPublic) != ed25519.PublicKeySize {
		return nil, v2input.ErrInvalid
	}
	public, ok := key.Public().(ed25519.PublicKey)
	if !ok || len(public) != ed25519.PublicKeySize || !bytes.Equal(public, s.personOriginPublic) {
		return nil, v2input.ErrInvalid
	}
	signer, err := NewProcessPersonOriginSigner(key, public)
	if err != nil {
		return nil, err
	}
	policy := s.pinnedPolicies.Person
	if !validImportRef(policy.Report) || policy.Expected.Operation != "mask.person" ||
		policy.Expected.Version != "1" || !policy.Expected.LicenseCleared ||
		!validImportRefForSHA(policy.Expected.ModelLockSHA) {
		return nil, v2input.ErrInvalid
	}
	return &ExportPersonOriginProcessInstallation{
		signer: signer, issuerID: issuerID,
		public:   append(ed25519.PublicKey(nil), public...),
		expected: cloneExportPersonExpected(policy.Expected),
	}, nil
}

func (s ExportStyleDeploymentAuthority) PersonOriginTrust() map[PortablePersonOriginKey]ed25519.PublicKey {
	if s.personOriginIssuer == uuid.Nil || len(s.personOriginPublic) != ed25519.PublicKeySize {
		return nil
	}
	digest := sha256.Sum256(s.personOriginPublic)
	return map[PortablePersonOriginKey]ed25519.PublicKey{
		{IssuerID: s.personOriginIssuer, KeyID: hex.EncodeToString(digest[:])}: append(ed25519.PublicKey(nil), s.personOriginPublic...),
	}
}

func cloneExportPersonExpected(source v2qualification.Expected) v2qualification.Expected {
	owned := source
	owned.RequiredChecks = append([]string(nil), source.RequiredChecks...)
	clone := func(values map[string]struct{}) map[string]struct{} {
		result := make(map[string]struct{}, len(values))
		for key := range values {
			result[key] = struct{}{}
		}
		return result
	}
	owned.CalibrationSHAs = clone(source.CalibrationSHAs)
	owned.TestFixtureSHAs = clone(source.TestFixtureSHAs)
	owned.EvidenceSHAs = clone(source.EvidenceSHAs)
	return owned
}

func (s *ExportPersonOriginProcessInstallation) Trust() map[PortablePersonOriginKey]ed25519.PublicKey {
	if s == nil {
		return nil
	}
	digest := sha256.Sum256(s.public)
	return map[PortablePersonOriginKey]ed25519.PublicKey{
		{IssuerID: s.issuerID, KeyID: hex.EncodeToString(digest[:])}: append(ed25519.PublicKey(nil), s.public...),
	}
}

func (s *ExportPersonOriginProcessInstallation) CandidateFactory(
	ctx context.Context, objects v2input.SessionAnalysisObjectReader,
) (ExportPersonOriginCandidateFactory, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil || s.signer == nil || objects == nil || s.issuerID == uuid.Nil ||
		len(s.public) != ed25519.PublicKeySize {
		return nil, v2input.ErrInvalid
	}
	return SQLExportPersonOriginCandidates{
		Objects: objects, Signer: s.signer, IssuerID: s.issuerID,
		PublicKey: append(ed25519.PublicKey(nil), s.public...),
		Expected:  cloneExportPersonExpected(s.expected),
	}, nil
}
