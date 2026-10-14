package v2portrait

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"testing"
)

type relightPreintentTx struct {
	pgx.Tx
	body    []byte
	role    string
	queries []string
}

func (tx *relightPreintentTx) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	tx.queries = append(tx.queries, query)
	switch query {
	case "SELECT current_user":
		return depthReaderRow{role: tx.role}
	case "SELECT photo.lock_v28_relight_source($1,$2,$3,$4,$5,$6::jsonb)":
		return depthReaderRow{ready: true}
	case "SELECT photo.v28_relight_preintent_producer($1,$2,$3,$4,$5,$6::jsonb,$7::jsonb,$8)":
		return depthReaderRow{body: tx.body}
	default:
		panic("unexpected pre-intent SQL")
	}
}
func TestRelightPreintentOriginalDiscovery(t *testing.T) {
	p, _, _, _, _ := relightQualificationFixture(t)
	selected := relightProducerLocator{ArtifactID: p.Depth.ArtifactID, SHA256: p.Depth.SHA256, Bucket: "photo", Key: "selected", Version: "v1", Bytes: 200, Media: "application/json"}
	original := selected
	original.ArtifactID = uuid.New()
	original.Key = "original"
	value := RelightPreintentProducer{Selected: selected, Root: selected, Params: original}
	for _, mode := range []string{"good", "role", "null", "bucket", "root", "alias", "size", "digest", "version"} {
		result := value
		tx := &relightPreintentTx{role: "photo_scheduler"}
		switch mode {
		case "role":
			tx.role = "photo_api"
		case "bucket":
			result.Params.Bucket = "foreign"
		case "root":
			result.Root.Version = "v2"
		case "alias":
			result.Params.ArtifactID = selected.ArtifactID
		case "size":
			result.Params.Bytes = 65537
		case "digest":
			result.Params.SHA256 = "invalid"
		case "version":
			result.Selected.Version = "null"
		}
		tx.body, _ = canonical.Marshal(result)
		if mode == "null" {
			tx.body = []byte("null")
		}
		got, err := DiscoverRelightPreintentProducer(context.Background(), tx, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, uuid.New(), uuid.New(), uuid.New(), p.Base, p.Depth, "depth", "photo")
		if mode == "good" {
			if err != nil || got != value || len(tx.queries) != 3 {
				t.Fatal(err, got)
			}
		} else if err == nil || got != (RelightPreintentProducer{}) {
			t.Fatal("invalid discovery", mode)
		}
		if mode == "role" && len(tx.queries) != 1 {
			t.Fatal("wrong role reached discovery")
		}
	}
}
