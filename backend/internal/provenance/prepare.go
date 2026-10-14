package provenance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"time"
)

// Source must authorize current tenant/editor/project/session and pin a ready
// export after all encoding/watermark/metadata operations. Lineage comes from
// its frozen execution graph, never from a request body.
type SigningExportSource interface {
	ResolveSigningExport(context.Context, auth.Identity, uuid.UUID, uuid.UUID) (SigningExportEvidence, error)
}
type SigningExportEvidence struct {
	Project     uuid.UUID
	ExportID    uuid.UUID
	Export      v2input.Ref
	MIME        string
	Lineage     v2input.Ref
	LineageBody []byte
}
type FrozenSigningIntent struct {
 legacyQueueOmitted bool
	delegated bool
	tenant, project, export, workflow, run, task uuid.UUID
	actor, session                               uuid.UUID
	key                                          string
	params, request, lineage                     []byte
	requestSHA                                   string
}

func (i FrozenSigningIntent) WorkflowID() uuid.UUID { return i.workflow }
func (i FrozenSigningIntent) RunID() uuid.UUID      { return i.run }
func (i FrozenSigningIntent) TaskID() uuid.UUID     { return i.task }
func (i FrozenSigningIntent) ProjectID() uuid.UUID  { return i.project }
func (i FrozenSigningIntent) ParamsBytes() []byte   { return append([]byte(nil), i.params...) }
func (i FrozenSigningIntent) LineageBytes() []byte  { return append([]byte(nil), i.lineage...) }
func (i FrozenSigningIntent) RequestBytes() []byte  { return append([]byte(nil), i.request...) }
func (i FrozenSigningIntent) RequestSHA() string    { return i.requestSHA }
func PrepareSigningIntent(ctx context.Context, who auth.Identity, tenant, export, policy uuid.UUID, key string, source SigningExportSource, registry *SigningPolicyRegistry, now time.Time) (FrozenSigningIntent, error) {
	var zero FrozenSigningIntent
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if who.UserID == uuid.Nil || who.SessionID == uuid.Nil || tenant == uuid.Nil || export == uuid.Nil || policy == uuid.Nil || len(key) < 1 || len(key) > 128 || source == nil || registry == nil || !registry.matchesTenant(tenant) {
		return zero, ErrInvalid
	}
	for _, c := range []byte(key) {
		if c < 33 || c > 126 {
			return zero, ErrInvalid
		}
	}
	installed, exists := registry.policies[policy]
	if !exists {
		return zero, ErrInvalid
	}
	evidence, err := source.ResolveSigningExport(ctx, who, tenant, export)
	if err != nil {
		return zero, err
	}
	if len(evidence.LineageBody) < 2 || len(evidence.LineageBody) > 8<<20 {
		return zero, ErrInvalid
	}
	evidence.LineageBody = append([]byte(nil), evidence.LineageBody...)
	if evidence.Project == uuid.Nil || evidence.ExportID != export || !ref(evidence.Export) {
		return zero, ErrInvalid
	}
	if _, err := DecodeSigningLineage(evidence.LineageBody, evidence.Lineage); err != nil {
		return zero, err
	}
	params := SignParams{Schema: 1, Export: evidence.Export, Lineage: evidence.Lineage, PolicyID: policy, Policy: installed.ref}
	if _, err := registry.Resolve(ctx, params, evidence.MIME, now); err != nil {
		return zero, err
	}
	paramsRaw, err := canonical.Marshal(params)
	if err != nil {
		return zero, err
	}
	requestRaw, err := canonical.Marshal(struct {
		Export uuid.UUID `json:"export_id"`
		Policy uuid.UUID `json:"signing_policy_id"`
	}{export, policy})
	if err != nil {
		return zero, err
	}
	// Detect replacement or revocation while preparing. Atomic admission must
	// repeat these predicates under its own locks before dispatch.
	current, err := source.ResolveSigningExport(ctx, who, tenant, export)
	if err != nil {
		return zero, err
	}
	if current.Project != evidence.Project || current.ExportID != evidence.ExportID || current.Export != evidence.Export || current.MIME != evidence.MIME || current.Lineage != evidence.Lineage || !bytes.Equal(current.LineageBody, evidence.LineageBody) {
		return zero, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	workflow := uuid.NewSHA1(tenant, []byte("provenance:"+who.UserID.String()+":"+key))
	hash := sha256.Sum256(requestRaw)
	return FrozenSigningIntent{actor: who.UserID, session: who.SessionID, key: key, tenant: tenant, project: evidence.Project, export: export, workflow: workflow, run: uuid.NewSHA1(workflow, []byte("run")), task: uuid.NewSHA1(workflow, []byte("provenance.sign:0")), params: paramsRaw, request: requestRaw, lineage: evidence.LineageBody, requestSHA: hex.EncodeToString(hash[:])}, nil
}
