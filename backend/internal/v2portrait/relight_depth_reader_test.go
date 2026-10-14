package v2portrait

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"io"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"strings"
	"testing"
)

type depthReaderDB struct {
	open      bool
	lookups   int
	role      string
	ready     bool
	originSQL string
	locator   relightProducerLocator
}
type depthReaderTx struct {
	pgx.Tx
	db *depthReaderDB
}

func (d *depthReaderDB) Begin(context.Context) (pgx.Tx, error) {
	d.open = true
	return &depthReaderTx{db: d}, nil
}
func (tx *depthReaderTx) Commit(context.Context) error   { tx.db.open = false; return nil }
func (tx *depthReaderTx) Rollback(context.Context) error { tx.db.open = false; return nil }
func (tx *depthReaderTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag("SELECT 1"), nil
}

type depthReaderRow struct {
	role  string
	ready bool
	body  []byte
}

func (r depthReaderRow) Scan(dest ...any) error {
	switch value := dest[0].(type) {
	case *string:
		*value = r.role
	case *bool:
		*value = r.ready
	case *[]byte:
		*value = r.body
	}
	return nil
}
func (tx *depthReaderTx) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	if query == "SELECT current_user" {
		return depthReaderRow{role: tx.db.role}
	}
	if query == "SELECT photo.save_v28_relight_intent($1,$2,$3::jsonb)" {
		return depthReaderRow{ready: tx.db.ready}
	}
	expected := tx.db.originSQL
	if expected == "" {
		expected = "SELECT photo.v28_relight_intent_depth_origin($1,$2,$3)"
	}
	if query != expected {
		panic("unexpected SQL")
	}
	tx.db.lookups++
	body, _ := canonical.Marshal(tx.db.locator)
	return depthReaderRow{body: body}
}

type depthReaderObjects struct {
	t      *testing.T
	db     *depthReaderDB
	raw    []byte
	calls  int
	mutate func()
}

func (o *depthReaderObjects) Open(_ context.Context, obj objectstore.Object) (io.ReadCloser, error) {
	if o.db.open {
		o.t.Fatal("object IO inside SQL transaction")
	}
	o.calls++
	if obj.Key != o.db.locator.Key || obj.VersionID != o.db.locator.Version || obj.SHA256 != o.db.locator.SHA256 {
		o.t.Fatal("unpinned object")
	}
	if o.mutate != nil {
		o.mutate()
	}
	return io.NopCloser(bytes.NewReader(o.raw)), nil
}

func checkRelightDepthReader(t *testing.T, intent RelightIntent, documents RelightInputDocuments) {
	t.Helper()
	checkRelightProducerReader(t, intent, documents, false)
	checkRelightProducerReader(t, intent, documents, true)
}

func checkRelightProducerReader(t *testing.T, intent RelightIntent, documents RelightInputDocuments, person bool) {
	t.Helper()
	var params RelightParams
	if json.Unmarshal([]byte(intent.ParamsJCS), &params) != nil {
		t.Fatal("params")
	}
	original := map[string]any{"image": params.Base.Source, "model_id": "depth_anything_v2_small", "model_lock_id": strings.Repeat("a", 64), "preprocessing_id": "depth_anything_v2_small_518_v1", "input_size": 518}
	if person {
		original = map[string]any{"image": params.Base.Source, "model_id": "lraspp_mobilenet_v3_large", "model_lock_id": strings.Repeat("a", 64), "preprocessing_id": "person_guided_v1", "long_side": 768, "guided_tile": 512}
	}
	raw, _ := canonical.Marshal(original)
	sha, _ := canonical.Hash(original)
	for _, mode := range []string{"good", "corrupt", "short", "overflow", "changed", "revoked", "role", "digest"} {
		db := &depthReaderDB{role: "photo_scheduler", ready: true, locator: relightProducerLocator{ArtifactID: uuid.New(), SHA256: sha, Bucket: "photo-test", Key: "original-params", Version: "pinned-v1", Bytes: int64(len(raw)), Media: "application/json"}}
		objects := &depthReaderObjects{t: t, db: db, raw: append([]byte(nil), raw...)}
		switch mode {
		case "corrupt":
			objects.raw[0] = '!'
		case "short":
			objects.raw = objects.raw[:len(raw)-1]
		case "overflow":
			objects.raw = append(objects.raw, ' ')
		case "changed":
			objects.mutate = func() { db.locator.Version = "pinned-v2" }
		case "revoked":
			objects.mutate = func() { db.ready = false }
		case "role":
			db.role = "photo_api"
		case "digest":
			db.locator.SHA256 = "invalid"
		}
		reader := SQLRelightDepthReader{DB: db, Objects: objects, Bucket: "photo-test"}
		var result RelightDepthProducer
		var err error
		if person {
			db.originSQL = "SELECT photo.v28_relight_intent_person_origin($1,$2,$3)"
			result, err = (SQLRelightPersonReader{DB: db, Objects: objects, Bucket: "photo-test"}).Load(context.Background(), uuid.New(), uuid.New(), intent, documents)
		} else {
			result, err = reader.Load(context.Background(), uuid.New(), uuid.New(), intent, documents.Source, documents.Depth)
		}
		if mode == "good" {
			if err != nil || result.Ref.ArtifactID != db.locator.ArtifactID || !bytes.Equal(result.Body, raw) || db.lookups != 2 {
				t.Fatal("depth reader", err)
			}
		} else if err == nil || result.Body != nil {
			t.Fatal("invalid origin accepted", mode)
		}
		if db.open {
			t.Fatal("transaction leaked")
		}
		if (mode == "role" || mode == "digest") && objects.calls != 0 {
			t.Fatal("invalid authority object IO")
		}
	}
}
