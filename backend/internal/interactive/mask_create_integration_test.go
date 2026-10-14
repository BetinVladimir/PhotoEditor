package interactive

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/auth"
	"testing"
	"time"
)

func TestIntegrationMaskCreateReplay(t *testing.T) {
	raw := os.Getenv("PHOTO_TEST_MASK_CREATE")
	if raw == "" {
		t.Skip("isolated fixture required")
	}
	var f struct {
		Tenant, Actor, Session, Version uuid.UUID
		SourceSHA                       string
	}
	if json.Unmarshal([]byte(raw), &f) != nil {
		t.Fatal("invalid fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cfg, err := pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_api"
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service := Service{DB: db}
	who := auth.Identity{UserID: f.Actor, SessionID: f.Session}
	req := MaskCreate{AssetVersionID: f.Version, InputSHA256: f.SourceSHA, Strokes: json.RawMessage(`[{"kind":"stroke","sequence":0,"mode":"add","points":[{"x":0.5,"y":0.5}],"radius":0.1,"hardness":1}]`)}
	key := uuid.NewString()
	first, err := service.CreateMask(ctx, who, f.Tenant, key, req)
	if err != nil || first.Status != 202 {
		t.Fatal("Mask.Create", err, first.Status)
	}
	again, err := service.CreateMask(ctx, who, f.Tenant, key, req)
	if err != nil || string(again.Body) != string(first.Body) {
		t.Fatal("Mask.Create replay", err)
	}
	bad := req
	bad.InputSHA256 = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	if _, err = service.CreateMask(ctx, who, f.Tenant, uuid.NewString(), bad); err == nil {
		t.Fatal("wrong source SHA accepted")
	}
	bad = req
	bad.Strokes = json.RawMessage(`[{"kind":"stroke","sequence":0,"mode":"add","points":[{"x":0.5,"y":0.5}],"radius":0.2,"hardness":1}]`)
	if _, err = service.CreateMask(ctx, who, f.Tenant, key, bad); err == nil {
		t.Fatal("changed body replayed")
	}
	var accepted MaskAccepted
	if json.Unmarshal(first.Body, &accepted) != nil || accepted.RunID == uuid.Nil {
		t.Fatal("invalid receipt")
	}
	t.Log("MASK_CREATE_REPLAY_OK", accepted.RunID)
}
