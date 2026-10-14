package delivery

import (
 "context"

 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/provenance"
 "photoeditor/backend/internal/v2input"
)

// ReadDelegatedFinalTask reads only the final export task. It does not prove
// ancestor tasks or operation-specific claims for a complete signing graph.
func ReadDelegatedFinalTask(ctx context.Context, db *pgxpool.Pool,
 lease ExpansionLease, slot Slot, bucket string) (provenance.ReadySigningExport,
 provenance.CanonicalSigningTask, error) {
 var noReady provenance.ReadySigningExport
 var noTask provenance.CanonicalSigningTask
 if err := ctx.Err(); err != nil { return noReady, noTask, err }
 if db == nil { return noReady, noTask, v2input.ErrInvalid }
 scope, ready, err := ReadDelegatedReadyExport(ctx, db, lease, slot, bucket)
 if err != nil { return noReady, noTask, err }
 var raw []byte
 err = db.QueryRow(ctx, `SELECT photo.v31_delivery_final_task_evidence($1,$2,$3,$4)`,
  lease.Expansion, lease.Token, slot.Ordinal, slot.PresetID).Scan(&raw)
 if err != nil { return noReady, noTask, err }
 task, err := provenance.DecodeDelegatedFinalTaskEvidence(raw, ready)
 if err != nil { return noReady, noTask, err }
 currentScope, currentReady, err := ReadDelegatedReadyExport(ctx, db, lease, slot, bucket)
 if err != nil { return noReady, noTask, err }
 if currentScope != scope || currentReady != ready { return noReady, noTask, v2input.ErrInvalid }
 if err := ctx.Err(); err != nil { return noReady, noTask, err }
 return ready, task, nil
}
