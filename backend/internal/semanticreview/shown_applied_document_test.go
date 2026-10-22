package semanticreview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"os"
	"photoeditor/backend/internal/auth"
	"testing"
)

type shownDocumentQuery struct {
	source PlanSource
	calls  int
	change bool
}
type shownDocumentRow struct{ source PlanSource }

func (q *shownDocumentQuery) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	q.calls++
	s := q.source
	if q.change && q.calls > 1 {
		s.Version = "replaced"
	}
	return shownDocumentRow{s}
}
func (r shownDocumentRow) Scan(dest ...any) error {
	*dest[0].(*string) = r.source.Contract
	*dest[1].(*uuid.UUID) = r.source.Ref.ArtifactID
	*dest[2].(*string) = r.source.Ref.SHA256
	*dest[3].(*int64) = r.source.Bytes
	*dest[4].(*string) = r.source.Bucket
	*dest[5].(*string) = r.source.Key
	*dest[6].(*string) = r.source.Version
	*dest[7].(*string) = r.source.MediaType
	return nil
}
func TestShownDocumentRejectsObjectVersionChangeDuringRead(t *testing.T) {
	fixture, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if err := json.Unmarshal(fixture, &images); err != nil || len(images) == 0 {
		t.Fatal("fixture")
	}
	raw := []byte(images[0])
	sum := sha256.Sum256(raw)
	q := &shownDocumentQuery{change: true, source: PlanSource{Contract: "WorkingImage", Bytes: int64(len(raw)), Bucket: "configured", Key: "image", Version: "pinned", MediaType: "application/json"}}
	q.source.Ref.ArtifactID = uuid.New()
	q.source.Ref.SHA256 = hex.EncodeToString(sum[:])
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	_, err = ReadShownAppliedDocument(context.Background(), q, who, uuid.New(), uuid.New(), "configured", func(_ context.Context, s PlanSource) ([]byte, error) {
		if s.Version != "pinned" {
			t.Fatal("unpinned read")
		}
		return raw, nil
	})
	if !errors.Is(err, ErrConflict) || q.calls != 2 {
		t.Fatalf("calls=%d error=%v", q.calls, err)
	}
}
func TestShownDocumentRefusesForeignBucketBeforeObjectIO(t *testing.T) {
	q := &shownDocumentQuery{source: PlanSource{Contract: "WorkingImage", Bytes: 2, Bucket: "foreign", Key: "image", Version: "pinned", MediaType: "application/json"}}
	q.source.Ref.ArtifactID = uuid.New()
	q.source.Ref.SHA256 = hex.EncodeToString(make([]byte, 32))
	_, err := ReadShownAppliedDocument(context.Background(), q, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, uuid.New(), uuid.New(), "configured", func(context.Context, PlanSource) ([]byte, error) { t.Fatal("foreign object IO"); return nil, nil })
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("error=%v", err)
	}
}
