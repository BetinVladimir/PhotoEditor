package workflow

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
)

type catalogObjects struct {
	rows           *outcomeRows
	bodies         map[string][]byte
	opened, closed int
}
type catalogReadCloser struct {
	*bytes.Reader
	owner *catalogObjects
}

func (c *catalogReadCloser) Close() error { c.owner.closed++; return nil }
func (o *catalogObjects) Open(_ context.Context, obj objectstore.Object) (io.ReadCloser, error) {
	if !o.rows.closed {
		return nil, errors.New("cursor held during IO")
	}
	if obj.VersionID != "v1" {
		return nil, errors.New("version not pinned")
	}
	body, ok := o.bodies[obj.Key]
	if !ok {
		return nil, errors.New("unexpected object")
	}
	o.opened++
	return &catalogReadCloser{bytes.NewReader(body), o}, nil
}
func catalogFixture(t *testing.T) (IngestSpec, []any, *catalogObjects, string) {
	t.Helper()
	img, refs, meta, icc := decodedFixture(t)
	spec := IngestSpec{RunID: uuid.New(), Items: []IngestItem{{Ordinal: 7, Source: meta.Source, Bytes: 1024, Suffix: ".jpg", Tile: 512}}}
	metadataRaw, _ := canonical.Marshal(inspectedAsset{Source: meta.Source, Width: meta.Width, Height: meta.Height, BitDepth: 8, Orientation: 1, Format: "jpeg"})
	mref := metadataRef(metadataRaw)
	imageRaw, _ := canonical.Marshal(img)
	refs.Manifest = metadataRef(imageRaw)
	metadata := inspectedObject{ID: mref.ID, SHA: mref.SHA256, Bucket: "bucket", Key: "metadata", Version: "v1", Bytes: int64(len(metadataRaw)), MediaType: "application/json"}
	manifest := inspectedObject{ID: refs.Manifest.ID, SHA: refs.Manifest.SHA256, Bucket: "bucket", Key: "manifest", Version: "v1", Bytes: int64(len(imageRaw)), MediaType: "application/json"}
	pixels := inspectedObject{ID: refs.Pixels.ID, SHA: refs.Pixels.SHA256, Bucket: "bucket", Key: "pixels", Version: "v1", Bytes: 1440128, MediaType: "application/octet-stream"}
	mraw, _ := canonical.Marshal(metadata)
	oraw, _ := canonical.Marshal([]decodeObject{{manifest, "working_image"}, {pixels, "working_rgb"}})
	task := uuid.New()
	state := "succeeded"
	var absent *string
	row := []any{int64(7), meta.Source.ID, meta.Source.SHA256, "decode", absent, absent, &task, &state, mraw, oraw}
	objects := &catalogObjects{bodies: map[string][]byte{"metadata": metadataRaw, "manifest": imageRaw}}
	return spec, row, objects, icc
}
func TestIngestCatalogLoaderPinsObjectsAndClosesRows(t *testing.T) {
	spec, row, objects, icc := catalogFixture(t)
	rows := &outcomeRows{values: [][]any{row}}
	objects.rows = rows
	batch, err := (IngestCatalogLoader{DB: outcomeDB{rows}, Objects: objects, Bucket: "bucket", WorkICCSHA: icc}).Load(context.Background(), uuid.New(), spec)
	if err != nil || !batch.Ready || len(batch.Images) != 1 || objects.opened != 2 || objects.closed != 2 || batch.Images[0].Image.Canvas.Width != 400 {
		t.Fatal("catalog load failed", err)
	}
}
func TestIngestCatalogLoaderWaitingAndFailuresAvoidObjectIO(t *testing.T) {
	for _, state := range []string{"blocked", "ready", "queued", "running", "retry_wait", "failed", "cancelled"} {
		t.Run(state, func(t *testing.T) {
			spec, row, objects, icc := catalogFixture(t)
			row[7] = &state
			row[9] = []byte(nil)
			rows := &outcomeRows{values: [][]any{row}}
			objects.rows = rows
			batch, err := (IngestCatalogLoader{DB: outcomeDB{rows}, Objects: objects, Bucket: "bucket", WorkICCSHA: icc}).Load(context.Background(), uuid.New(), spec)
			terminal := state == "failed" || state == "cancelled"
			if err != nil || batch.Ready != terminal || len(batch.Images) != 0 || objects.opened != 0 || !rows.closed {
				t.Fatal("pending/failure mishandled", err)
			}
			if terminal && (len(batch.Failures) != 1 || batch.Failures[0].Stage != "asset.decode") {
				t.Fatal("failure dropped")
			}
		})
	}
}
func TestIngestCatalogLoaderRejectsIncompleteAndForeignOutputs(t *testing.T) {
	for _, mode := range []string{"missing", "duplicate ordinal", "unknown role", "missing pixels", "wrong body", "wrong size"} {
		t.Run(mode, func(t *testing.T) {
			spec, row, objects, icc := catalogFixture(t)
			values := [][]any{row}
			switch mode {
			case "missing":
				values = nil
			case "duplicate ordinal":
				values = append(values, row)
			case "unknown role":
				row[9] = bytes.ReplaceAll(row[9].([]byte), []byte("working_rgb"), []byte("other"))
			case "missing pixels":
				row[9] = []byte(`[]`)
			case "wrong body":
				objects.bodies["manifest"] = append(objects.bodies["manifest"], ' ')
			case "wrong size":
				row[8] = bytes.ReplaceAll(row[8].([]byte), []byte(`"bytes":`), []byte(`"bytes":9`))
			}
			rows := &outcomeRows{values: values}
			objects.rows = rows
			if _, err := (IngestCatalogLoader{DB: outcomeDB{rows}, Objects: objects, Bucket: "bucket", WorkICCSHA: icc}).Load(context.Background(), uuid.New(), spec); err == nil || !rows.closed || objects.opened != objects.closed {
				t.Fatal("invalid batch accepted or resource leaked")
			}
		})
	}
}

func TestIngestCatalogLoaderPreservesEarlierFailure(t *testing.T) {
	spec, row, objects, icc := catalogFixture(t)
	stage, reason := "asset.inspect", "failed"
	var task *uuid.UUID
	var state *string
	row[3], row[4], row[5], row[6], row[7], row[8], row[9] = "failed", &stage, &reason, task, state, []byte(nil), []byte(nil)
	rows := &outcomeRows{values: [][]any{row}}
	objects.rows = rows
	batch, err := (IngestCatalogLoader{DB: outcomeDB{rows}, Objects: objects, Bucket: "bucket", WorkICCSHA: icc}).Load(context.Background(), uuid.New(), spec)
	if err != nil || !batch.Ready || len(batch.Failures) != 1 || batch.Failures[0].Stage != stage || batch.Failures[0].Ordinal != 7 || objects.opened != 0 {
		t.Fatal("earlier failure lost", err)
	}
}

func TestIngestCatalogLoaderWholeBatchWaits(t *testing.T) {
	spec, row, objects, icc := catalogFixture(t)
	item := spec.Items[0]
	item.Ordinal = 8
	spec.Items = append(spec.Items, item)
	second := append([]any(nil), row...)
	state := "retry_wait"
	second[0], second[7], second[9] = int64(8), &state, []byte(nil)
	rows := &outcomeRows{values: [][]any{row, second}}
	objects.rows = rows
	batch, err := (IngestCatalogLoader{DB: outcomeDB{rows}, Objects: objects, Bucket: "bucket", WorkICCSHA: icc}).Load(context.Background(), uuid.New(), spec)
	if err != nil || batch.Ready || len(batch.Images) != 0 || len(batch.Failures) != 0 || objects.opened != 0 {
		t.Fatal("partial batch escaped", err)
	}
}
