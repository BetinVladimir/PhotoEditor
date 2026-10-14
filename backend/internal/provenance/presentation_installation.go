package provenance

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
)

// NewSQLPresentationSigningExecution composes concrete export authority,
// canonical task ancestry, version-pinned JSON reads, canonical producers and
// physically hashed original roots. It supports asset.decode and presentation
// operations only. Render/generative/optical tasks remain explicitly unsupported
// until their adopted-claims resolvers are installed. This does not activate a
// signer, create a workflow, publish lineage or claim SDK qualification.
func NewSQLPresentationSigningExecution(db *pgxpool.Pool, bucket string, objects SigningObjects, workICCSHA string) (SigningExecutionSource, error) {
	if db == nil || objects == nil || !text(bucket, 63) || len(bucket) < 3 || !digest(workICCSHA) {
		return nil, ErrInvalid
	}
	exports := SQLSigningExportCatalog{DB: db, Bucket: bucket}
	tasks := SQLSigningTaskCatalog{DB: db, Exports: exports}
	documents := BoundSigningTaskDocumentReader{Catalog: SQLSigningTaskDocumentCatalog{DB: db, Bucket: bucket, Tasks: tasks}, Objects: objects}
	producers := SigningImageProducerWithRender{Direct: SQLSigningImageProducer{DB: db, Tasks: tasks}, Render: BoundSigningRenderImageProducer{Catalog: SQLSigningRenderImageProducerCatalog{DB: db, Tasks: tasks}, Adoption: SQLSigningRenderAdoptionReader{DB: db, Tasks: tasks}, Documents: documents}}
	originals := BoundSigningOriginalQualifier{Catalog: SQLSigningOriginalCatalog{DB: db, Bucket: bucket, Tasks: tasks}, Objects: objects}
	return BoundSigningExecutionSource{Tasks: tasks, Claims: PresentationSigningTaskResolver{Documents: documents, Producers: producers, Originals: originals, WorkICCSHA: workICCSHA}}, nil
}

// NewSQLPresentationSigningExportSource connects the restricted presentation
// claim graph to dedicated SQL lineage reservation and immutable object I/O.
// Unsupported operation ancestry remains an error. This factory does not
// install a signer, expose SignExport or establish SDK qualification.
func NewSQLPresentationSigningExportSource(db *pgxpool.Pool, bucket string, reads SigningObjects, writes artifactjson.Objects, workICCSHA string) (SigningExportSource, error) {
	if writes == nil {
		return nil, ErrInvalid
	}
	execution, err := NewSQLPresentationSigningExecution(db, bucket, reads, workICCSHA)
	if err != nil {
		return nil, err
	}
	exports := SQLSigningExportCatalog{DB: db, Bucket: bucket}
	publisher := ImmutableSigningLineagePublisher{Catalog: exports, Store: SQLSigningLineageStore{DB: db, Bucket: bucket}, Objects: writes}
	return BoundSigningExportSource{Catalog: exports, Lineages: CompiledExportLineageSource{Execution: execution, Documents: publisher}}, nil
}
