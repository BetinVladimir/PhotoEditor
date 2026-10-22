package run

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/v2draft"
	"strings"
)

// The Python harness provisions an isolated DB and actual source assets; this
// test uses the same service and photo_api pool as the running application.
func TestIntegrationBillingRoutedAdmission(t *testing.T) {
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

	mode := os.Getenv("PHOTO_BILLING_ROUTE")
	var item, sourceVersion uuid.UUID
	var inputSHA string
	if err = admin.QueryRow(ctx, `SELECT ci.id,ci.asset_version_id,av.content_hash FROM photo.collection_items ci JOIN photo.asset_versions av ON av.tenant_id=ci.tenant_id AND av.id=ci.asset_version_id WHERE ci.tenant_id=$1 AND ci.collection_id=$2 LIMIT 1`, f.Tenant, f.Collection).Scan(&item, &sourceVersion, &inputSHA); err != nil {
		t.Fatal(err)
	}
	body, err := v2draft.BuildAnalysisOverride([]byte(identityStyleJSON), inputSHA, v2draft.CorrectionVector{})
	if err != nil {
		t.Fatal(err)
	}
	overrideHash, err := canonical.Hash(json.RawMessage(body))
	if err != nil {
		t.Fatal(err)
	}
	var overrideID uuid.UUID
	if err = admin.QueryRow(ctx, `INSERT INTO photo.overrides(tenant_id,asset_version_id,input_sha256,revision,config,config_hash,created_by) VALUES($1,$2,$3,1,$4::jsonb,$5,$6) RETURNING id`, f.Tenant, sourceVersion, inputSHA, string(body), overrideHash, f.Actor).Scan(&overrideID); err != nil {
		t.Fatal(err)
	}
	saveReq := SaveEditsRequest{SourceRevision: f.Revision, Scope: "selected", SelectedItemIDs: []uuid.UUID{item}, OverridesID: overrideID, StyleVersionID: &version}
	request.Scope = "selected"
	request.ItemIDs = []uuid.UUID{item}
	parentReject := false
	commits := 0
	invoke := func(ctx context.Context, who auth.Identity, tid, source uuid.UUID, key string, req ApplyRequest) (idempotency.Response, error) {
		if mode == "Run.SaveEdits" {
			return svc.SaveEdits(ctx, who, tid, source, key, saveReq)
		}
		return svc.ApplyWithCommit(ctx, who, tid, source, key, req, strings.Repeat("d", 64), []BoundItemOverride{{ItemID: item, AssetVersionID: sourceVersion, InputSHA256: inputSHA, Body: body}}, func(ctx context.Context, tx pgx.Tx, accepted ApplyAccepted) error {
			commits++
			if parentReject {
				return errors.New("parent refused")
			}
			return nil
		})
	}
	if mode == "Run.SaveEdits" {
		// Manual SaveEdits remains available while billing is unavailable.
		manual := saveReq
		manual.StyleVersionID = nil
		if _, err = admin.Exec(ctx, `UPDATE photo.billing_accounting_controls SET lifecycle='closed' WHERE tenant_id=$1`, f.Tenant); err != nil {
			t.Fatal(err)
		}
		manualKey := uuid.NewString()
		first, err := svc.SaveEdits(ctx, who, f.Tenant, f.Collection, manualKey, manual)
		if err != nil {
			t.Fatal("manual save", err)
		}
		replay, err := svc.SaveEdits(ctx, who, f.Tenant, f.Collection, manualKey, manual)
		if err != nil || !bytes.Equal(first.Body, replay.Body) {
			t.Fatal("manual replay", err)
		}
		var tickets int
		if err = admin.QueryRow(ctx, `SELECT count(*) FROM photo.billing_usage_tickets WHERE tenant_id=$1`, f.Tenant).Scan(&tickets); err != nil || tickets != 0 {
			t.Fatal("manual consumed funding", tickets, err)
		}
		if _, err = admin.Exec(ctx, `UPDATE photo.billing_accounting_controls SET lifecycle='active' WHERE tenant_id=$1`, f.Tenant); err != nil {
			t.Fatal(err)
		}
	} else {
		parentReject = true
		if _, err = invoke(ctx, who, f.Tenant, f.Collection, uuid.NewString(), request); err == nil || err.Error() != "parent refused" {
			t.Fatal("parent refusal", err)
		}
		var count int
		if err = admin.QueryRow(ctx, `SELECT count(*) FROM photo.billing_usage_requests WHERE tenant_id=$1`, f.Tenant).Scan(&count); err != nil || count != 0 {
			t.Fatal("parent refusal leaked admission", count, err)
		}
		parentReject = false
		commits = 0
	}
	key := uuid.NewString()
	holder, err := admin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback(context.WithoutCancel(ctx))
	if _, err = holder.Exec(ctx, `SELECT 1 FROM photo.billing_resource_balances WHERE tenant_id=$1 AND resource_code='standard_photo' FOR UPDATE`, f.Tenant); err != nil {
		t.Fatal(err)
	}
	if _, err = invoke(ctx, who, f.Tenant, f.Collection, key, request); domain.AsError(err).Code != "BILLING_UNAVAILABLE" {
		t.Fatal("cold billing lock must fail closed within its deadline", err)
	}
	if err = holder.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	first, err := invoke(ctx, who, f.Tenant, f.Collection, key, request)
	if err != nil {
		t.Fatal("real Apply admission", err)
	}
	if first.Status != 202 || first.ResourceID == nil {
		t.Fatal("invalid admission", first)
	}
	replay, err := invoke(ctx, who, f.Tenant, f.Collection, key, request)
	if err != nil || !bytes.Equal(first.Body, replay.Body) {
		t.Fatal("Apply replay changed", err)
	}
	// Exact receipt replays race through the real API pool and must reuse funding.
	replayErrors := make(chan error, 6)
	for i := 0; i < 6; i++ {
		go func() {
			got, e := invoke(ctx, who, f.Tenant, f.Collection, key, request)
			if e == nil && (!got.Replayed || !bytes.Equal(first.Body, got.Body)) {
				e = errors.New("concurrent replay changed receipt")
			}
			replayErrors <- e
		}()
	}
	for i := 0; i < 6; i++ {
		if e := <-replayErrors; e != nil {
			t.Fatal(e)
		}
	}
	if mode == "SessionAnalysis.Apply" && commits != 1 {
		t.Fatal("parent committed on replay", commits)
	}
	var count int
	if err = admin.QueryRow(ctx, `SELECT count(*) FROM photo.billing_usage_requests WHERE tenant_id=$1 AND run_id=$2`, f.Tenant, *first.ResourceID).Scan(&count); err != nil || count != 1 {
		t.Fatal("missing request binding", count, err)
	}
	if err = admin.QueryRow(ctx, `SELECT count(*) FROM photo.billing_usage_tickets WHERE tenant_id=$1`, f.Tenant).Scan(&count); err != nil || count != 1 {
		t.Fatal("duplicate ticket", count, err)
	}
	_, err = invoke(ctx, who, f.Tenant, f.Collection, uuid.NewString(), request)
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
	if _, err = invoke(ctx, who, f.Tenant, f.Collection, key, request); err == nil {
		t.Fatal("revoked session replayed billed receipt")
	}
}
