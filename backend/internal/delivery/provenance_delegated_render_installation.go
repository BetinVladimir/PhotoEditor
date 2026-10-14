package delivery

import (
	"photoeditor/backend/internal/provenance"
	"photoeditor/backend/internal/v2input"
)

// NewDelegatedRenderSigningClaims composes the session-free render claim graph
// under one immutable Delivery delegation. Its return value is not installed
// into the public required-provenance route by this constructor.
func NewDelegatedRenderSigningClaims(tasks SQLDelegatedTaskCatalog,
	objects provenance.SigningObjects,
	producers provenance.DelegatedImageProducer) (provenance.DelegatedTaskClaims, error) {
	if tasks.DB == nil || objects == nil || producers == nil ||
		ValidateSignerDelegationScope(tasks.Lease, tasks.Slot, tasks.Scope) != nil ||
		tasks.Ready.Tenant != tasks.Scope.TenantID ||
		tasks.Ready.Project != tasks.Scope.ProjectID ||
		tasks.Ready.ExportID != tasks.Scope.ExportID ||
		tasks.Ready.Ref.ArtifactID != tasks.Scope.ExportArtifactID ||
		tasks.Ready.Ref.SHA256 != tasks.Scope.ExportSHA ||
		len(tasks.Bucket) < 3 || len(tasks.Bucket) > 63 {
		return nil, v2input.ErrInvalid
	}
	documents := DelegatedTaskDocumentReader{Tasks: tasks, Objects: objects}
	adopted := DelegatedAdoptedResultReader{Tasks: tasks, Documents: documents}
	evidence := provenance.BoundDelegatedRenderEvidenceSource{
		Adopted: adopted,
		Documents: documents,
		Counter: provenance.BoundDelegatedRenderMaskCounter{
			Catalog: DelegatedRenderMaskCatalog{Tasks: tasks, Documents: documents, Adopted: adopted},
			Objects: objects,
		},
		Pixels: provenance.BoundDelegatedRenderPixelsVerifier{
			Catalog: DelegatedRenderPixelsCatalog{Tasks: tasks, Documents: documents, Adopted: adopted},
			Objects: objects,
		},
		Qualification: DelegatedRenderQualificationCatalog{Tasks: tasks, Adopted: adopted},
		Parents: DelegatedRenderParentCatalog{Tasks: tasks, Adopted: adopted},
		Reports: DelegatedParentReportReader{Tasks: tasks, Documents: documents},
	}
	closure := provenance.BoundDelegatedRenderClosureSource{
		Documents: documents,
		Closure: DelegatedRenderClosureResolver{
			Inputs: DelegatedRenderInputCatalog{Tasks: tasks}, Objects: objects,
		},
	}
	causal := provenance.BoundDelegatedRenderCausalSource{
		Evidence: evidence, Closure: closure, Producers: producers,
	}
	reviewed := provenance.BoundDelegatedRenderReviewedCausalSource{
		Causal: causal,
		Reviews: DelegatedCandidateReviewReader{Tasks: tasks, Documents: documents},
	}
	return provenance.DelegatedRenderSigningTaskResolver{
		Reviewed: reviewed,
		Dependencies: provenance.BoundDelegatedRenderDependencyResolver{Causal: causal},
	}, nil
}
