package provenance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// PrepareDelegatedSigningIntent freezes a scheduler child from independently
// proven parent scope, ready export and lineage. It supplies no editor session;
// only delegated SQL admission may accept the resulting IDs and Params.
func PrepareDelegatedSigningIntent(ctx context.Context, actor uuid.UUID,
	ready ReadySigningExport, policyID uuid.UUID, policyRef v2input.Ref,
	key string, lineage v2input.Ref, lineageBody []byte,
	registry *SigningPolicyRegistry, now time.Time) (FrozenSigningIntent, error) {
	var zero FrozenSigningIntent
	if err := ctx.Err(); err != nil { return zero, err }
	if actor == uuid.Nil || ready.Tenant == uuid.Nil || ready.Project == uuid.Nil ||
		ready.ExportID == uuid.Nil || ready.Version == uuid.Nil ||
		ready.Task == uuid.Nil || ready.Run == uuid.Nil || !ref(ready.Ref) ||
		ready.Object.SHA256 != ready.Ref.SHA256 || ready.Object.Key == "" ||
		ready.Object.VersionID == "" || ready.Object.VersionID == "null" ||
		ready.Object.Bytes < 1 || ready.Object.Bytes > 512<<20 ||
		(ready.Object.ContentType != "image/jpeg" && ready.Object.ContentType != "image/png") ||
		policyID == uuid.Nil || !ref(policyRef) || !ref(lineage) ||
		len(lineageBody) < 2 || len(lineageBody) > 8<<20 ||
		len(key) < 1 || len(key) > 128 || registry == nil ||
		!registry.matchesTenant(ready.Tenant) || now.IsZero() {
		return zero, ErrInvalid
	}
	for _, c := range []byte(key) {
		if c < 33 || c > 126 { return zero, ErrInvalid }
	}
	installed, exists := registry.policies[policyID]
	if !exists || installed.ref != policyRef { return zero, ErrInvalid }
	body := append([]byte(nil), lineageBody...)
	if _, err := DecodeSigningLineage(body, lineage); err != nil { return zero, err }
	params := SignParams{Schema: 1, Export: ready.Ref, Lineage: lineage,
		PolicyID: policyID, Policy: policyRef}
	if _, err := registry.Resolve(ctx, params, ready.Object.ContentType, now); err != nil {
		return zero, err
	}
	paramsRaw, err := canonical.Marshal(params)
	if err != nil { return zero, err }
	requestRaw, err := canonical.Marshal(struct {
		Export uuid.UUID `json:"export_id"`
		Policy uuid.UUID `json:"signing_policy_id"`
	}{ready.ExportID, policyID})
	if err != nil { return zero, err }
	workflow := uuid.NewSHA1(ready.Tenant,
		[]byte("provenance:"+actor.String()+":"+key))
	hash := sha256.Sum256(requestRaw)
	if err := ctx.Err(); err != nil { return zero, err }
	return FrozenSigningIntent{delegated: true, tenant: ready.Tenant,
		project: ready.Project, export: ready.ExportID, actor: actor,
		key: key, workflow: workflow,
		run: uuid.NewSHA1(workflow, []byte("run")),
		task: uuid.NewSHA1(workflow, []byte("provenance.sign:0")),
		params: paramsRaw, request: requestRaw, lineage: body,
		requestSHA: hex.EncodeToString(hash[:])}, nil
}
