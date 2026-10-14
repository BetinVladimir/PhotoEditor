package provenance

import "github.com/jackc/pgx/v5/pgxpool"

// NewSQLSigningRenderEvidence assembles all scoped historical evidence readers.
// Construction performs no IO and grants no signing action or human approval.
func NewSQLSigningRenderEvidence(db *pgxpool.Pool, bucket string, tasks SigningTaskCatalog, objects SigningObjects) (BoundSigningRenderEvidenceSource, error) {
	if db == nil || tasks == nil || objects == nil || len(bucket) < 3 || !text(bucket, 63) {
		return BoundSigningRenderEvidenceSource{}, ErrInvalid
	}
	adoption := SQLSigningRenderAdoptionReader{DB: db, Tasks: tasks}
	documents := BoundSigningTaskDocumentReader{Catalog: SQLSigningTaskDocumentCatalog{DB: db, Bucket: bucket, Tasks: tasks}, Objects: objects}
	masks := SQLSigningRenderMaskCatalog{DB: db, Bucket: bucket, Adoption: adoption, Documents: documents}
	return BoundSigningRenderEvidenceSource{
		Adoption: adoption, Documents: documents,
		Counter: BoundSigningRenderMaskCounter{Catalog: masks, Objects: objects},
		Parents: SQLSigningRenderParentReader{DB: db, Adoption: adoption, Tasks: tasks},
		Reports: SQLSigningReportCatalog{DB: db, Bucket: bucket, Tasks: tasks}, Objects: objects,
		Qualification: SQLSigningRenderQualificationReader{DB: db, Adoption: adoption},
	}, nil
}

// NewSQLSigningRenderCausalSource wires the physical receipt join to the same
// historical task catalogue and producer adoption readers. It remains a private
// resolver stage: model/control authority and review are required separately.
func NewSQLSigningRenderCausalSource(db *pgxpool.Pool, bucket string, tasks SigningTaskCatalog, objects SigningObjects) (BoundSigningRenderCausalSource, error) {
	evidence, err := NewSQLSigningRenderEvidence(db, bucket, tasks, objects)
	if err != nil {
		return BoundSigningRenderCausalSource{}, err
	}
	adoption := SQLSigningRenderAdoptionReader{DB: db, Tasks: tasks}
	documents := BoundSigningTaskDocumentReader{Catalog: SQLSigningTaskDocumentCatalog{DB: db, Bucket: bucket, Tasks: tasks}, Objects: objects}
	return BoundSigningRenderCausalSource{
		Evidence: evidence,
		Closure:  SQLSigningRenderClosureReader{DB: db, Bucket: bucket, Tasks: tasks, Objects: objects},
		Producers: SigningImageProducerWithRender{
			Direct: SQLSigningImageProducer{DB: db, Tasks: tasks},
			Render: BoundSigningRenderImageProducer{Catalog: SQLSigningRenderImageProducerCatalog{DB: db, Tasks: tasks}, Adoption: adoption, Documents: documents},
		},
	}, nil
}

// NewSQLSigningRenderReviewedCausalSource composes historical physical evidence
// and exact candidate review. Construction does not create a signer or enable
// render operation routing in the export graph.
func NewSQLSigningRenderReviewedCausalSource(db *pgxpool.Pool, bucket string, tasks SigningTaskCatalog, objects SigningObjects) (BoundSigningRenderReviewedCausalSource, error) {
	causal, err := NewSQLSigningRenderCausalSource(db, bucket, tasks, objects)
	if err != nil {
		return BoundSigningRenderReviewedCausalSource{}, err
	}
	adoption := SQLSigningRenderAdoptionReader{DB: db, Tasks: tasks}
	documents := BoundSigningTaskDocumentReader{Catalog: SQLSigningTaskDocumentCatalog{DB: db, Bucket: bucket, Tasks: tasks}, Objects: objects}
	return BoundSigningRenderReviewedCausalSource{Causal: causal, Reviews: SQLSigningRenderReviewReader{DB: db, Adoption: adoption, Documents: documents}}, nil
}
