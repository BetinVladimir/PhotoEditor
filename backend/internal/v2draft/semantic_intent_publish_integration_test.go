package v2draft

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
	"time"
)

func TestIntegrationSemanticIntentPublication(t *testing.T) {
	raw := os.Getenv("PHOTO_SEMANTIC_INTENT_PUBLISH_CASE")
	if raw == "" {
		t.Skip("isolated database/object fixture required")
	}
	var fixture struct {
		Tenant, Actor, Session, Document uuid.UUID
		Intent                           v2input.SemanticEditIntent
		Objects                          objectstore.Config
	}
	if json.Unmarshal([]byte(raw), &fixture) != nil {
		t.Fatal("invalid fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	config, err := pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal("database configuration")
	}
	config.ConnConfig.RuntimeParams["role"] = "photo_api"
	api, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("database unavailable")
	}
	defer api.Close()
	objects, err := objectstore.New(fixture.Objects)
	if err != nil {
		t.Fatal("object configuration")
	}
	publisher := Publisher{DB: api, Objects: objects, Bucket: fixture.Objects.Bucket}
	who := auth.Identity{UserID: fixture.Actor, SessionID: fixture.Session}
	body, err := canonical.Marshal(fixture.Intent)
	if err != nil {
		t.Fatal(err)
	}
	first, err := publisher.PublishSemanticIntent(ctx, who, fixture.Tenant, fixture.Intent.Reference.AssetVersionID, fixture.Document, body)
	if err != nil {
		t.Fatal(err)
	}
	second, err := publisher.PublishSemanticIntent(ctx, who, fixture.Tenant, fixture.Intent.Reference.AssetVersionID, fixture.Document, body)
	if err != nil || first != second {
		t.Fatal("publication replay", err)
	}
	fixture.Intent.Strength = .9
	changed, _ := canonical.Marshal(fixture.Intent)
	if _, err := publisher.PublishSemanticIntent(ctx, who, fixture.Tenant, fixture.Intent.Reference.AssetVersionID, fixture.Document, changed); err == nil {
		t.Fatal("changed publication accepted")
	}

	params := v2input.SemanticTransferParams{SchemaVersion: 1, Intent: first, Reference: fixture.Intent.Reference, Target: fixture.Intent.Reference, ModelLockSHA: strings.Repeat("c", 64), Calibration: fixture.Intent.ReferenceMask}
	paramsBody, err := canonical.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	paramsID := uuid.NewSHA1(fixture.Document, []byte("semantic.transfer.params"))
	paramsRef, err := publisher.PublishSemanticTransferParams(ctx, who, fixture.Tenant, params.Target.AssetVersionID, paramsID, paramsBody)
	if err != nil {
		t.Fatal("transfer publication", err)
	}
	replay, err := publisher.PublishSemanticTransferParams(ctx, who, fixture.Tenant, params.Target.AssetVersionID, paramsID, paramsBody)
	if err != nil || replay != paramsRef {
		t.Fatal("transfer replay", err)
	}
	params.ModelLockSHA = strings.Repeat("d", 64)
	changedParams, _ := canonical.Marshal(params)
	if _, err := publisher.PublishSemanticTransferParams(ctx, who, fixture.Tenant, params.Target.AssetVersionID, paramsID, changedParams); err == nil {
		t.Fatal("changed transfer publication accepted")
	}
	t.Log("V27_TRANSFER_PARAMS_PUBLICATION_OK")
	t.Log("V27_INTENT_PUBLICATION_OK")
}
