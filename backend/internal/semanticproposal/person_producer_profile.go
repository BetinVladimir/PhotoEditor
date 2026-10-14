package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// verifyPersonProducerProfile matches stored admission/attempt locks. Runtime
// measurements enforced by the worker installer remain a separate obligation.
func (s SQLTransferSourceReader) verifyPersonProducerProfile(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, mask VerifiedPersonMask, expected v2qualification.Expected) error {
 if err := ctx.Err(); err != nil { return err }
 if s.DB == nil || tenantID == uuid.Nil || s.ProjectID == uuid.Nil ||
  who.UserID == uuid.Nil || who.SessionID == uuid.Nil ||
  binding.AssetVersionID == uuid.Nil || !validPersonProfileRef(binding.Source) ||
  !validPersonProfileRef(mask.Root) || !validPersonProfileRef(mask.Pixels) ||
  expected.Operation != "mask.person" || expected.Version != "1" {
  return v2qualification.ErrUnqualified
 }
	body, err := canonical.Marshal(expected)
	if err != nil {
		return err
	}
	return tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var matches bool
		err := tx.QueryRow(ctx, "SELECT photo.v30_person_profile_consent_matches($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)", tenantID, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, mask.Root.ArtifactID, mask.Root.SHA256, mask.Pixels.ArtifactID, mask.Pixels.SHA256, body).Scan(&matches)
		if err != nil {
			return err
		}
		if !matches {
			return v2qualification.ErrUnqualified
		}
		return nil
	})
}

func validPersonProfileRef(ref v2input.Ref) bool {
 if ref.ArtifactID == uuid.Nil || len(ref.SHA256) != 64 { return false }
 for _,c := range ref.SHA256 { if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) { return false } }
 return true
}
