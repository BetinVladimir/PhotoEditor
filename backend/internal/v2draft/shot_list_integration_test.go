package v2draft

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

func TestIntegrationShotListCASReplayAndRevokedConsent(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL with V24 migrations required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_api"
	api, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	var actor, tenantID, projectID, collectionID, sessionID, personID uuid.UUID
	var epoch int64
	if err = admin.QueryRow(ctx, `SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)`, uuid.NewString()+"@test.example").Scan(&actor, &epoch); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `SELECT id FROM photo.tenants WHERE owner_user_id=$1`, actor).Scan(&tenantID); err != nil {
		t.Fatal(err)
	}
	authSession := uuid.New()
	if _, err = admin.Exec(ctx, `INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,$4,'native',now()+interval '1 hour')`, authSession, actor, auth.HashSecret(uuid.NewString()), uuid.New()); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.projects(tenant_id,name) VALUES($1,'Capture') RETURNING id`, tenantID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.collections(tenant_id,project_id,name) VALUES($1,$2,'Capture') RETURNING id`, tenantID, projectID).Scan(&collectionID); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.shoot_sessions(tenant_id,project_id,collection_id,name,state,policy,consent_epoch) VALUES($1,$2,$3,'Capture','open','{}',1) RETURNING id`, tenantID, projectID, collectionID).Scan(&sessionID); err != nil {
		t.Fatal(err)
	}
	who := auth.Identity{UserID: actor, SessionID: authSession, Epoch: epoch}
	objects := approvalReadStore{bodies: map[string][]byte{}}
	service := Service{DB: api, Objects: objects, Bucket: "test"}
	initial, err := service.GetShotList(ctx, who, tenantID, sessionID)
	if err != nil || initial.Revision != "1" || initial.Document != nil || initial.Requirements == nil {
		t.Fatalf("initial=%v err=%v", initial, err)
	}
	request := ShotListReplaceRequest{ExpectedRevision: "1", Requirements: []v2input.CaptureShotRequirement{}}
	key := uuid.NewString()
	saved, err := service.ReplaceShotList(ctx, who, tenantID, sessionID, key, request)
	if err != nil {
		t.Fatal("first replacement", err)
	}
	replay, err := service.ReplaceShotList(ctx, who, tenantID, sessionID, key, request)
	if err != nil || !replay.Replayed || !bytes.Equal(saved.Body, replay.Body) {
		t.Fatal("replay", err)
	}
	var view ShotListView
	if json.Unmarshal(saved.Body, &view) != nil || view.Revision != "2" || view.Document == nil {
		t.Fatalf("saved=%s", saved.Body)
	}
	if _, err = service.ReplaceShotList(ctx, who, tenantID, sessionID, uuid.NewString(), request); err == nil {
		t.Fatal("stale session revision accepted")
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.project_persons(tenant_id,project_id,label,state,consent_epoch) VALUES($1,$2,'Person','active',1) RETURNING id`, tenantID, projectID).Scan(&personID); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `INSERT INTO photo.consent_grants(tenant_id,project_id,purpose,person_id,status,epoch,policy_version,actor_id) VALUES($1,$2,'identity_matching',$3,'granted',1,'test',$4)`, tenantID, projectID, personID, actor); err != nil {
		t.Fatal(err)
	}
	request = ShotListReplaceRequest{ExpectedRevision: "2", Requirements: []v2input.CaptureShotRequirement{{ID: uuid.New(), Label: "Portrait", PersonIDs: []uuid.UUID{personID}, MinConfirmedFrames: 1, MinQuality: .7}}}
	if _, err = service.ReplaceShotList(ctx, who, tenantID, sessionID, uuid.NewString(), request); err != nil {
		t.Fatal("replace existing list", err)
	}
	got, err := service.GetShotList(ctx, who, tenantID, sessionID)
	if err != nil || got.Revision != "3" || len(got.Requirements) != 1 {
		t.Fatalf("got=%v err=%v", got, err)
	}
	var currentDocument uuid.UUID
	if err = admin.QueryRow(ctx, `SELECT document_id FROM photo.shot_lists WHERE tenant_id=$1 AND session_id=$2`, tenantID, sessionID).Scan(&currentDocument); err != nil {
		t.Fatal(err)
	}
	if body, e := service.GetDocument(ctx, who, tenantID, currentDocument); e != nil || !json.Valid(body) {
		t.Fatal("generic ShotList read", e)
	}
	var documentCount int
	if err = admin.QueryRow(ctx, `SELECT count(*) FROM photo.v2_documents WHERE tenant_id=$1 AND document_type='ShotList'`, tenantID).Scan(&documentCount); err != nil || documentCount != 2 {
		t.Fatal("immutable history", documentCount, err)
	}
	if _, err = admin.Exec(ctx, `UPDATE photo.consent_grants SET status='revoked',epoch=epoch+1 WHERE tenant_id=$1 AND person_id=$2 AND purpose='identity_matching'`, tenantID, personID); err != nil {
		t.Fatal(err)
	}
	if _, err = service.GetDocument(ctx, who, tenantID, currentDocument); err == nil {
		t.Fatal("generic read bypassed revoked consent")
	}
	if _, err = service.GetShotList(ctx, who, tenantID, sessionID); err == nil {
		t.Fatal("revoked identity document returned")
	}
}
