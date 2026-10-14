package v2input

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"os"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"strings"
	"testing"
)

type deliveryMetadataOriginalDB struct {
	row   lensOriginalRow
	calls int
}

func (d *deliveryMetadataOriginalDB) QueryRow(_ context.Context, q string, args ...any) pgx.Row {
	if !strings.Contains(q, "photo.v30_delivery_metadata_original(") {
		panic("wrong metadata authority")
	}
	if len(args) != 7 {
		panic("missing authenticated scope")
	}
	d.calls++
	return d.row
}
func TestDeliveryMetadataOriginalVerifiesSourceBeforeCatalog(t *testing.T) {
	raw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []json.RawMessage
	if err = json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	body := fixtures[0]
	image, err := review.ParseWorkingImage(body)
	if err != nil {
		t.Fatal(err)
	}
	canvas, _ := canonical.Hash(image.Canvas)
	digest := sha256.Sum256(body)
	sha := hex.EncodeToString(digest[:])
	binding := SessionVersionBinding{uuid.New(), Ref{uuid.New(), sha}, canvas}
	obj := objectstore.Object{Key: "source", VersionID: "v1", SHA256: sha, Bytes: int64(len(body)), ContentType: "application/json"}
	db := &deliveryMetadataOriginalDB{row: lensOriginalRow{Ref{uuid.New(), image.Canvas.SourceSHA256}, image.Canvas.SourceSHA256}}
	source := SQLDeliveryMetadataOriginal{DB: db, Bucket: "photos", Objects: sessionImageBytes(body)}
	tenant, project, actor, session := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	if _, err = source.Load(context.Background(), tenant, project, actor, session, binding, obj); err != nil {
		t.Fatal(err)
	}
	for _, media := range []string{"image/x-adobe-dng", "image/x-canon-cr2", "image/x-nikon-nef",
		"image/x-sony-arw", "image/x-fuji-raf", "image/x-olympus-orf", "image/x-panasonic-rw2"} {
		db.row.media = media
		if got, err := source.Load(context.Background(), tenant, project, actor, session, binding, obj); err != nil || got.Object.ContentType != media {
			t.Fatal(media, err)
		}
	}
	db.row.media = "image/raw"
	if _, err = source.Load(context.Background(), tenant, project, actor, session, binding, obj); err == nil {
		t.Fatal("unknown RAW MIME accepted")
	}
	db.row.media = ""
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	calls := db.calls
	if _, err = source.Load(cancelled, tenant, project, actor, session, binding, obj); err != context.Canceled || db.calls != calls {
		t.Fatal("cancelled lookup reached SQL", err)
	}
	originalCanvas := binding.CanvasHash
	binding.CanvasHash = strings.Repeat("e", 64)
	if _, err = source.Load(context.Background(), tenant, project, actor, session, binding, obj); err == nil || db.calls != calls {
		t.Fatal("canvas substitution reached SQL")
	}
	binding.CanvasHash = originalCanvas

	tampered := append([]byte(nil), body...)
	tampered[len(tampered)-2] = '0'
	source.Objects = sessionImageBytes(tampered)
	if _, err = source.Load(context.Background(), tenant, project, actor, session, binding, obj); err == nil || db.calls != calls {
		t.Fatal("unverified source reached catalog")
	}
	source.Objects = sessionImageBytes(body)
	obj.VersionID = "null"
	if _, err = source.Load(context.Background(), tenant, project, actor, session, binding, obj); err == nil || db.calls != calls {
		t.Fatal("unversioned source reached catalog")
	}
}
