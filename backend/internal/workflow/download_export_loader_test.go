package workflow

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
)

type downloadRows struct {
	pgx.Rows
	values [][]any
	index  int
	closed bool
}

func (r *downloadRows) Next() bool { return r.index < len(r.values) }
func (r *downloadRows) Scan(dest ...any) error {
	for index, value := range r.values[r.index] {
		reflect.ValueOf(dest[index]).Elem().Set(reflect.ValueOf(value))
	}
	r.index++
	return nil
}
func (r *downloadRows) Close()     { r.closed = true }
func (r *downloadRows) Err() error { return nil }

type downloadLoaderDB struct {
	contextRows, inputRows *downloadRows
	calls                  int
}

func (d *downloadLoaderDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	d.calls++
	if d.calls == 1 {
		return d.contextRows, nil
	}
	return d.inputRows, nil
}

type downloadObjects struct{ body map[string][]byte }

func (o downloadObjects) Open(_ context.Context, object objectstore.Object) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(o.body[object.Key])), nil
}

func downloadLoaderFixture(t *testing.T) (DownloadExportLoader, uuid.UUID, uuid.UUID, *downloadLoaderDB) {
	t.Helper()
	tenant, run, job, snapshot, version := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	masterID, pixelID := uuid.New(), uuid.New()
	workSHA, targetSHA := downloadDigest("work"), downloadDigest("target")
	sourceSHA := downloadDigest("source")
	chain, err := canonical.Marshal(struct {
		Steps []any `json:"steps"`
	}{Steps: []any{}})
	if err != nil {
		t.Fatal(err)
	}
	chainHash, _ := canonical.Hash(struct {
		Steps []any `json:"steps"`
	}{Steps: []any{}})
	image := review.WorkingImage{
		Pixels: pixelRef(pixelID), Canvas: review.Canvas{Width: 8, Height: 4, Orientation: 1, SourceSHA256: sourceSHA, SpaceID: "oriented:" + sourceSHA},
		Color: review.ColorSpace{ProfileID: "work_rec2020_gamma24_v1", ICCSHA256: workSHA, Encoding: "gamma24"},
		DType: "float32", TransformChain: append([]byte(nil), chain...),
	}
	// ParseWorkingImage requires the transform-chain hash inside the object.
	image.TransformChain, _ = canonical.Marshal(struct {
		Steps []any  `json:"steps"`
		Hash  string `json:"hash"`
	}{[]any{}, chainHash})
	raw, err := canonical.Marshal(image)
	if err != nil {
		t.Fatal(err)
	}
	masterSHA := downloadDigest(string(raw))
	quality := 95
	preset := DownloadPreset{Format: "jpeg", ICCID: "srgb_v1", Quality: &quality, Subsampling: "444", Alpha: "matte", MatteRGB: []float64{1, 1, 1}, MetadataPolicy: "strip"}
	presetHash, _ := canonical.Hash(preset)
	envelope, _ := canonical.Marshal(downloadPresetEnvelope{ID: uuid.New(), Revision: 1, Hash: presetHash, Config: preset})
	contextRows := &downloadRows{values: [][]any{{job, snapshot, downloadDigest("snapshot"), int64(1), envelope, int64(0), version,
		masterID, masterSHA, "photos", "master", "v1", int64(len(raw)), "application/json"}}}
	inputRows := &downloadRows{values: [][]any{{int64(0), "working_rgb", pixelID, pixelRef(pixelID).SHA256, int64(1024), "photos", "pixels", "v2", "application/octet-stream"}}}
	db := &downloadLoaderDB{contextRows: contextRows, inputRows: inputRows}
	return DownloadExportLoader{DB: db, Objects: downloadObjects{map[string][]byte{"master": raw}}, Bucket: "photos",
		WorkICCSHA256: workSHA, TargetICCs: map[string]string{"srgb_v1": targetSHA}}, tenant, run, db
}

func pixelRef(id uuid.UUID) review.ArtifactRef {
	return review.ArtifactRef{ID: id, SHA256: downloadDigest(id.String())}
}

func TestDownloadExportLoaderBindsCanonicalNestedOutputs(t *testing.T) {
	loader, tenant, run, db := downloadLoaderFixture(t)
	batch, err := loader.Load(context.Background(), tenant, run)
	if err != nil || len(batch.Spec.Items) != 1 || len(batch.Spec.Items[0].Image.Pixels.SHA256) != 64 {
		t.Fatal("valid download export context rejected", batch, err)
	}
	if !db.contextRows.closed || !db.inputRows.closed {
		t.Fatal("SQL cursors remained open over object IO")
	}
}

func TestDownloadExportLoaderRejectsManifestRefNotAttestedByProducer(t *testing.T) {
	loader, tenant, run, db := downloadLoaderFixture(t)
	db.inputRows.values[0][2] = uuid.New()
	if _, err := loader.Load(context.Background(), tenant, run); err == nil {
		t.Fatal("rebound pixel artifact accepted")
	}
}
