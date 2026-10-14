package interchange

import (
	"bytes"
	"context"
	"encoding/json"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// SaveScheduler persists already qualified, immutable export evidence under
// the exact scheduler claim. It does not adopt or complete a run.
func (p PreparedExportAdoption) SaveScheduler(ctx context.Context,
	claims SQLExportAdoptionClaimStore, claim ExportAdoptionClaim,
	frozen FrozenExportDocuments, bucket string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if claims.DB == nil || claim.validate() != nil || len(bucket) < 3 || len(bucket) > 63 ||
		len(p.body) < 2 || len(p.body) > 16<<20 || p.sha != rawSHA(p.body) ||
		!validImportRef(p.root) || !validImportRef(p.bundle) {
		return v2input.ErrInvalid
	}
	intent, err := exportIntentFromClaim(claim)
	if err != nil {
		return err
	}
	if _, _, err := DecodeFrozenExportDocuments(ctx, intent, frozen.Body()); err != nil {
		return err
	}
	normalized, err := canonical.Bytes(p.body)
	if err != nil || !bytes.Equal(normalized, p.body) {
		return v2input.ErrInvalid
	}
	var evidence exportAdoptionEvidence
	decoder := json.NewDecoder(bytes.NewReader(p.body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&evidence); err != nil {
		return err
	}
	encoded, err := canonical.Marshal(evidence)
	if err != nil || !bytes.Equal(encoded, p.body) || evidence.Schema != 1 ||
		evidence.Format != claim.Request.Format || evidence.Tenant != claim.Tenant ||
		evidence.Actor != claim.Actor || evidence.Identity != intent.Identity ||
		evidence.RequestSHA != claim.RequestSHA || evidence.FrozenSHA != rawSHA(frozen.Body()) ||
		evidence.Root != p.root || evidence.Bundle != p.bundle || evidence.Bucket != bucket {
		return v2input.ErrInvalid
	}
	var saved bool
	err = claims.DB.QueryRow(ctx, `SELECT photo.save_v30_export_adoption_evidence(
 $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		claim.Tenant, claim.Workflow, claim.Run, claim.Task, claim.Result,
		claim.Actor, claim.Key, claim.RequestSHA, claim.Token,
		string(p.body), p.sha).Scan(&saved)
	if err != nil {
		return err
	}
	if !saved {
		return domain.NewError("INTERCHANGE_RESULT_STALE", 409)
	}
	return ctx.Err()
}
