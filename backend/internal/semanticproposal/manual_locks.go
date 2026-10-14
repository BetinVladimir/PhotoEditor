package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"sort"
)

// lockManualPreparationProof locks only privately verified evidence. Any error
// requires rollback by the caller. Source/auth/version locks and atomic admission
// remain independent obligations; this method never commits or queues work.
func (s SQLTransferSourceReader) lockManualPreparationProof(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, proof manualPreparationProof) error {
	return s.lockManualPreparationProofs(ctx, tx, who, tenant, binding, []manualPreparationProof{proof})
}

// Batch evidence locks precede the single writer barrier. All private pins are
// owned before the first SQL callback; conflicting duplicate roots fail closed.
func (s SQLTransferSourceReader) lockManualPreparationProofs(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, proofs []manualPreparationProof) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || len(proofs) < 1 || len(proofs) > 64 {
		return v2input.ErrInvalid
	}
	type evidence struct {
		root   uuid.UUID
		target v2input.Ref
		pins   string
	}
	entries := make([]evidence, 0, len(proofs))
	seen := map[uuid.UUID]evidence{}
	for _, proof := range proofs {
		if !validManualPreparationScope(s.ProjectID, who, tenant, binding, proof) {
			return v2input.ErrInvalid
		}
		pins, err := marshalManualEvidencePins(proof, s.Bucket)
		if err != nil {
			return err
		}
		entry := evidence{proof.objects.Root.ref.ArtifactID, proof.objects.Pixel.ref, string(pins)}
		if previous, ok := seen[entry.root]; ok {
			if previous != entry {
				return v2input.ErrConflict
			}
			continue
		}
		seen[entry.root] = entry
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].root.String() < entries[j].root.String() })
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	lockAll := func() error {
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return err
			}
			var ready bool
			err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_manual_evidence($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)", tenant, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, entry.target.ArtifactID, entry.target.SHA256, entry.pins).Scan(&ready)
			if err != nil {
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
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_person_inventory($1,$2,$3,$4)", tenant, s.ProjectID, who.UserID, who.SessionID).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	return lockAll()
}
