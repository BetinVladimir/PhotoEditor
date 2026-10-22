package run

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
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
)

// The Python harness provisions an isolated DB and actual source assets; this
// test uses the same service and photo_api pool as the running application.
func TestIntegrationBillingApplyAdmission(t *testing.T) {
	raw := os.Getenv("PHOTO_BILLING_APPLY_FIXTURE")
	if raw == "" {
		t.Skip("isolated billing Apply fixture required")
	}
	var f struct {
		Tenant, Actor, Session, Project, Collection uuid.UUID
		Epoch, Revision                             int64
	}
	if json.Unmarshal([]byte(raw), &f) != nil {
		t.Fatal("invalid billing fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	cfg, err := pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_api"
	api, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	hash, err := canonical.Hash(json.RawMessage(identityStyleJSON))
	if err != nil {
		t.Fatal(err)
	}
	var artifact, style, version uuid.UUID
	if err = admin.QueryRow(ctx, `INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,state) VALUES($1,'photo',$2,'v1',$3,$4,'application/json','style','ready') RETURNING id`, f.Tenant, uuid.NewString(), hash, len(identityStyleJSON)).Scan(&artifact); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.styles(tenant_id,project_id,name,created_by) VALUES($1,$2,'Billing identity',$3) RETURNING id`, f.Tenant, f.Project, f.Actor).Scan(&style); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.style_versions(tenant_id,style_id,version_no,config_json,config_hash,schema_version,renderer_version,approval_status,metrics,export_artifact_id) VALUES($1,$2,1,$3::jsonb,$4,'2.0.0','photo_renderer_v2','accepted','{}',$5) RETURNING id`, f.Tenant, style, string(identityStyleJSON), hash, artifact).Scan(&version); err != nil {
		t.Fatal(err)
	}
	svc := Service{DB: api}
	who := auth.Identity{UserID: f.Actor, SessionID: f.Session, Epoch: f.Epoch}
	request := ApplyRequest{SourceRevision: f.Revision, StyleVersionID: version, Scope: "all"}
	key := uuid.NewString()
	holder, err := admin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback(context.WithoutCancel(ctx))
	if _, err = holder.Exec(ctx, `SELECT 1 FROM photo.billing_resource_balances WHERE tenant_id=$1 AND resource_code='standard_photo' FOR UPDATE`, f.Tenant); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Apply(ctx, who, f.Tenant, f.Collection, key, request); domain.AsError(err).Code != "BILLING_UNAVAILABLE" {
		t.Fatal("cold billing lock must fail closed within its deadline", err)
	}
	if err = holder.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	first, err := svc.Apply(ctx, who, f.Tenant, f.Collection, key, request)
	if err != nil {
		t.Fatal("real Apply admission", err)
	}
	if first.Status != 202 || first.ResourceID == nil {
		t.Fatal("invalid admission", first)
	}
	replay, err := svc.Apply(ctx, who, f.Tenant, f.Collection, key, request)
	if err != nil || !bytes.Equal(first.Body, replay.Body) {
		t.Fatal("Apply replay changed", err)
	}
	var count int
	if err = admin.QueryRow(ctx, `SELECT count(*) FROM photo.billing_usage_requests WHERE tenant_id=$1 AND run_id=$2`, f.Tenant, *first.ResourceID).Scan(&count); err != nil || count != 1 {
		t.Fatal("missing request binding", count, err)
	}
	if err = admin.QueryRow(ctx, `SELECT count(*) FROM photo.billing_usage_tickets WHERE tenant_id=$1`, f.Tenant).Scan(&count); err != nil || count != 1 {
		t.Fatal("duplicate ticket", count, err)
	}
	_, err = svc.Apply(ctx, who, f.Tenant, f.Collection, uuid.NewString(), request)
	if domain.AsError(err).Code != "PHOTO_INITIAL_IN_PROGRESS" {
		t.Fatal("independent in-flight root admitted", err)
	}
	var status Status
	status, err = svc.Cancel(ctx, who, f.Tenant, *first.ResourceID, uuid.NewString())
	if err != nil || status.State != "cancelling" {
		t.Fatal("API cancel", status, err)
	}
	if _, err = admin.Exec(ctx, `SELECT photo.finalize_run_cancellation()`); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `SELECT count(*) FROM photo.billing_usage_tickets WHERE tenant_id=$1 AND state='released'`, f.Tenant).Scan(&count); err != nil || count != 1 {
		t.Fatal("API cancellation refund", count, err)
	}
	// A revoked session cannot replay the original 202 receipt.
	if _, err = admin.Exec(ctx, `UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.Session); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Apply(ctx, who, f.Tenant, f.Collection, key, request); err == nil {
		t.Fatal("revoked session replayed billed receipt")
	}
}
