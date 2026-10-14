package v2optical

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"testing"
)

type multiArrayCatalogTx struct {
	perspectiveArrayTx
	calls   int
	ordinal int
}

func (t *multiArrayCatalogTx) QueryRow(ctx context.Context, q string, args ...any) pgx.Row {
	if q == "SELECT current_user" {
		return t.negativeLockTx.QueryRow(ctx, q, args...)
	}
	return negativeLockRow(func(dest ...any) error {
		t.calls++
		if q != "SELECT photo.v29_multi_array_source($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)" || len(args) != 12 || args[7] != t.ordinal {
			return errors.New("unexpected ordered source query")
		}
		*dest[0].(*[]byte) = append([]byte(nil), t.raw...)
		return nil
	})
}

type multiArrayCatalogDB struct{ tx *multiArrayCatalogTx }

func (d multiArrayCatalogDB) Begin(context.Context) (pgx.Tx, error) { return d.tx, nil }
func TestMultiArrayCatalogExactOrderedLocator(t *testing.T) {
	p, originals := multiOriginals(t)
	body, _, _ := p.Compile()
	specs, err := MultiSourceArrays("optical.panorama", body, originals)
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"", "ordinal", "binding", "version", "bucket"} {
		t.Run(fault, func(t *testing.T) {
			tenant, project, snapshot := uuid.New(), uuid.New(), uuid.New()
			tx := &multiArrayCatalogTx{perspectiveArrayTx: perspectiveArrayTx{negativeLockTx: negativeLockTx{tenant: tenant}}, ordinal: 1}
			spec := specs[1]
			locator := map[string]any{"artifact_id": spec.Array.Ref.ArtifactID, "sha256": spec.Array.Ref.SHA256, "bucket": "photos", "object_key": "pixels.npy", "object_version": "v1", "bytes": 128, "media_type": "application/x-npy"}
			switch fault {
			case "ordinal":
				spec.Ordinal = 0
			case "binding":
				spec.Binding.AssetVersionID = uuid.New()
			case "version":
				locator["object_version"] = "null"
			case "bucket":
				locator["bucket"] = "foreign"
			}
			tx.raw, err = canonical.Marshal(locator)
			if err != nil {
				t.Fatal(err)
			}
			catalog := SQLMultiSourceArrayCatalog{DB: multiArrayCatalogDB{tx}, Bucket: "photos"}
			object, err := catalog.ResolveMultiSourceArray(context.Background(), auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, tenant, project, snapshot, "optical.panorama", body, originals[1], spec)
			if fault == "" {
				if err != nil || !tx.committed || object.VersionID != "v1" || tx.calls != 1 {
					t.Fatalf("%v calls=%d", err, tx.calls)
				}
			} else {
				if err == nil || tx.committed {
					t.Fatal("invalid locator/scope committed")
				}
				if (fault == "ordinal" || fault == "binding") && tx.calls != 0 {
					t.Fatal("invalid occurrence reached SQL")
				}
			}
		})
	}
}
