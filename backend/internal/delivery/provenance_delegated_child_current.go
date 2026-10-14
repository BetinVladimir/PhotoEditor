package delivery

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/provenance"
	"photoeditor/backend/internal/v2input"
)

// CompileCurrentDelegatedSignerChild brackets policy/operator resolution and
// task compilation with current parent-lease and ready-export reads. The
// lineage ref/body must be the receipt of delegated immutable publication.
// Atomic SQL admission must repeat these checks under its own locks.
func CompileCurrentDelegatedSignerChild(ctx context.Context, db *pgxpool.Pool,
	lease ExpansionLease, slot Slot, bucket string,
	lineageRef v2input.Ref, lineageBody []byte,
	registry *provenance.SigningPolicyRegistry,
	resources provenance.SigningTaskResources, now time.Time) (SignerDelegationScope,
	provenance.ReadySigningExport, provenance.FrozenSigningIntent,
	provenance.FrozenSigningTask, error) {
	var scope SignerDelegationScope
	var ready provenance.ReadySigningExport
	var intent provenance.FrozenSigningIntent
	var task provenance.FrozenSigningTask
	if err := ctx.Err(); err != nil { return scope, ready, intent, task, err }
	if db == nil || len(bucket) < 3 || len(bucket) > 63 ||
		registry == nil || lineageRef.ArtifactID == uuid.Nil ||
		!shaPattern.MatchString(lineageRef.SHA256) {
		return scope, ready, intent, task, v2input.ErrInvalid
	}
	firstScope, firstReady, err := ReadDelegatedReadyExport(ctx, db, lease, slot, bucket)
	if err != nil { return scope, ready, intent, task, err }
	lineage := SQLDelegatedLineageStore{DB: db, Lease: lease, Slot: slot,
		Scope: firstScope, Ready: firstReady, Bucket: bucket}
	if err := lineage.VerifyReady(ctx, lineageRef, lineageBody); err != nil {
		return scope, ready, intent, task, err
	}
	compiled, planned, err := CompileDelegatedSignerChild(ctx, lease, slot,
		firstScope, firstReady, lineageRef, lineageBody, registry, resources, now)
	if err != nil { return scope, ready, intent, task, err }
	currentScope, currentReady, err := ReadDelegatedReadyExport(ctx, db, lease, slot, bucket)
	if err != nil { return scope, ready, intent, task, err }
	if currentScope != firstScope || currentReady != firstReady {
		return scope, ready, intent, task, v2input.ErrInvalid
	}
	if err := lineage.VerifyReady(ctx, lineageRef, lineageBody); err != nil {
		return scope, ready, intent, task, err
	}
	if err := ctx.Err(); err != nil { return scope, ready, intent, task, err }
	return firstScope, firstReady, compiled, planned, nil
}
