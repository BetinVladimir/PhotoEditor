package v2optical

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"testing"
)

type perspectiveArrayDB struct{ tx *perspectiveArrayTx }

func (d perspectiveArrayDB) Begin(context.Context) (pgx.Tx, error) { return d.tx, nil }

type perspectiveArrayTx struct {
	negativeLockTx
	raw        []byte
	committed  bool
	rolledBack bool
	snapshot   uuid.UUID
}

func (t *perspectiveArrayTx) Commit(context.Context) error   { t.committed = true; return nil }
func (t *perspectiveArrayTx) Rollback(context.Context) error { t.rolledBack = true; return nil }
func (t *perspectiveArrayTx) QueryRow(ctx context.Context, q string, args ...any) pgx.Row {
	if q == "SELECT current_user" {
		return t.negativeLockTx.QueryRow(ctx, q, args...)
	}
	return negativeLockRow(func(dest ...any) error {
		if q != "SELECT photo.v29_perspective_array_source($1,$2,$3,$4,$5,$6::jsonb,$7,$8,$9,$10)" || len(args) != 10 || args[0] != t.tenant || args[2] != t.snapshot {
			return errors.New("unexpected scope/query")
		}
		*dest[0].(*[]byte) = append([]byte(nil), t.raw...)
		return nil
	})
}

func TestPerspectiveSourceArrayCatalogExactLocatorAndAuthority(t *testing.T) {
	source, err := os.ReadFile("testdata/negative-source.json")
	if err != nil {
		t.Fatal(err)
	}
	image, err := review.ParseWorkingImage(source)
	if err != nil {
		t.Fatal(err)
	}
	source, err = canonical.Marshal(image)
	if err != nil {
		t.Fatal(err)
	}
	params := perspectiveIntent()
	params.Base.Source.SHA256, err = canonical.Hash(image)
	if err != nil {
		t.Fatal(err)
	}
	params.Base.CanvasHash, err = canonical.Hash(image.Canvas)
	if err != nil {
		t.Fatal(err)
	}
	specs, err := PerspectiveSourceArrays(params, source)
	if err != nil {
		t.Fatal(err)
	}
	spec := specs[0]
	for _, fault := range []string{"", "role", "stale", "bucket", "id", "sha", "version", "size", "media", "unknown", "occurrence", "snapshot", "cancel"} {
		t.Run(fault, func(t *testing.T) {
			tenant, project, snapshot := uuid.New(), uuid.New(), uuid.New()
			tx := &perspectiveArrayTx{negativeLockTx: negativeLockTx{tenant: tenant}, snapshot: snapshot}
			locator := map[string]any{"artifact_id": spec.Ref.ArtifactID, "sha256": spec.Ref.SHA256, "bucket": "photos", "object_key": "pixels.npy", "object_version": "v1", "bytes": int64(128), "media_type": "application/x-npy"}
			requested := spec
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch fault {
			case "role":
				tx.mode = "role"
			case "bucket":
				locator["bucket"] = "foreign"
			case "id":
				locator["artifact_id"] = uuid.New()
			case "sha":
				locator["sha256"] = "invalid"
			case "version":
				locator["object_version"] = "null"
			case "size":
				locator["bytes"] = int64(9007199254740991)
			case "media":
				locator["media_type"] = "image/png"
			case "unknown":
				locator["extra"] = true
			case "occurrence":
				requested.Width++
			case "snapshot":
				snapshot = uuid.Nil
			case "cancel":
				cancel()
			}
			tx.raw, err = canonical.Marshal(locator)
			if err != nil {
				t.Fatal(err)
			}
			if fault == "stale" {
				tx.raw = nil
			}
			catalog := SQLPerspectiveSourceArrayCatalog{DB: perspectiveArrayDB{tx}, Bucket: "photos"}
			object, got := catalog.ResolvePerspectiveSourceArray(ctx, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, tenant, project, snapshot, params, source, requested)
			if fault == "" {
				if got != nil || object.VersionID != "v1" || !tx.committed || !tx.rolledBack {
					t.Fatalf("valid locator: %v", got)
				}
			} else if got == nil || tx.committed {
				t.Fatalf("accepted %s", fault)
			}
		})
	}
}
