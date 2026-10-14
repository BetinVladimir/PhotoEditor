package provenance

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

type signingSourcePins struct {
	Project       uuid.UUID `json:"project"`
	Version       uuid.UUID `json:"version"`
	Task          uuid.UUID `json:"task"`
	Run           uuid.UUID `json:"run"`
	Artifact      uuid.UUID `json:"artifact"`
	SHA           string    `json:"sha256"`
	Key           string    `json:"key"`
	ObjectVersion string    `json:"object_version"`
	Bytes         int64     `json:"bytes"`
	MIME          string    `json:"media_type"`
}
type signingSavedProfile struct {
	Backend     string `json:"backend"`
	Renderer    string `json:"renderer_version"`
	Pool        string `json:"execution_pool"`
	Contracts   string `json:"contracts_sha"`
	MaxJSON     int64  `json:"max_input_json_bytes"`
	SDK         string `json:"sdk_lock_sha"`
	Certificate string `json:"certificate_chain_sha"`
	Trust       string `json:"trust_bundle_sha"`
	Estimate    struct {
		RAM     int64 `json:"ram_bytes"`
		VRAM    int64 `json:"vram_bytes"`
		Scratch int64 `json:"scratch_bytes"`
		Output  int64 `json:"output_bytes"`
		Pixels  int64 `json:"pixel_count"`
	} `json:"resource_estimate"`
}

// RestoreSigningPending validates historical bytes and recompiles with the
// currently installed policy/operator. It grants no current source authority:
// SigningActivationPreparation must freshly re-resolve and requalify afterward.
// Current authenticated session replaces the old session; actor/key stay fixed.
func RestoreSigningPending(ctx context.Context, who auth.Identity, tid, export uuid.UUID, key, requestSHA string, raw []byte, registry *SigningPolicyRegistry, now time.Time) (FrozenSigningIntent, FrozenSigningTask, ReadySigningExport, error) {
	fail := func(err error) (FrozenSigningIntent, FrozenSigningTask, ReadySigningExport, error) {
		return FrozenSigningIntent{}, FrozenSigningTask{}, ReadySigningExport{}, err
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	if who.UserID == uuid.Nil || who.SessionID == uuid.Nil || tid == uuid.Nil || export == uuid.Nil || !idempotency.ValidKey(key) || !digest(requestSHA) {
		return fail(ErrInvalid)
	}
	var saved signingPendingDocument
	if decode(raw, &saved, 10<<20) != nil || saved.Schema != 1 || (saved.QueueClass != "" && saved.QueueClass != "batch") || saved.Tenant != tid || saved.Actor != who.UserID || saved.Export != export || saved.RequestSHA != requestSHA || bodyDigest([]byte(saved.Request)) != requestSHA {
		return fail(ErrInvalid)
	}
	var pins signingSourcePins
	if decode(saved.Source, &pins, 16384) != nil {
		return fail(ErrInvalid)
	}
	ready := ReadySigningExport{Tenant: tid, Project: pins.Project, ExportID: export, Version: pins.Version, Task: pins.Task, Run: pins.Run, Ref: v2input.Ref{ArtifactID: pins.Artifact, SHA256: pins.SHA}, Object: objectstore.Object{Key: pins.Key, VersionID: pins.ObjectVersion, Bytes: pins.Bytes, SHA256: pins.SHA, ContentType: pins.MIME}}
	intent := FrozenSigningIntent{legacyQueueOmitted: saved.QueueClass == "", tenant: tid, project: pins.Project, export: export, actor: who.UserID, session: who.SessionID, key: key, workflow: saved.Workflow, run: saved.Run, task: saved.TaskID, requestSHA: requestSHA, request: []byte(saved.Request), params: []byte(saved.Params), lineage: []byte(saved.Lineage)}
	var profile signingSavedProfile
	if decode(saved.Task.ExecutionProfile, &profile, 65536) != nil || profile.Backend != "cpu" || profile.Renderer != "v2.2" || profile.Pool != "signer" || profile.Estimate.VRAM != 0 || profile.Estimate.Pixels != 0 {
		return fail(ErrInvalid)
	}
	resources := SigningTaskResources{ContractsSHA: profile.Contracts, RAMBytes: profile.Estimate.RAM, ScratchBytes: profile.Estimate.Scratch, OutputBytes: profile.Estimate.Output, MaxInputJSONBytes: profile.MaxJSON}
	compiled, err := CompileSigningTask(ctx, intent, registry, pins.MIME, resources, now)
	if err != nil {
		return fail(err)
	}
	// Full canonical roundtrip covers exact profile pins, task, typed inputs,
	// source metadata, request fields and Params/lineage identity/hash/limits.
	expected, err := compileSigningPending(who, ready, PreparedSigningActivation{intent: intent, compiled: compiled, mime: pins.MIME}, saved.Bucket)
	if err != nil {
		return fail(err)
	}
	if !bytes.Equal(expected, raw) {
		return fail(ErrInvalid)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	return cloneSigningIntent(intent), cloneSigningTask(compiled), ready, nil
}

type SQLSigningPendingReader struct {
	DB       *pgxpool.Pool
	Registry *SigningPolicyRegistry
	Clock    func() time.Time
}

// Read commits its short metadata transaction before invoking operator checks.
// It reads only the exact unexpired pending actor/key/hash; no recovery evidence
// is accepted from an HTTP payload. Current source qualification is subsequent.
func (s SQLSigningPendingReader) Read(ctx context.Context, who auth.Identity, tid, export uuid.UUID, key, requestSHA string) (FrozenSigningIntent, FrozenSigningTask, ReadySigningExport, error) {
	var i FrozenSigningIntent
	var task FrozenSigningTask
	var ready ReadySigningExport
	if err := ctx.Err(); err != nil {
		return i, task, ready, err
	}
	if s.DB == nil || s.Registry == nil || s.Clock == nil || !idempotency.ValidKey(key) || !digest(requestSHA) || tid == uuid.Nil || export == uuid.Nil {
		return i, task, ready, ErrInvalid
	}
	var raw []byte
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		return tx.QueryRow(ctx, `SELECT response_body FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_type='user' AND principal_id=$2 AND route='Provenance.Sign.Pending' AND key=$3 AND request_hash=$4 AND response_status IS NULL AND response_body IS NOT NULL AND expires_at>clock_timestamp() AND octet_length(response_body::text)<=10485760`, tid, who.UserID, key, requestSHA).Scan(&raw)
	})
	if err != nil {
		return i, task, ready, err
	}
	// JSONB is not JCS: normalize DB serialization only, then strict typed decode.
	normalized, err := canonical.Bytes(json.RawMessage(raw))
	if err != nil {
		return i, task, ready, err
	}
	return RestoreSigningPending(ctx, who, tid, export, key, requestSHA, normalized, s.Registry, s.Clock())
}
