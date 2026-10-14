package provenance

import (
	"bytes"
	"context"
	"encoding/json"
	"photoeditor/backend/internal/canonical"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// SQLSigningImageProducer requires the task catalog's export-ancestry authority,
// including its exact same-project asset-master boundary for cross-run inputs.
type SQLSigningImageProducer struct {
	DB    *pgxpool.Pool
	Tasks SigningTaskCatalog
}

func (s SQLSigningImageProducer) ResolveSigningImageProducer(ctx context.Context, who auth.Identity, ready ReadySigningExport, r v2input.Ref) (uuid.UUID, error) {
	if err := ctx.Err(); err != nil {
		return uuid.Nil, err
	}
	if s.DB == nil || s.Tasks == nil || ready.Tenant == uuid.Nil || ready.Task == uuid.Nil || ready.Run == uuid.Nil || !ref(r) || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return uuid.Nil, ErrInvalid
	}
	final, err := s.Tasks.ReadCanonicalSigningTask(ctx, who, ready, ready.Task)
	if err != nil {
		return uuid.Nil, err
	}
	if final.Task != ready.Task || final.Run != ready.Run || normalizeCanonicalSigningTask(&final) != nil {
		return uuid.Nil, ErrInvalid
	}
	final, err = ownSigningProducerTask(final)
	if err != nil {
		return uuid.Nil, err
	}
	var producer uuid.UUID
	var attempt int
	var fence int64
	err = tenant.WithTenant(ctx, s.DB, who, ready.Tenant, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		return tx.QueryRow(ctx, signingImageProducerSQL, ready.Tenant, r.ArtifactID, r.SHA256).Scan(&producer, &attempt, &fence)
	})
	if err != nil {
		return uuid.Nil, err
	}
	if producer == uuid.Nil || producer == ready.Task {
		return uuid.Nil, ErrInvalid
	}
	task, err := s.Tasks.ReadCanonicalSigningTask(ctx, who, ready, producer)
	if err != nil {
		return uuid.Nil, err
	}
	if task.Task != producer || task.Attempt != attempt || task.Fence != fence || normalizeCanonicalSigningTask(&task) != nil {
		return uuid.Nil, ErrInvalid
	}
	task, err = ownSigningProducerTask(task)
	if err != nil {
		return uuid.Nil, err
	}
	outputs, err := canonicalTaskOutputRefs(task.Outputs)
	if err != nil {
		return uuid.Nil, err
	}
	found := false
	for _, output := range outputs {
		found = found || output == r
	}
	if !found {
		return uuid.Nil, ErrInvalid
	}
	// The task reader performs external authority reads. Recheck the object
	// tuple afterwards instead of trusting the earlier producer association.
	var currentProducer uuid.UUID
	var currentAttempt int
	var currentFence int64
	err = tenant.WithTenant(ctx, s.DB, who, ready.Tenant, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		return tx.QueryRow(ctx, signingImageProducerSQL, ready.Tenant, r.ArtifactID, r.SHA256).Scan(&currentProducer, &currentAttempt, &currentFence)
	})
	if err != nil {
		return uuid.Nil, err
	}
	if currentProducer != producer || currentAttempt != attempt || currentFence != fence {
		return uuid.Nil, ErrInvalid
	}
	currentTask, err := s.Tasks.ReadCanonicalSigningTask(ctx, who, ready, producer)
	if err != nil {
		return uuid.Nil, err
	}
	if !sameSigningProducerTask(task, currentTask) {
		return uuid.Nil, ErrInvalid
	}
	currentFinal, err := s.Tasks.ReadCanonicalSigningTask(ctx, who, ready, ready.Task)
	if err != nil {
		return uuid.Nil, err
	}
	if !sameSigningProducerTask(final, currentFinal) {
		return uuid.Nil, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return uuid.Nil, err
	}
	return producer, nil
}

var _ SigningImageProducer = SQLSigningImageProducer{}

const signingImageProducerSQL = `SELECT a.created_by_task_id,a.produced_attempt,a.produced_fence
   FROM photo.artifacts a
   JOIN photo.tasks t ON t.tenant_id=a.tenant_id AND t.id=a.created_by_task_id
   JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
   WHERE a.tenant_id=$1 AND a.id=$2 AND a.sha256=$3
   AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version NOT IN ('','null')
   AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 1048576
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
   AND t.state='succeeded'
   AND a.produced_attempt=t.attempt AND a.produced_fence=t.fencing_token
   AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
   AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id::text,'sha256',a.sha256))
   FOR SHARE OF a,t,z`

func sameSigningProducerTask(expected, current CanonicalSigningTask) bool {
	if normalizeCanonicalSigningTask(&expected) != nil || normalizeCanonicalSigningTask(&current) != nil {
		return false
	}
	left, err := canonical.Marshal(expected)
	if err != nil {
		return false
	}
	right, err := canonical.Marshal(current)
	return err == nil && bytes.Equal(left, right)
}

func ownSigningProducerTask(task CanonicalSigningTask) (CanonicalSigningTask, error) {
	var owned CanonicalSigningTask
	raw, err := canonical.Marshal(task)
	if err != nil || json.Unmarshal(raw, &owned) != nil {
		return owned, ErrInvalid
	}
	return owned, nil
}
