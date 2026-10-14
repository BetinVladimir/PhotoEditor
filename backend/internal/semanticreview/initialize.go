package semanticreview

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// InitializePreparedReview writes within the caller transaction; failed proof or
// stale scope must roll back that entire transaction. No human decisions occur.
func InitializePreparedReview(ctx context.Context, tx pgx.Tx, tenant, workflow, actor, session uuid.UUID, prepared PreparedReview) (uuid.UUID, error) {
	proof, err := EncodeInitializationDocuments(prepared)
	if err != nil {
		return uuid.Nil, err
	}
	if _, err = LoadLockedPreparedSources(ctx, tx, tenant, workflow, actor, session, prepared); err != nil {
		return uuid.Nil, err
	}
	var id uuid.UUID
	err = tx.QueryRow(ctx, "SELECT photo.initialize_semantic_review($1,$2,$3,$4,$5,$6::jsonb)", tenant, workflow, actor, session, prepared.Sources.Revision, json.RawMessage(proof)).Scan(&id)
	if err != nil {
		return uuid.Nil, err
	}
	if id == uuid.Nil {
		return uuid.Nil, ErrConflict
	}
	return id, nil
}
