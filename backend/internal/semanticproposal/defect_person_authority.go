package semanticproposal

import (
	"context"
	"encoding/json"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type defectPersonAuthority struct {
	SchemaVersion      int             `json:"schema_version"`
	Root               v2input.Ref     `json:"root"`
	Child              v2input.Ref     `json:"child"`
	Protection         v2input.Ref     `json:"protection"`
	ProtectedPixels    v2input.Ref     `json:"protected_pixels"`
	Count              int64           `json:"count"`
	Expected           json.RawMessage `json:"expected"`
	Evidence           json.RawMessage `json:"evidence"`
	Qualification      json.RawMessage `json:"qualification"`
	ProtectionEvidence json.RawMessage `json:"protection_evidence"`
}

// Exact one-real-person coverage and protected mask evidence; no fabricated
// duplicate proof. Native Params must contain the independently covered pixels.
// SQL producer/qualification/coverage/barrier authority is still required.
func (s SQLTransferSourceReader) marshalDefectPersonAuthority(ctx context.Context, who auth.Identity, prepared defectPersonAdmissionPreparation) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.validateDefectPersonAdmission(ctx, who, prepared.plan, prepared.person); err != nil {
		return nil, err
	}
	proof := prepared.person
	p := proof.protectionEvidence
	if !validProtectionPreparationScope(s, who, prepared.plan.request.TenantID, proof.Binding, p) {
		return nil, v2input.ErrInvalid
	}
	pins := make([]manualEvidencePin, 0, 2)
	for i, pin := range [2]defectEvidenceObject{p.pixels, p.root} {
		pins = append(pins, manualEvidencePin{[2]string{"protected_pixels", "protected_ref"}[i], pin.ref.ArtifactID, pin.ref.SHA256, p.bucket, pin.object.Key, pin.object.VersionID, pin.object.Bytes, pin.object.ContentType})
	}
	evidence, err := canonical.Marshal(pins)
	if err != nil {
		return nil, err
	}
	body, err := canonical.Marshal(defectPersonAuthority{SchemaVersion: 1, Root: proof.Root, Child: proof.Child, Protection: proof.Protection, ProtectedPixels: p.pixels.ref, Count: proof.Count, Expected: json.RawMessage(proof.Expected), Evidence: json.RawMessage(proof.Evidence), Qualification: json.RawMessage(proof.Qualification), ProtectionEvidence: evidence})
	if err != nil {
		return nil, err
	}
	if len(body) > 8<<20 {
		return nil, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return body, nil
}
