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

type lensOriginalRow struct {
	ref Ref
	sha string
}

func (r lensOriginalRow) Scan(d ...any) error {
	*d[0].(*uuid.UUID) = r.ref.ArtifactID
	*d[1].(*string) = r.sha
	*d[2].(*int64) = 100
	*d[3].(*string) = "photos"
	*d[4].(*string) = "original"
	*d[5].(*string) = "v1"
	*d[6].(*string) = "image/jpeg"
	return nil
}

type lensOriginalDB struct {
	row   lensOriginalRow
	calls int
}

func (d *lensOriginalDB) QueryRow(_ context.Context, q string, args ...any) pgx.Row {
	d.calls++
	return d.row
}
func TestLensOriginalRequiresExactCanvasAndOriginalContent(t *testing.T) {
	image := review.WorkingImage{Canvas: review.Canvas{Width: 2, Height: 2, SourceSHA256: strings.Repeat("a", 64)}}
	hash, _ := canonical.Hash(image.Canvas)
	binding := SessionVersionBinding{AssetVersionID: uuid.New(), Source: Ref{uuid.New(), strings.Repeat("b", 64)}, CanvasHash: hash}
	db := &lensOriginalDB{row: lensOriginalRow{Ref{uuid.New(), strings.Repeat("a", 64)}, strings.Repeat("a", 64)}}
	source := SQLLensOriginalSource{DB: db, Bucket: "photos"}
	tenant, project, actor, session := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	result, err := source.resolve(context.Background(), tenant, project, actor, session, binding, image)
	if err != nil || result.Object.SHA256 != image.Canvas.SourceSHA256 {
		t.Fatal(result, err)
	}
	binding.CanvasHash = strings.Repeat("c", 64)
	if _, err = source.resolve(context.Background(), tenant, project, actor, session, binding, image); err == nil || db.calls != 1 {
		t.Fatal("canvas substitution accepted")
	}
	binding.CanvasHash = hash
	db.row.sha = strings.Repeat("d", 64)
	if _, err = source.resolve(context.Background(), tenant, project, actor, session, binding, image); err == nil {
		t.Fatal("original content substitution accepted")
	}
}

func TestLensOriginalLoadVerifiesSourceBytesBeforeCatalog(t *testing.T) {
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
	db := &lensOriginalDB{row: lensOriginalRow{Ref{uuid.New(), image.Canvas.SourceSHA256}, image.Canvas.SourceSHA256}}
	source := SQLLensOriginalSource{DB: db, Bucket: "photos", Objects: sessionImageBytes(body)}
	tenant, project, actor, session := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	if _, err = source.Load(context.Background(), tenant, project, actor, session, binding, obj); err != nil {
		t.Fatal(err)
	}
	tampered := append([]byte(nil), body...)
	tampered[len(tampered)-2] = '0'
	source.Objects = sessionImageBytes(tampered)
	if _, err = source.Load(context.Background(), tenant, project, actor, session, binding, obj); err == nil || db.calls != 1 {
		t.Fatal("unverified source reached catalog")
	}
	source.Objects = sessionImageBytes(body)
	obj.VersionID = "null"
	if _, err = source.Load(context.Background(), tenant, project, actor, session, binding, obj); err == nil || db.calls != 1 {
		t.Fatal("unversioned source reached catalog")
	}
}

func TestCompileLensOriginalBindingContainerAllowlist(t *testing.T) {
	binding := SessionVersionBinding{uuid.New(), Ref{uuid.New(), strings.Repeat("a", 64)}, strings.Repeat("b", 64)}
	original := LensOriginalSource{Ref: Ref{uuid.New(), strings.Repeat("c", 64)}, Object: objectstore.Object{Key: "original", VersionID: "v1", SHA256: strings.Repeat("c", 64), Bytes: 100, ContentType: "image/jpeg"}}
	for media, suffix := range map[string]string{"image/jpeg": ".jpg", "image/tiff": ".tiff", "image/x-adobe-dng": ".dng"} {
		original.Object.ContentType = media
		got, err := CompileLensOriginalBinding(binding, original)
		if err != nil || got.Suffix != suffix || got.Binding != binding || got.Original != original.Ref {
			t.Fatal(got, err)
		}
	}
	original.Object.ContentType = "application/octet-stream"
	if _, err := CompileLensOriginalBinding(binding, original); err == nil {
		t.Fatal("ambiguous container accepted")
	}
	original.Object.ContentType = "image/jpeg"
	original.Object.SHA256 = strings.Repeat("d", 64)
	if _, err := CompileLensOriginalBinding(binding, original); err == nil {
		t.Fatal("inconsistent original SHA accepted")
	}
}
