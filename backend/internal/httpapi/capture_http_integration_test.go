package httpapi

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Synthetic storage metadata; tests real HTTP/auth/API-role transactions,
// not an object store or GPU-generated report.
func TestIntegrationCaptureAlertHTTP(t *testing.T) {
	h, db, token, tenant := httpFixture(t)
	ctx := context.Background()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	scan := func(q string, dst any, args ...any) {
		t.Helper()
		if err := tx.QueryRow(ctx, q, args...).Scan(dst); err != nil {
			t.Fatal(err)
		}
	}
	must := func(q string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	var actor, project, collection, session, artifact, asset, version, reservation, device, receipt, alert uuid.UUID
	sha := strings.Repeat("b", 64)
	scan(`SELECT owner_user_id FROM photo.tenants WHERE id=$1`, &actor, tenant)
	scan(`INSERT INTO photo.projects(tenant_id,name,visibility) VALUES($1,'HTTP Capture','restricted') RETURNING id`, &project, tenant)
	scan(`INSERT INTO photo.collections(tenant_id,project_id,name) VALUES($1,$2,'Capture') RETURNING id`, &collection, tenant, project)
	scan(`INSERT INTO photo.shoot_sessions(tenant_id,project_id,collection_id,name,state,policy,consent_epoch,capture_policy) VALUES($1,$2,$3,'Capture','open','{}',1,'{}') RETURNING id`, &session, tenant, project, collection)
	scan(`INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at) VALUES($1,'test',$2,$3,2,'application/json','v1',now()) RETURNING id`, &artifact, tenant, "http-capture/"+uuid.NewString(), sha)
	scan(`INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES($1,$2,$3,'{}','verified','{}') RETURNING id`, &asset, tenant, project, artifact)
	scan(`INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state) VALUES($1,$2,$3,$4,'{}','accepted') RETURNING id`, &version, tenant, asset, artifact, sha)
	upload := uuid.New()
	scan(`INSERT INTO photo.quota_reservations(tenant_id,principal_type,principal_id,holder_type,holder_id,bytes,files,state,expires_at) VALUES($1,'user',$2,'upload',$3,2,1,'consumed',now()+interval '1 hour') RETURNING id`, &reservation, tenant, actor, upload)
	must(`INSERT INTO photo.upload_sessions(id,tenant_id,project_id,collection_id,principal_type,principal_id,expected_size,actual_size,filename,object_key,state,expires_at,verified_version_id,reservation_id) VALUES($1,$2,$3,$4,'user',$5,2,2,'capture.jpg',$6,'verified',now()+interval '1 hour',$7,$8)`, upload, tenant, project, collection, actor, "upload/"+uuid.NewString(), version, reservation)
	scan(`INSERT INTO photo.capture_devices(tenant_id,project_id,label,public_key,created_by) VALUES($1,$2,'Camera',$3,$4) RETURNING id`, &device, tenant, project, strings.Repeat("A", 43), actor)
	scan(`INSERT INTO photo.capture_receipts(tenant_id,project_id,device_id,session_id,sequence,content_sha,upload_id) VALUES($1,$2,$3,$4,0,$5,$6) RETURNING id`, &receipt, tenant, project, device, session, sha, upload)
	must(`INSERT INTO photo.capture_receipt_assets(tenant_id,project_id,receipt_id,asset_version_id) VALUES($1,$2,$3,$4)`, tenant, project, receipt, version)
	scan(`INSERT INTO photo.capture_alerts(tenant_id,project_id,session_id,asset_version_id,report_artifact_id,severity,technical_quality,quality_metric,quality_report_sha,receipt_id,qa_stale) VALUES($1,$2,$3,$4,$5,'warning',0.8,'capture.technical_quality/1',$6,$7,true) RETURNING id`, &alert, tenant, project, session, version, artifact, sha, receipt)
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	base := "/api/v1/tenants/" + tenant.String()
	list := base + "/shoot-sessions/" + session.String() + "/alerts"
	w := request(h, "GET", list, "", token, "", "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var page struct {
		Items []map[string]any `json:"items"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0]["receipt_id"] != receipt.String() || page.Items[0]["qa_stale"] != true {
		t.Fatal("alert projection", w.Body.String())
	}
	for _, key := range []string{"technical_quality", "quality_metric", "quality_report_sha"} {
		if _, ok := page.Items[0][key]; ok {
			t.Fatalf("private field exposed: %s", key)
		}
	}
	confirm := base + "/capture-receipts/" + receipt.String() + "/confirm"
	body := `{"alert_id":"` + alert.String() + `"}`
	if w = request(h, "POST", confirm, body, "", "confirm", ""); w.Code != 401 {
		t.Fatal("unauthenticated", w.Code, w.Body.String())
	}
	if w = request(h, "POST", confirm, body, token, "", ""); w.Code != 400 {
		t.Fatal("missing key", w.Code, w.Body.String())
	}
	w = request(h, "POST", confirm, body, token, "confirm", "")
	if w.Code != 200 {
		t.Fatal("confirm", w.Code, w.Body.String())
	}

	listed := request(h, "GET", list, "", token, "", "")
	if listed.Code != 200 {
		t.Fatal("confirmed alert read", listed.Code, listed.Body.String())
	}
	var confirmations struct {
		Items []struct {
			ID             uuid.UUID  `json:"id"`
			ConfirmationID *uuid.UUID `json:"confirmation_id"`
		}
	}
	if err = json.Unmarshal(listed.Body.Bytes(), &confirmations); err != nil || len(confirmations.Items) != 1 || confirmations.Items[0].ConfirmationID == nil {
		t.Fatal("server confirmation projection", listed.Body.String(), err)
	}
	first := w.Body.String()
	w = request(h, "POST", confirm, body, token, "confirm", "")
	if w.Code != 200 || w.Body.String() != first || w.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatal("confirmation replay", w.Code, w.Body.String())
	}
	w = request(h, "POST", confirm, `{"alert_id":"`+uuid.NewString()+`"}`, token, "confirm", "")
	if w.Code != 409 {
		t.Fatal("changed body", w.Code, w.Body.String())
	}
	ack := base + "/capture-alerts/" + alert.String() + "/ack"
	w = request(h, "POST", ack, `{"expected_revision":"1"}`, token, "ack", "")
	if w.Code != 200 {
		t.Fatal("acknowledge", w.Code, w.Body.String())
	}
	first = w.Body.String()
	w = request(h, "POST", ack, `{"expected_revision":"1"}`, token, "ack", "")
	if w.Code != 200 || w.Body.String() != first || w.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatal("acknowledgement replay", w.Code, w.Body.String())
	}
	w = request(h, "POST", ack, `{"expected_revision":"1"}`, token, "stale", "")
	if w.Code != 409 {
		t.Fatal("stale acknowledgement", w.Code, w.Body.String())
	}
	w = request(h, "GET", "/api/v1/tenants/"+uuid.NewString()+"/shoot-sessions/"+session.String()+"/alerts", "", token, "", "")
	if w.Code != 404 {
		t.Fatal("foreign tenant hidden", w.Code, w.Body.String())
	}
	// A newly assessed frame must be on the first page, matching Capture's
	// delta reader; the continuation must retain the older alert.
	var newerArtifact, newerAlert uuid.UUID
	if err = db.QueryRow(ctx, `INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at) VALUES($1,'test',$2,$3,2,'application/json','v1',now()) RETURNING id`, tenant, "newer/"+uuid.NewString(), sha).Scan(&newerArtifact); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(ctx, `INSERT INTO photo.capture_alerts(tenant_id,project_id,session_id,asset_version_id,report_artifact_id,severity,technical_quality,quality_metric,quality_report_sha,receipt_id,qa_stale,created_at) VALUES($1,$2,$3,$4,$5,'info',0.8,'capture.technical_quality/1',$6,$7,false,clock_timestamp()+interval '1 second') RETURNING id`, tenant, project, session, version, newerArtifact, sha, receipt).Scan(&newerAlert); err != nil {
		t.Fatal(err)
	}
	w = request(h, "GET", list+"?limit=1", "", token, "", "")
	var ordered struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
		NextCursor *string `json:"next_cursor"`
	}
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if err = json.Unmarshal(w.Body.Bytes(), &ordered); err != nil {
		t.Fatal(err)
	}
	if len(ordered.Items) != 1 || ordered.Items[0].ID != newerAlert.String() || ordered.NextCursor == nil {
		t.Fatal("newest alert not first", w.Body.String())
	}
	w = request(h, "GET", list+"?limit=1&cursor="+url.QueryEscape(*ordered.NextCursor), "", token, "", "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if err = json.Unmarshal(w.Body.Bytes(), &ordered); err != nil {
		t.Fatal(err)
	}
	if len(ordered.Items) != 1 || ordered.Items[0].ID != alert.String() || ordered.NextCursor != nil {
		t.Fatal("older continuation", w.Body.String())
	}

	// The second real native principal belongs to the tenant as an editor but
	// initially has no access to its restricted project.
	viewerHandler, _, viewerToken, viewerTenant := httpFixture(t)
	var viewer uuid.UUID
	if err = db.QueryRow(ctx, `SELECT owner_user_id FROM photo.tenants WHERE id=$1`, viewerTenant).Scan(&viewer); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `INSERT INTO photo.memberships(tenant_id,user_id,role,status) VALUES($1,$2,'editor','active')`, tenant, viewer); err != nil {
		t.Fatal(err)
	}
	w = request(viewerHandler, "GET", list, "", viewerToken, "", "")
	if w.Code != 404 {
		t.Fatal("restricted project leaked without ACL", w.Code, w.Body.String())
	}
	if _, err = db.Exec(ctx, `INSERT INTO photo.project_acl(tenant_id,project_id,user_id,role) VALUES($1,$2,$3,'viewer')`, tenant, project, viewer); err != nil {
		t.Fatal(err)
	}
	w = request(viewerHandler, "GET", list+"?limit=1", "", viewerToken, "", "")
	if w.Code != 200 {
		t.Fatal("viewer ACL denied read", w.Code, w.Body.String())
	}
	if err = json.Unmarshal(w.Body.Bytes(), &ordered); err != nil {
		t.Fatal(err)
	}
	if ordered.NextCursor == nil {
		t.Fatal("viewer continuation missing")
	}
	viewerCursor := *ordered.NextCursor
	w = request(viewerHandler, "POST", confirm, body, viewerToken, "viewer-confirm", "")
	if w.Code != 403 {
		t.Fatal("project viewer confirmed frame", w.Code, w.Body.String())
	}
	w = request(viewerHandler, "POST", ack, `{"expected_revision":"2"}`, viewerToken, "viewer-ack", "")
	if w.Code != 403 {
		t.Fatal("project viewer acknowledged alert", w.Code, w.Body.String())
	}
	if _, err = db.Exec(ctx, `DELETE FROM photo.project_acl WHERE tenant_id=$1 AND project_id=$2 AND user_id=$3`, tenant, project, viewer); err != nil {
		t.Fatal(err)
	}
	w = request(viewerHandler, "GET", list+"?limit=1&cursor="+url.QueryEscape(viewerCursor), "", viewerToken, "", "")
	if w.Code != 404 {
		t.Fatal("cursor bypassed revoked project ACL", w.Code, w.Body.String())
	}

}
