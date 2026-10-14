package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"sort"
)

// Selected protected evidence only. All proofs are owned/checked before SQL;
// producer locks precede the tenant input writer barrier and repeat after it.
// Required policy/coverage/source/auth authority remains caller obligations.
func (s SQLTransferSourceReader) lockProtectionPreparationProofs(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, proofs []protectionPreparationProof) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || len(proofs) < 1 || len(proofs) > 64 {
		return v2input.ErrInvalid
	}
	entries := make([]protectionPreparationProof, 0, len(proofs))
	seen := map[uuid.UUID]protectionPreparationProof{}
	for _, proof := range proofs {
		if !validProtectionPreparationScope(s, who, tenant, binding, proof) {
			return v2input.ErrInvalid
		}
		root := proof.root.ref.ArtifactID
		if old, ok := seen[root]; ok {
			if old != proof {
				return v2input.ErrConflict
			}
			continue
		}
		seen[root] = proof
		entries = append(entries, proof)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].root.ref.ArtifactID.String() < entries[j].root.ref.ArtifactID.String()
	})
	lockAll := func() error {
		for _, proof := range entries {
			if err := s.lockProtectionPreparationProof(ctx, tx, who, tenant, binding, proof); err != nil {
				return err
			}
		}
		return nil
	}
	if err := lockAll(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
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
