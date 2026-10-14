package v2portrait

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"testing"
	"time"
)

func TestIntegrationManualDocumentsPhysical(t *testing.T) {
	raw := os.Getenv("PHOTO_TEST_MANUAL_DOCUMENTS")
	if raw == "" {
		t.Skip("isolated physical fixture required")
	}
	var f struct {
		Tenant, Project, Snapshot, Actor, Session uuid.UUID
		Binding                                   v2input.SessionVersionBinding
		Target, Guard, TargetPixel, GuardPixel    v2input.Ref
		Source, Root, Params, Failure, Vectors    string
		Objects                                   objectstore.Config
	}
	if err := json.Unmarshal([]byte(raw), &f); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cfg, err := pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store, err := objectstore.New(f.Objects)
	if err != nil {
		t.Fatal(err)
	}
	who := auth.Identity{UserID: f.Actor, SessionID: f.Session}
	read := func(identity auth.Identity, tenant uuid.UUID) (ManualPairDocuments, error) {
		return ReadManualPairDocuments(ctx, db, store, identity, tenant, f.Project, f.Snapshot, f.Binding, []byte(f.Source), f.Target, f.Guard, f.TargetPixel, f.GuardPixel, f.Objects.Bucket)
	}
	got, err := read(who, f.Tenant)
	if f.Failure != "" {
		if err == nil || len(got.RootRaw) != 0 || len(got.ParamsRaw) != 0 {
			t.Fatal("unsafe manual documents returned", f.Failure)
		}
		t.Log("MANUAL_DOCUMENTS_PHYSICAL_REFUSED", f.Failure)
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if string(got.RootRaw) != f.Root || string(got.ParamsRaw) != f.Params || string(got.VectorsRaw) != f.Vectors {
		t.Fatal("original inline documents not preserved")
	}
	if f.Vectors == "" {
		if got.VectorsContract != "Params" || got.VectorsRef != got.Origin.Params {
			t.Fatal("inline vectors pin")
		}
	} else if got.VectorsContract != "ManualMask" || got.VectorsRef == got.Origin.Params {
		t.Fatal("external vectors pin")
	}
	if _, err = read(who, uuid.New()); err == nil {
		t.Fatal("foreign tenant accepted")
	}
	who.SessionID = uuid.New()
	if _, err = read(who, f.Tenant); err == nil {
		t.Fatal("foreign session accepted")
	}
	t.Log("MANUAL_DOCUMENTS_PHYSICAL_OK")
}
