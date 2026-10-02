package workflow

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
)

type artifactReader struct {
	*bytes.Reader
	closed *bool
}

func (r artifactReader) Close() error { *r.closed = true; return nil }

type artifactObjects struct {
	body   []byte
	opened int
	closed bool
	object objectstore.Object
}

func (o *artifactObjects) Open(_ context.Context, obj objectstore.Object) (io.ReadCloser, error) {
	o.opened++
	o.object = obj
	return artifactReader{bytes.NewReader(o.body), &o.closed}, nil
}
func artifactFixture(raw []byte) (ExpansionArtifacts, *expansionQueries, *artifactObjects, DefectExpansionLease, review.ArtifactRef) {
	hash := sha256.Sum256(raw)
	ref := review.ArtifactRef{ID: uuid.New(), SHA256: hex.EncodeToString(hash[:])}
	role, producer := "working_image", "asset.decode"
	q := &expansionQueries{row: expansionRow{values: []any{ref.ID, "test-bucket", "tenant/key", "version-1", ref.SHA256, int64(len(raw)), "application/json", &role, &producer, true}}}
	objects := &artifactObjects{body: raw}
	return ExpansionArtifacts{DB: q, Objects: objects, Bucket: "test-bucket"}, q, objects, DefectExpansionLease{ExpansionID: uuid.New(), Token: uuid.New()}, ref
}
func TestExpansionArtifactPinnedAndFullyVerified(t *testing.T) {
	a, q, objects, lease, ref := artifactFixture([]byte(`{"ok":true}`))
	raw, meta, err := a.JSON(context.Background(), lease, ref)
	if err != nil || !bytes.Equal(raw, objects.body) || !objects.closed || meta.Role != "working_image" || !meta.Canonical || objects.object.VersionID != "version-1" {
		t.Fatal("invalid verified read", err)
	}
	if len(q.args) != 4 || q.args[0] != lease.ExpansionID || q.args[1] != lease.Token || q.args[2] != ref.ID || q.args[3] != ref.SHA256 {
		t.Fatal("artifact lookup not lease-bound")
	}
}
func TestExpansionArtifactRejectsCorruptOrAmbiguousDocument(t *testing.T) {
	for _, kind := range []string{"corrupt", "duplicate-json", "oversized", "bucket", "missing"} {
		t.Run(kind, func(t *testing.T) {
			raw := []byte(`{"ok":true}`)
			if kind == "duplicate-json" {
				raw = []byte(`{"ok":true,"ok":false}`)
			}
			a, q, objects, lease, ref := artifactFixture(raw)
			switch kind {
			case "corrupt":
				objects.body = []byte(`{"ok":null}`)
			case "oversized":
				q.row.values[5] = int64(1048577)
			case "bucket":
				q.row.values[1] = "other-bucket"
			case "missing":
				q.row = expansionRow{err: pgx.ErrNoRows}
			}
			result, _, err := a.JSON(context.Background(), lease, ref)
			if err == nil || result != nil {
				t.Fatal("unverified bytes exposed")
			}
			if kind == "oversized" || kind == "bucket" || kind == "missing" {
				if objects.opened != 0 {
					t.Fatal("invalid metadata downloaded")
				}
			} else if !objects.closed {
				t.Fatal("reader leaked")
			}
			if kind == "missing" && !errors.Is(err, objectstore.ErrMissing) {
				t.Fatal(err)
			}
		})
	}
}
