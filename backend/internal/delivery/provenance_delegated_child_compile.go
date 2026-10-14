package delivery

import (
	"context"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/provenance"
	"photoeditor/backend/internal/v2input"
)

// CompileDelegatedSignerChild binds the session-free signer task to the exact
// parent expansion slot. Its caller must publish the lineage and Params
// under delegated SQL authority and repeat this scope during atomic admission.
func CompileDelegatedSignerChild(ctx context.Context, lease ExpansionLease, slot Slot,
	scope SignerDelegationScope, ready provenance.ReadySigningExport,
	lineageRef v2input.Ref, lineageBody []byte,
	registry *provenance.SigningPolicyRegistry,
	resources provenance.SigningTaskResources, now time.Time) (provenance.FrozenSigningIntent,
	provenance.FrozenSigningTask, error) {
	var intent provenance.FrozenSigningIntent
	var task provenance.FrozenSigningTask
	if err := ctx.Err(); err != nil { return intent, task, err }
	if err := ValidateSignerDelegationScope(lease, slot, scope); err != nil {
		return intent, task, err
	}
	if ready.Tenant != scope.TenantID || ready.Project != scope.ProjectID ||
		ready.ExportID != scope.ExportID || ready.Ref.ArtifactID != scope.ExportArtifactID ||
		ready.Ref.SHA256 != scope.ExportSHA || scope.PolicyID == uuid.Nil ||
		registry == nil || !shaPattern.MatchString(lineageRef.SHA256) ||
		lineageRef.ArtifactID != uuid.NewSHA1(scope.ExportID,
			[]byte("signing:lineage:"+lineageRef.SHA256)) {
		return intent, task, v2input.ErrInvalid
	}
	policyRef := v2input.Ref{ArtifactID: uuid.NewSHA1(scope.TenantID,
		[]byte("signing:policy:"+scope.PolicyID.String()+":"+scope.PolicySHA)),
		SHA256: scope.PolicySHA}
	params := provenance.SignParams{Schema: 1, Export: ready.Ref,
		Lineage: lineageRef, PolicyID: scope.PolicyID, Policy: policyRef}
	installedRef, _, err := registry.PolicyDocument(ctx, scope.TenantID,
		params, ready.Object.ContentType, now)
	if err != nil { return intent, task, err }
	if installedRef != policyRef { return intent, task, v2input.ErrInvalid }
	compiled, err := provenance.PrepareDelegatedSigningIntent(ctx, scope.ActorID,
		ready, scope.PolicyID, policyRef, scope.IdempotencyKey,
		lineageRef, lineageBody, registry, now)
	if err != nil { return intent, task, err }
	if compiled.WorkflowID() != scope.WorkflowID ||
		compiled.TaskID() != scope.TaskID ||
		compiled.RunID() != uuid.NewSHA1(scope.WorkflowID, []byte("run")) ||
		compiled.ProjectID() != scope.ProjectID {
		return intent, task, v2input.ErrInvalid
	}
	planned, err := provenance.CompileDelegatedSigningTask(ctx, compiled,
		registry, ready.Object.ContentType, resources, now)
	if err != nil { return intent, task, err }
	if planned.Task().ID != scope.TaskID { return intent, task, v2input.ErrInvalid }
	if err := ctx.Err(); err != nil { return intent, task, err }
	return compiled, planned, nil
}
