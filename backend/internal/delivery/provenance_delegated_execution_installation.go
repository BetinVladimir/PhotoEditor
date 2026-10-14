package delivery

import (
	"context"

	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/provenance"
	"photoeditor/backend/internal/v2input"
)

// NewDelegatedSigningExecutionReader binds every supported V31 graph claim
// to one Delivery parent lease. It does not dispatch a signer or enable the
// public required-provenance mode.
func NewDelegatedSigningExecutionReader(tasks SQLDelegatedTaskCatalog,
	objects provenance.SigningObjects,
	negativePolicies *provenance.SigningNegativePolicyRegistry,
	workICCSHA string) (BoundDelegatedSigningExecutionReader, error) {
	var zero BoundDelegatedSigningExecutionReader
	if tasks.DB == nil || objects == nil || negativePolicies == nil ||
		!shaPattern.MatchString(workICCSHA) ||
		ValidateSignerDelegationScope(tasks.Lease, tasks.Slot, tasks.Scope) != nil ||
		tasks.Ready.Tenant != tasks.Scope.TenantID ||
		tasks.Ready.Project != tasks.Scope.ProjectID ||
		tasks.Ready.ExportID != tasks.Scope.ExportID ||
		tasks.Ready.Ref.ArtifactID != tasks.Scope.ExportArtifactID ||
		tasks.Ready.Ref.SHA256 != tasks.Scope.ExportSHA ||
		len(tasks.Bucket) < 3 || len(tasks.Bucket) > 63 {
		return zero, v2input.ErrInvalid
	}
	documents := DelegatedTaskDocumentReader{Tasks: tasks, Objects: objects}
	producers := DelegatedImageProducerRouter{
		Direct: DelegatedDirectImageProducer{Tasks: tasks},
		Adopted: DelegatedAdoptedImageProducer{Tasks: tasks, Documents: documents},
	}
	render, err := NewDelegatedRenderSigningClaims(tasks, objects, producers)
	if err != nil { return zero, err }
	negative, err := NewDelegatedNegativeSigningClaims(tasks, objects,
		negativePolicies, producers)
	if err != nil { return zero, err }
	claims := provenance.DelegatedPresentationTaskResolver{
		Documents: documents,
		Producers: producers,
		Originals: DelegatedOriginalQualifier{Tasks: tasks, Objects: objects},
		WorkICCSHA: workICCSHA,
		Render: render,
		Negative: negative,
	}
	return BoundDelegatedSigningExecutionReader{Catalog: tasks, Claims: claims}, nil
}

// CompileAndPublishCurrentDelegatedLineage installs the scoped graph reader
// into the existing repeat-before/after immutable lineage coordinator.
func CompileAndPublishCurrentDelegatedLineage(ctx context.Context,
	store SQLDelegatedLineageStore, signingObjects provenance.SigningObjects,
	lineageObjects artifactjson.Objects,
	negativePolicies *provenance.SigningNegativePolicyRegistry,
	workICCSHA string) (v2input.Ref, []byte, error) {
	if err := ctx.Err(); err != nil { return v2input.Ref{}, nil, err }
	if lineageObjects == nil { return v2input.Ref{}, nil, v2input.ErrInvalid }
	tasks := SQLDelegatedTaskCatalog{DB: store.DB, Lease: store.Lease,
		Slot: store.Slot, Scope: store.Scope, Ready: store.Ready,
		Bucket: store.Bucket}
	reader, err := NewDelegatedSigningExecutionReader(tasks, signingObjects,
		negativePolicies, workICCSHA)
	if err != nil { return v2input.Ref{}, nil, err }
	return CompileAndPublishDelegatedLineage(ctx, store, reader, lineageObjects)
}
