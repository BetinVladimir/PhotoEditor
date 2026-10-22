package interchange

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/v2input"
)

// Scheduler-scoped installation. Compilation and activation remain separate
// current-authority stages; this factory grants neither accepted status nor ACLs.
func NewSQLImportExecutionPreparation(ctx context.Context, tenant uuid.UUID, documents ImportPreparedDocuments, parsedRaw []byte, deps ImportResolverDependencies, objects ExportExecutionObjects) (ImportExecutionPreparation, error) {
	var zero ImportExecutionPreparation
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if tenant == uuid.Nil || documents.identity.Run == uuid.Nil || documents.identity.Workflow == uuid.Nil || documents.evidence == nil || objects == nil || deps.DB == nil || len(deps.Bucket) < 3 || len(deps.Bucket) > 63 {
		return zero, v2input.ErrInvalid
	}
	parsed := append([]byte(nil), parsedRaw...)
	if _, err := validateParsedImportDocument(ctx, parsed); err != nil {
		return zero, err
	}
	var params importParamsDocument
	if err := decodeImportDocument(documents.params.Body, &params); err != nil {
		return zero, err
	}
	if params.Parsed.SHA256 != rawSHA(parsed) || params.Plan != documents.plan.Ref || params.Evidence == nil || *params.Evidence != documents.evidence.Ref {
		return zero, v2input.ErrInvalid
	}
	for _, doc := range documents.Documents() {
		if err := validateImportStorageDocument(documents.identity, doc); err != nil {
			return zero, err
		}
	}
	var plan importPlanDocument
	if err := decodeImportDocument(documents.plan.Body, &plan); err != nil {
		return zero, err
	}
	// Both reads and writes use the same process-owned object store.
	deps.Objects = objects
	resolver, err := NewSQLImportResolver(tenant, documents.identity.Workflow, plan.Project, plan.Snapshot, documents.plan.Body, parsed, deps)
	if err != nil {
		return zero, err
	}
	store := SQLImportDocumentStore{DB: deps.DB, Tenant: tenant, Identity: documents.identity, Parsed: append([]byte(nil), parsed...), Bucket: deps.Bucket}
	transport := artifactjson.Publisher{Repository: artifactjson.SQLRepository{DB: deps.DB, Bucket: deps.Bucket}, Objects: objects}
	return ImportExecutionPreparation{Documents: ImportStoragePublisher{Tenant: tenant, Store: store, Objects: objects}, Resolver: resolver, Materializer: SQLImportTaskMaterializer{DB: deps.DB, Tenant: tenant, Parsed: append([]byte(nil), parsed...)}, Transport: v2input.ScopedPublisher{Documents: transport, Tenant: tenant, Run: documents.identity.Run}, Registrar: SQLImportInputRegistrar{DB: deps.DB, Tenant: tenant, Run: documents.identity.Run}}, ctx.Err()
}
