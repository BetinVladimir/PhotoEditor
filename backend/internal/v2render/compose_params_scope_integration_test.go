package v2render

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
	"time"
)

func TestIntegrationOriginalCompositionParamsScope(t *testing.T) {
	raw := os.Getenv("PHOTO_COMPOSITION_PARAMS_CASE")
	if raw == "" {
		t.Skip("isolated committed composition fixture required")
	}
	var request struct {
		Tenant, Project, Snapshot, Actor, Session uuid.UUID
		Artifact                                  v2input.Ref
		Params, Profile                           string
		Width, Height                             int64
		Revoked                                   bool
	}
	if err := json.Unmarshal([]byte(raw), &request); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	config, err := pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := SQLOriginalCompositionPreflight{DB: pool}
	who := auth.Identity{UserID: request.Actor, SessionID: request.Session}
	check := func(artifact v2input.Ref, params, profile string) error {
		return store.CheckParams(ctx, request.Tenant, request.Project, request.Snapshot, who, artifact, json.RawMessage(params), request.Width, request.Height, json.RawMessage(profile))
	}
	err = check(request.Artifact, request.Params, request.Profile)
	if request.Revoked {
		if !errors.Is(err, v2input.ErrExpired) {
			t.Fatal("revoked params scope", err)
		}
		return
	}
	if err != nil {
		t.Fatal("positive params artifact scope", err)
	}
	wrong := request.Artifact
	wrong.ArtifactID = uuid.New()
	if err := check(wrong, request.Params, request.Profile); !errors.Is(err, v2input.ErrExpired) {
		t.Fatal("foreign params artifact", err)
	}
	wrong = request.Artifact
	wrong.SHA256 = hex.EncodeToString(make([]byte, 32))
	if err := check(wrong, request.Params, request.Profile); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("params SHA mismatch", err)
	}
	noncanonical := request.Params + " "
	sum := sha256.Sum256([]byte(noncanonical))
	wrong = request.Artifact
	wrong.SHA256 = hex.EncodeToString(sum[:])
	if err := check(wrong, noncanonical, request.Profile); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("noncanonical params", err)
	}
	if err := check(request.Artifact, request.Params, request.Profile+" "); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("noncanonical profile", err)
	}
}
