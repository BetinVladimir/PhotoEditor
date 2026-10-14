package semanticproposal

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

type personCandidate struct{ root, pixels v2input.Ref }

// An empty qualified candidate inventory is a missing dependency, not invalid
// client intent. Existing malformed/ambiguous evidence keeps its own refusal.
var errPersonProducerUnavailable = fmt.Errorf("qualified person producer unavailable: %w", v2qualification.ErrUnqualified)

func (s SQLTransferSourceReader) personCandidate(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, expected v2qualification.Expected) (personCandidate, error) {
	expected = ownQualificationExpected(expected)
	empty := personCandidate{}
	if s.DB == nil || expected.Operation != "mask.person" || expected.Version != "1" {
		return empty, v2input.ErrInvalid
	}
	body, err := canonical.Marshal(expected)
	if err != nil {
		return empty, err
	}
	result := empty
	err = tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		rows, err := tx.Query(ctx, "SELECT root_id,root_sha,child_id,child_sha FROM photo.semantic_proposal_person_candidates($1,$2,$3,$4,$5,$6,$7,$8)", tenantID, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, body)
		if err != nil {
			return err
		}
		defer rows.Close()
		count := 0
		for rows.Next() {
			if count > 0 {
				return fmt.Errorf("multiple qualified person producers for source: %w", v2input.ErrConflict)
			}
			if err := rows.Scan(&result.root.ArtifactID, &result.root.SHA256, &result.pixels.ArtifactID, &result.pixels.SHA256); err != nil {
				return err
			}
			count++
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if count != 1 {
			return errPersonProducerUnavailable
		}
		return nil
	})
	if errors.Is(err, errPersonProducerUnavailable) {
		// An empty candidate query can also mean an invalid source scope. Verify
		// the original version/document outside the transaction before calling
		// it a missing producer; never convert a foreign/missing source to503.
		if _, sourceErr := s.Read(ctx, who, tenantID, binding); sourceErr != nil {
			return empty, sourceErr
		}
	}
	if err != nil {
		return empty, err
	}
	return result, nil
}

// SelectQualifiedPersonEvidence discovers one exact-source canonical candidate,
// validates complete evidence and independent qualification, then repeats the
// discovery. It never picks newest or manufactures an empty/manual fallback.
// Final transactional inventory stability remains an admission obligation.
func (s SQLTransferSourceReader) SelectQualifiedPersonEvidence(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, qualificationReport v2input.Ref, expected v2qualification.Expected) (VerifiedPersonMask, error) {
	expected = ownQualificationExpected(expected)
	empty := VerifiedPersonMask{}
	selected, err := s.personCandidate(ctx, who, tenantID, binding, expected)
	if err != nil {
		return empty, err
	}
	verified, err := s.ReadQualifiedPersonEvidence(ctx, who, tenantID, binding, selected.root, qualificationReport, expected)
	if err != nil {
		return empty, err
	}
	if verified.Pixels != selected.pixels {
		return empty, v2input.ErrConflict
	}
	current, err := s.personCandidate(ctx, who, tenantID, binding, expected)
	if err != nil {
		return empty, err
	}
	if current != selected {
		return empty, v2input.ErrConflict
	}
	return verified, nil
}

// SelectQualifiedPersonProtectionCoverage discovers the unique canonical person
// mask, proves qualified coverage, and repeats discovery after all streaming IO.
// The returned root is server-selected; final admission must still lock inventory.
func (s SQLTransferSourceReader) SelectQualifiedPersonProtectionCoverage(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, protectionRoot, qualificationReport v2input.Ref, expected v2qualification.Expected) (v2input.Ref, int64, error) {
	proof, err := s.selectPersonProtectionProof(ctx, who, tenantID, binding, protectionRoot, qualificationReport, expected)
	if err != nil {
		return v2input.Ref{}, 0, err
	}
	return proof.Root, proof.Count, nil
}

// personProtectionProof is created only after qualified evidence and coverage IO.
// It records preflight evidence; it does not replace final transaction locks.
type personProtectionProof struct {
	Binding                         v2input.SessionVersionBinding
	Root, Child, Protection, Report v2input.Ref
	Qualification                   string
	Evidence                        string
	Expected                        string
	Count                           int64
	protectionEvidence              protectionPreparationProof
}

func (s SQLTransferSourceReader) selectPersonProtectionProof(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, protectionRoot, qualificationReport v2input.Ref, expected v2qualification.Expected) (personProtectionProof, error) {
	expected = ownQualificationExpected(expected)
	empty := personProtectionProof{}
	selected, err := s.personCandidate(ctx, who, tenantID, binding, expected)
	if err != nil {
		return empty, err
	}
	_, protectionEvidence, err := s.readProtectionPreparationProof(ctx, who, tenantID, binding, protectionRoot)
	if err != nil {
		return empty, err
	}
	count, evidencePins, qualificationPin, err := s.verifyQualifiedPersonProtectionCoveragePinned(ctx, who, tenantID, binding, selected.root, protectionRoot, qualificationReport, expected)
	if err != nil {
		return empty, err
	}
	if err := s.recheckProtectionPreparationProof(ctx, who, tenantID, binding, protectionEvidence); err != nil {
		return empty, err
	}
	current, err := s.personCandidate(ctx, who, tenantID, binding, expected)
	if err != nil {
		return empty, err
	}
	if current != selected {
		return empty, v2input.ErrConflict
	}
	body, err := canonical.Marshal(expected)
	if err != nil {
		return empty, err
	}
	evidenceBody, err := marshalPersonEvidencePins(evidencePins, s.Bucket)
	if err != nil {
		return empty, err
	}
	qualificationBody, err := canonical.Marshal(personEvidencePin{qualificationPin.contract, qualificationPin.ref.ArtifactID, qualificationPin.ref.SHA256, s.Bucket, qualificationPin.object.Key, qualificationPin.object.VersionID, qualificationPin.object.Bytes, qualificationPin.backend})
	if err != nil {
		return empty, err
	}
	return personProtectionProof{Binding: binding, Root: selected.root, Child: selected.pixels, Protection: protectionRoot, Report: qualificationReport, Expected: string(body), Evidence: string(evidenceBody), Qualification: string(qualificationBody), Count: count, protectionEvidence: protectionEvidence}, nil
}
