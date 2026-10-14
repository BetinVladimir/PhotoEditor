package semanticreview

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"sort"
)

type initializationDocument struct {
	Contract   string    `json:"contract"`
	ArtifactID uuid.UUID `json:"artifact_id"`
	SHA256     string    `json:"sha256"`
	Body       string    `json:"body"`
}

// EncodeInitializationDocuments supplies exact document bytes as JSON strings
// so PostgreSQL can hash them before parsing. It ignores mutable exported intent
// and target projections; the eventual SQL initializer must derive rows itself.
func EncodeInitializationDocuments(prepared PreparedReview) ([]byte, error) {
	if len(prepared.documents) < 6 || len(prepared.documents) > 5005 {
		return nil, ErrInvalid
	}
	var total int64
	for _, document := range prepared.documents {
		if len(document.Body) > 8<<20 {
			return nil, ErrInvalid
		}
		total += int64(len(document.Body))
		if total > 64<<20 {
			return nil, ErrInvalid
		}
	}
	if _, err := PrepareReviewDocuments(prepared.Sources, prepared.documents, prepared.modelSHA, prepared.datasetSHA); err != nil {
		return nil, err
	}
	rows := make([]initializationDocument, len(prepared.documents))
	for n, document := range prepared.documents {
		rows[n] = initializationDocument{document.Source.Contract, document.Source.Ref.ArtifactID, document.Source.Ref.SHA256, string(document.Body)}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Contract != rows[j].Contract {
			return rows[i].Contract < rows[j].Contract
		}
		return rows[i].ArtifactID.String() < rows[j].ArtifactID.String()
	})
	raw, err := canonical.Marshal(rows)
	if err != nil || len(raw) > 128<<20 {
		return nil, ErrInvalid
	}
	return raw, nil
}
