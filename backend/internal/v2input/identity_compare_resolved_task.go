package v2input

import (
	"context"

	"github.com/google/uuid"
)

// CompileResolvedIdentityCompareTask is the production admission entry point:
// the policy/report pin comes from the scheduler's scoped SQL catalog and
// physically verified report bytes, never an API-supplied qualification.
func CompileResolvedIdentityCompareTask(ctx context.Context, workflow uuid.UUID,
	ordinal int, query IdentityQualificationQuery, params IdentityCompareParams,
	paramsRef Ref, documents IdentityCompareDocuments,
	sizes []SemanticInputSize, costs SemanticQualifiedCosts,
	ramLimit, vramLimit, scratchLimit, outputLimit int64,
	catalog IdentityQualificationCatalog,
	objects SessionAnalysisObjectReader) (FrozenIdentityCompareTask, error) {
	var zero FrozenIdentityCompareTask
	if query.Tenant == uuid.Nil || query.Project == uuid.Nil ||
		query.ModelLockSHA != params.ModelLockSHA ||
		query.Calibration != params.Calibration ||
		(query.Backend != "cpu" && query.Backend != "cuda") {
		return zero, ErrInvalid
	}
	pin, err := ResolveQualifiedIdentityPin(ctx, query, documents.Calibration,
		catalog, objects)
	if err != nil {
		return zero, err
	}
	return CompileQualifiedIdentityCompareTask(ctx, workflow, ordinal,
		params, paramsRef, documents, sizes, costs, pin,
		ramLimit, vramLimit, scratchLimit, outputLimit)
}
