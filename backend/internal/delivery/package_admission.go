package delivery

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
	"unicode/utf8"
)

type PackageAdmission struct {
	Tenant, Package, Workflow uuid.UUID
	Name                      string
	Policy                    v2input.Ref
}

// AdmitPackage runs inside the same authorized transaction as preparation proof
// persistence. It neither creates shells nor dispatches work. SQL repeats source
// authority, checks the physical policy artifact and makes exact replay safe.
func (p PreparePlan) AdmitPackage(ctx context.Context, tx ProofTransaction, who auth.Identity, request PackageAdmission) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || request.Tenant == uuid.Nil || request.Package == uuid.Nil || request.Workflow != p.workflow || p.workflow == uuid.Nil || p.task == uuid.Nil || request.Policy.ArtifactID == uuid.Nil || !utf8.ValidString(request.Name) || utf8.RuneCountInString(request.Name) < 1 || utf8.RuneCountInString(request.Name) > 200 {
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	var proof PrepareProof
	if json.Unmarshal(p.proofJCS, &proof) != nil || proof.AuthSessionID != who.SessionID {
		return v2input.ErrInvalid
	}
	var frozen FrozenInput
	if json.Unmarshal([]byte(proof.FrozenInputJCS), &frozen) != nil {
		return v2input.ErrInvalid
	}
	raw, err := canonical.Marshal(frozen.Policy)
	if err != nil || digest(raw) != request.Policy.SHA256 {
		return v2input.ErrInvalid
	}
	var accepted bool
	err = tx.QueryRow(ctx, `SELECT photo.admit_v30_delivery_package($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		request.Tenant, who.UserID, who.SessionID, request.Package, request.Workflow, p.task, request.Name, request.Policy.ArtifactID, request.Policy.SHA256, string(raw), p.params.SHA256, string(p.paramsJCS)).Scan(&accepted)
	if err != nil {
		return err
	}
	if !accepted {
		return domain.NewError("DELIVERY_SOURCE_STALE", 409)
	}
	return nil
}
