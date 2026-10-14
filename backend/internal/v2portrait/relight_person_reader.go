package v2portrait

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// SQLRelightPersonReader reads original PersonParams under current scheduler
// authority. It does not grant task admission or verify mask array contents.
type SQLRelightPersonReader struct {
	DB      DonorConfirmationTransactions
	Objects v2input.SessionAnalysisObjectReader
	Bucket  string
}

type RelightPersonProducer = RelightDepthProducer

func (s SQLRelightPersonReader) Load(ctx context.Context, tenant, run uuid.UUID, intent RelightIntent, documents RelightInputDocuments) (RelightPersonProducer, error) {
	reader := SQLRelightDepthReader{DB: s.DB, Objects: s.Objects, Bucket: s.Bucket}
	return reader.loadProducer(ctx, tenant, run, intent, documents.Subject,
		"SELECT photo.v28_relight_intent_person_origin($1,$2,$3)", func(p RelightParams) error {
			_, err := RelightMaskInputs(p, intent.Qualification, documents.Source, documents.Subject, documents.Protected)
			return err
		}, VerifyRelightPersonProducerParams)
}
