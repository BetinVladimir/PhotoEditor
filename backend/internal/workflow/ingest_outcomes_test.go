package workflow

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"testing"
)

type outcomeRows struct {
	pgx.Rows
	values [][]any
	index  int
	closed bool
}

func (r *outcomeRows) Next() bool { r.index++; return r.index <= len(r.values) }
func (r *outcomeRows) Scan(dest ...any) error {
	return (expansionRow{values: r.values[r.index-1]}).Scan(dest...)
}
func (r *outcomeRows) Close()     { r.closed = true }
func (r *outcomeRows) Err() error { return nil }

type outcomeDB struct{ rows *outcomeRows }

func (d outcomeDB) Query(context.Context, string, ...any) (pgx.Rows, error) { return d.rows, nil }
func TestIngestOutcomeReaderPinsVersionAndChecksSize(t *testing.T) {
	spec, _ := ingestFixture(t)
	spec.Items = spec.Items[:1]
	item := spec.Items[0]
	raw, _ := canonical.Marshal(inspectedAsset{Source: item.Source, Width: 400, Height: 300, BitDepth: 8, Orientation: 1, Format: "png"})
	ref := metadataRef(raw)
	for _, wrongSize := range []bool{false, true} {
		size := int64(len(raw))
		if wrongSize {
			size++
		}
		manifest, _ := canonical.Marshal(inspectedObject{ID: ref.ID, SHA: ref.SHA256, Bucket: "test-bucket", Key: "metadata/object", Version: "v1", Bytes: size, MediaType: "application/json"})
		var reason *string
		rows := &outcomeRows{values: [][]any{{item.Ordinal, Succeeded, Succeeded, reason, item.Source.ID, item.Source.SHA256, manifest}}}
		objects := &artifactObjects{body: raw}
		reader := IngestOutcomeReader{DB: outcomeDB{rows}, Objects: objects, Bucket: "test-bucket"}
		outcomes, err := reader.Load(context.Background(), uuid.New(), spec)
		if !objects.closed || !rows.closed || objects.object.VersionID != "v1" {
			t.Fatal("resources leaked or unpinned object")
		}
		if wrongSize {
			if err == nil {
				t.Fatal("wrong DB size accepted")
			}
		} else if err != nil || len(outcomes) != 1 || outcomes[0].Metadata.Width != 400 {
			t.Fatal("outcome load failed", err)
		}
	}
}
