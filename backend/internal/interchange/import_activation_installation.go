package interchange

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// Builds a fresh scoped resolver for the final physical re-read. The resource
// authority must come from a pinned operator report, never request estimates.
// This installation neither changes task state nor replaces final SQL admission.
func NewSQLImportActivationPreparation(ctx context.Context, tenant uuid.UUID, documents ImportPreparedDocuments, parsedRaw []byte, deps ImportResolverDependencies, resources *MeasuredImportResourceQualifier) (ImportActivationPreparation, error) {
	var zero ImportActivationPreparation
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if tenant == uuid.Nil || documents.identity.Run == uuid.Nil || documents.identity.Workflow == uuid.Nil || documents.evidence == nil || resources == nil || deps.DB == nil || deps.Objects == nil {
		return zero, v2input.ErrInvalid
	}
	parsed := append([]byte(nil), parsedRaw...)
	if _, err := validateParsedImportDocument(ctx, parsed); err != nil {
		return zero, err
	}
	for _, doc := range documents.Documents() {
		if err := validateImportStorageDocument(documents.identity, doc); err != nil {
			return zero, err
		}
	}
	var params importParamsDocument
	if err := decodeImportDocument(documents.params.Body, &params); err != nil {
		return zero, err
	}
	if params.Parsed.SHA256 != rawSHA(parsed) || params.Plan != documents.plan.Ref || params.Evidence == nil || *params.Evidence != documents.evidence.Ref {
		return zero, v2input.ErrInvalid
	}
	var plan importPlanDocument
	if err := decodeImportDocument(documents.plan.Body, &plan); err != nil {
		return zero, err
	}
	resolver, err := NewSQLImportResolver(tenant, documents.identity.Workflow, plan.Project, plan.Snapshot, documents.plan.Body, parsed, deps)
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return ImportActivationPreparation{Resolver: resolver, Resources: resources, Reviews: deps.Reviews}, nil
}
