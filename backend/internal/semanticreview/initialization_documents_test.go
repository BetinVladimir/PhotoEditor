package semanticreview

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

func TestInitializationProofOwnsOriginalBytesAndIgnoresMutatedProjection(t *testing.T) {
	sources, documents, model, dataset := preparedDocumentsFixture(t)
	prepared, err := PrepareReviewDocuments(sources, documents, model, dataset)
	if err != nil {
		t.Fatal(err)
	}
	before, err := EncodeInitializationDocuments(prepared)
	if err != nil {
		t.Fatal(err)
	}
	prepared.Targets[0].State = "queued"
	prepared.Intent.Kind = "retouch_region"
	documents[0].Body[0] = 'x'
	after, err := EncodeInitializationDocuments(prepared)
	if err != nil || string(before) != string(after) {
		t.Fatal("proof mutated", err)
	}
	var rows []initializationDocument
	if json.Unmarshal(after, &rows) != nil || len(rows) != 8 {
		t.Fatal("proof envelope")
	}
	for _, row := range rows {
		sum := sha256.Sum256([]byte(row.Body))
		if hex.EncodeToString(sum[:]) != row.SHA256 {
			t.Fatal("raw bytes lost")
		}
	}
	prepared.Sources.Objects[0].Version = "changed"
	if _, err := EncodeInitializationDocuments(prepared); err == nil {
		t.Fatal("changed inventory accepted")
	}
	if _, err := EncodeInitializationDocuments(PreparedReview{}); err == nil {
		t.Fatal("unprepared value accepted")
	}
}
