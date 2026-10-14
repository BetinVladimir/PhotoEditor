package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"sort"
)

// Selected source/face/parser composition only. All producer locks precede the
// shared tenant barrier. Global required policies, other evidence families and
// final authorization/admission remain caller obligations; errors require rollback.
func (s SQLTransferSourceReader) lockFaceAndParseSourceInventory(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, color TransferColorProfile, source sourcePreparationProof, faces []facePreparationProof, parsers []faceParsePreparationProof) error {
	return s.lockSelectedSourceInventories(ctx, tx, who, tenant, binding, color, source, faces, parsers, nil, nil, "")
}

// Includes selected original manual closures; manual inventory completeness is
// independent. Pointer-backed vector pins are serialized before SQL callbacks.
func (s SQLTransferSourceReader) lockFaceParseManualSourceInventory(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, color TransferColorProfile, source sourcePreparationProof, faces []facePreparationProof, parsers []faceParsePreparationProof, manual []manualPreparationProof) error {
	if len(manual) < 1 || len(manual) > 64 {
		return v2input.ErrInvalid
	}
	return s.lockSelectedSourceInventories(ctx, tx, who, tenant, binding, color, source, faces, parsers, manual, nil, "")
}

// Includes privately verified person coverage and original protection evidence.
// Still selected evidence only, with global policy/admission obligations separate.
func (s SQLTransferSourceReader) lockSelectedPersonSourceInventory(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, color TransferColorProfile, source sourcePreparationProof, faces []facePreparationProof, parsers []faceParsePreparationProof, manual []manualPreparationProof, person []personProtectionProof) error {
	if len(person) < 1 || len(person) > 64 {
		return v2input.ErrInvalid
	}
	return s.lockSelectedSourceInventories(ctx, tx, who, tenant, binding, color, source, faces, parsers, manual, person, "")
}

// Freezes the explicit operation policy catalog together with selected evidence.
// Required-model selection and final queued admission remain independent.
func (s SQLTransferSourceReader) lockPolicySelectedSourceInventory(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, color TransferColorProfile, source sourcePreparationProof, faces []facePreparationProof, parsers []faceParsePreparationProof, manual []manualPreparationProof, person []personProtectionProof, policy qualificationPolicySnapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validQualificationPolicySnapshot(s, who, tenant, policy) || len(person) < 1 || len(person) > 64 {
		return v2input.ErrInvalid
	}
	if err := matchSelectedEvidencePolicies(policy, faces, parsers, person); err != nil {
		return err
	}
	return s.lockSelectedSourceInventories(ctx, tx, who, tenant, binding, color, source, faces, parsers, manual, person, policy.body)
}

// Requires both execution policies before locking selected original evidence.
// Does not attest execution report/calibration bytes, measured costs or queue work.
func (s SQLTransferSourceReader) lockProposalPolicySourceInventory(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, color TransferColorProfile, source sourcePreparationProof, faces []facePreparationProof, parsers []faceParsePreparationProof, manual []manualPreparationProof, person []personProtectionProof, policy qualificationPolicySnapshot, defect, transfer v2qualification.Expected) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validQualificationPolicySnapshot(s, who, tenant, policy) {
		return v2input.ErrInvalid
	}
	if err := matchProposalExecutionPolicies(policy, defect, transfer); err != nil {
		return err
	}
	return s.lockPolicySelectedSourceInventory(ctx, tx, who, tenant, binding, color, source, faces, parsers, manual, person, policy)
}

func (s SQLTransferSourceReader) lockSelectedSourceInventories(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, color TransferColorProfile, source sourcePreparationProof, faces []facePreparationProof, parsers []faceParsePreparationProof, manual []manualPreparationProof, person []personProtectionProof, policyBody string) error {
	return s.lockSelectedSourceInventoriesWithTransfer(ctx, tx, who, tenant, binding, color, source, faces, parsers, manual, person, policyBody, nil)
}

func (s SQLTransferSourceReader) lockSelectedSourceInventoriesWithTransfer(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, color TransferColorProfile, source sourcePreparationProof, faces []facePreparationProof, parsers []faceParsePreparationProof, manual []manualPreparationProof, person []personProtectionProof, policyBody string, transferLock func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || tenant == uuid.Nil || s.ProjectID == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || s.Bucket == "" || source.tenant != tenant || source.project != s.ProjectID || source.actor != who.UserID || source.session != who.SessionID || source.bucket != s.Bucket || source.binding != binding || source.iccSHA != color.sha || !source.unitRGB {
		return v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(color.sha); err != nil {
		return v2input.ErrInvalid
	}
	if _, err := marshalSourceEvidencePins(source); err != nil {
		return err
	}
	ownedFaces := append([]facePreparationProof(nil), faces...)
	ownedParsers := append([]faceParsePreparationProof(nil), parsers...)
	faceBody, faceExpected, err := s.marshalFacePreparationInventory(who, tenant, binding, ownedFaces)
	if err != nil {
		return err
	}
	parseBody, parseExpected, err := s.marshalFaceParsePreparationInventory(who, tenant, binding, ownedParsers)
	if err != nil {
		return err
	}
	// Validate evidence serialization for all later locks before the first SQL call.
	for _, proof := range ownedFaces {
		if _, err := marshalFaceEvidencePins(proof); err != nil {
			return err
		}
	}
	for _, proof := range ownedParsers {
		if _, err := marshalFaceParseEvidencePins(proof); err != nil {
			return err
		}
	}
	sort.Slice(ownedFaces, func(i, j int) bool {
		return ownedFaces[i].objects[0][0].ref.ArtifactID.String() < ownedFaces[j].objects[0][0].ref.ArtifactID.String()
	})
	sort.Slice(ownedParsers, func(i, j int) bool {
		return ownedParsers[i].evidence.objects[0][0].ref.ArtifactID.String() < ownedParsers[j].evidence.objects[0][0].ref.ArtifactID.String()
	})
	if len(manual) > 64 {
		return v2input.ErrInvalid
	}
	type manualPin struct {
		root   uuid.UUID
		target v2input.Ref
		pins   string
	}
	manualPins := make([]manualPin, 0, len(manual))
	seenManual := map[uuid.UUID]manualPin{}
	for _, proof := range manual {
		if !validManualPreparationScope(s.ProjectID, who, tenant, binding, proof) {
			return v2input.ErrInvalid
		}
		body, err := marshalManualEvidencePins(proof, s.Bucket)
		if err != nil {
			return err
		}
		pin := manualPin{proof.objects.Root.ref.ArtifactID, proof.objects.Pixel.ref, string(body)}
		if previous, ok := seenManual[pin.root]; ok {
			if previous != pin {
				return v2input.ErrConflict
			}
			continue
		}
		seenManual[pin.root] = pin
		manualPins = append(manualPins, pin)
	}
	sort.Slice(manualPins, func(i, j int) bool { return manualPins[i].root.String() < manualPins[j].root.String() })
	if len(person) > 64 {
		return v2input.ErrInvalid
	}
	persons := make([]personProtectionProof, 0, len(person))
	seenPersons := map[uuid.UUID]personProtectionProof{}
	for _, proof := range person {
		if !validPersonPreparationScope(s, who, tenant, binding, proof) {
			return v2input.ErrInvalid
		}
		if previous, ok := seenPersons[proof.Root.ArtifactID]; ok {
			if previous != proof {
				return v2input.ErrConflict
			}
			continue
		}
		seenPersons[proof.Root.ArtifactID] = proof
		persons = append(persons, proof)
	}
	sort.Slice(persons, func(i, j int) bool { return persons[i].Root.ArtifactID.String() < persons[j].Root.ArtifactID.String() })
	lockAll := func() error {
		if transferLock != nil {
			if err := transferLock(); err != nil {
				return err
			}
		}
		if err := s.lockSourcePreparationProof(ctx, tx, who, tenant, binding, source); err != nil {
			return err
		}
		for _, proof := range ownedFaces {
			if err := s.lockFaceEvidenceProof(ctx, tx, who, tenant, binding, proof); err != nil {
				return err
			}
			if err := s.lockFaceQualificationProof(ctx, tx, who, tenant, binding, proof); err != nil {
				return err
			}
		}
		for _, proof := range ownedParsers {
			if err := s.lockFaceParseEvidenceProof(ctx, tx, who, tenant, binding, proof); err != nil {
				return err
			}
			if err := s.lockFaceParseQualificationProof(ctx, tx, who, tenant, binding, proof); err != nil {
				return err
			}
		}
		for _, pin := range manualPins {
			if err := ctx.Err(); err != nil {
				return err
			}
			var ready bool
			if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_manual_evidence($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)", tenant, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, pin.target.ArtifactID, pin.target.SHA256, pin.pins).Scan(&ready); err != nil {
				return err
			}
			if !ready {
				return v2input.ErrConflict
			}
		}
		for _, proof := range persons {
			if err := s.lockProtectionPreparationProof(ctx, tx, who, tenant, binding, proof.protectionEvidence); err != nil {
				return err
			}
			var ready bool
			if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_person_evidence($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)", tenant, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, proof.Root.ArtifactID, proof.Root.SHA256, proof.Child.ArtifactID, proof.Child.SHA256, proof.Expected, proof.Evidence).Scan(&ready); err != nil {
				return err
			}
			if !ready {
				return v2input.ErrConflict
			}
			if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_person_qualification($1,$2,$3,$4,$5,$6)", tenant, s.ProjectID, who.UserID, who.SessionID, proof.Expected, proof.Qualification).Scan(&ready); err != nil {
				return err
			}
			if !ready {
				return v2input.ErrConflict
			}
		}
		return nil
	}
	if err := lockAll(); err != nil {
		return err
	}
	var ready bool
	lockPolicy := func() error {
		if policyBody == "" {
			return nil
		}
		if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_qualification_policy_snapshot($1,$2,$3,$4,$5)", tenant, s.ProjectID, who.UserID, who.SessionID, policyBody).Scan(&ready); err != nil {
			return err
		}
		if !ready {
			return v2input.ErrConflict
		}
		return nil
	}
	if err := lockPolicy(); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_person_inventory($1,$2,$3,$4)", tenant, s.ProjectID, who.UserID, who.SessionID).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	if err := lockAll(); err != nil {
		return err
	}
	for _, gate := range []struct{ sql, expected, body string }{
		{"SELECT photo.lock_semantic_proposal_face_inventory($1,$2,$3,$4,$5,$6,$7,$8,$9)", faceExpected, string(faceBody)},
		{"SELECT photo.lock_semantic_proposal_face_parse_inventory($1,$2,$3,$4,$5,$6,$7,$8,$9)", parseExpected, string(parseBody)},
	} {
		if err := tx.QueryRow(ctx, gate.sql, tenant, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, gate.expected, gate.body).Scan(&ready); err != nil {
			return err
		}
		if !ready {
			return v2input.ErrConflict
		}
	}
	for _, proof := range persons {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, "SELECT photo.semantic_proposal_person_candidate_matches($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)", tenant, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, proof.Root.ArtifactID, proof.Root.SHA256, proof.Child.ArtifactID, proof.Child.SHA256, proof.Expected).Scan(&ready); err != nil {
			return err
		}
		if !ready {
			return v2input.ErrConflict
		}
	}
	if err := lockPolicy(); err != nil {
		return err
	}
	return ctx.Err()
}

// Locks verified transfer documents before inventory barriers and repeats their
// live authority under those barriers. Defect report/cost admission remains separate.
func (s SQLTransferSourceReader) lockTransferQualifiedSourceInventory(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, color TransferColorProfile, source sourcePreparationProof, faces []facePreparationProof, parsers []faceParsePreparationProof, manual []manualPreparationProof, person []personProtectionProof, policy qualificationPolicySnapshot, transferProof transferQualificationPreparationProof, expected v2qualification.Expected) error {
	if _, _, err := s.marshalTransferQualificationPreparation(ctx, who, tenant, transferProof, expected); err != nil {
		return err
	}
	if !validQualificationPolicySnapshot(s, who, tenant, policy) {
		return v2input.ErrInvalid
	}
	selected, err := selectQualificationPolicies(policy, []string{"semantic.transfer"})
	if err != nil {
		return err
	}
	if !sameQualificationPolicyExpected(expected, selected["semantic.transfer"]) {
		return v2qualification.ErrUnqualified
	}
	if err := matchSelectedEvidencePolicies(policy, faces, parsers, person); err != nil {
		return err
	}
	expected = ownQualificationExpected(expected)
	return s.lockSelectedSourceInventoriesWithTransfer(ctx, tx, who, tenant, binding, color, source, faces, parsers, manual, person, policy.body, func() error {
		return s.lockTransferQualificationPreparation(ctx, tx, who, tenant, transferProof, expected)
	})
}

// Both execution qualifications accompany selected original evidence. This
// freezes authority only; measured costs and atomic queue admission remain separate.
func (s SQLTransferSourceReader) lockExecutionQualifiedSourceInventory(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, color TransferColorProfile, source sourcePreparationProof, faces []facePreparationProof, parsers []faceParsePreparationProof, manual []manualPreparationProof, person []personProtectionProof, policy qualificationPolicySnapshot, defectProof defectQualificationPreparationProof, transferProof transferQualificationPreparationProof, defect, transfer v2qualification.Expected) error {
	defect, transfer = ownQualificationExpected(defect), ownQualificationExpected(transfer)
	if _, err := s.marshalDefectQualificationPreparation(ctx, who, tenant, defectProof, defect); err != nil {
		return err
	}
	if _, _, err := s.marshalTransferQualificationPreparation(ctx, who, tenant, transferProof, transfer); err != nil {
		return err
	}
	if !validQualificationPolicySnapshot(s, who, tenant, policy) {
		return v2input.ErrInvalid
	}
	if err := matchProposalExecutionPolicies(policy, defect, transfer); err != nil {
		return err
	}
	if err := matchSelectedEvidencePolicies(policy, faces, parsers, person); err != nil {
		return err
	}
	return s.lockSelectedSourceInventoriesWithTransfer(ctx, tx, who, tenant, binding, color, source, faces, parsers, manual, person, policy.body, func() error {
		if err := s.lockDefectQualificationPreparation(ctx, tx, who, tenant, defectProof, defect); err != nil {
			return err
		}
		return s.lockTransferQualificationPreparation(ctx, tx, who, tenant, transferProof, transfer)
	})
}
