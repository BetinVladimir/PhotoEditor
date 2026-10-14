package interchange

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
)

func TestIntegrationArchiveBeginOperatorReconciliation(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL with migration 01962 required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	signature := "photo.reconcile_v30_archive_begin_generation(uuid,uuid,uuid,uuid,uuid,text,text,boolean,text)"
	for _, role := range []string{"photo_api", "photo_worker", "photo_scheduler", "photo_gateway", "photo_portal", "photo_maintenance", "photo_publisher"} {
		var allowed bool
		if err := db.QueryRow(ctx, `SELECT has_function_privilege($1,$2,'EXECUTE')`, role, signature).Scan(&allowed); err != nil || allowed {
			t.Fatal("runtime role can settle archive Begin", role, err)
		}
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	var actor, tenantID, project, session uuid.UUID
	var epoch int64
	if err = tx.QueryRow(ctx, `SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)`, uuid.NewString()+"@archive-reconcile.test").Scan(&actor, &epoch); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `SELECT id FROM photo.tenants WHERE owner_user_id=$1`, actor).Scan(&tenantID); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `INSERT INTO photo.projects(tenant_id,name) VALUES($1,'Archive reconcile') RETURNING id`, tenantID).Scan(&project); err != nil {
		t.Fatal(err)
	}
	session = uuid.New()
	if _, err = tx.Exec(ctx, `INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at)
		VALUES($1,$2,$3,$4,'native',now()+interval '1 hour')`, session, actor, auth.HashSecret(uuid.NewString()), uuid.New()); err != nil {
		t.Fatal(err)
	}
	who := auth.Identity{UserID: actor, SessionID: session, Epoch: epoch}
	intent, err := FreezeArchiveUpload(ctx, tenantID, who, "operator-begin", ArchiveUploadRequest{
		ProjectID: project, Filename: "operator.zip", Bytes: 22, SHA256: rawSHA([]byte("archive")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `SELECT set_config('app.tenant_id',$1,true)`, tenantID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `SET LOCAL ROLE photo_api`); err != nil {
		t.Fatal(err)
	}
	var outcome string
	var upload, reservation *uuid.UUID
	var state, multipart *string
	if err = tx.QueryRow(ctx, `SELECT outcome,upload_id,token,state,multipart_id
		FROM photo.reserve_v30_archive_upload($1,$2,$3,$4,$5,'photos')`,
		tenantID, actor, session, "operator-begin", string(intent.RequestBody())).Scan(&outcome, &upload, &reservation, &state, &multipart); err != nil {
		t.Fatal(err)
	}
	if outcome != "created" || upload == nil || reservation == nil {
		t.Fatal("archive reservation not created", outcome)
	}
	var generation, writer uuid.UUID
	var objectKey string
	if err = tx.QueryRow(ctx, `SELECT generation_id,writer_token,object_key
		FROM photo.start_v30_archive_begin_generation_checked($1,$2,$3,$4,$5,$6)`,
		tenantID, actor, session, *upload, *reservation, intent.RequestSHA()).Scan(&generation, &writer, &objectKey); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `RESET ROLE`); err != nil {
		t.Fatal(err)
	}
	args := []any{tenantID, *upload, generation, writer, *reservation, "bind", "multipart-token", true, rawSHA([]byte("quiescence"))}
	var accepted bool
	wrong := append([]any(nil), args...)
	wrong[3] = uuid.New()
	if err = tx.QueryRow(ctx, `SELECT photo.reconcile_v30_archive_begin_generation($1,$2,$3,$4,$5,$6,$7,$8,$9)`, wrong...).Scan(&accepted); err != nil || accepted {
		t.Fatal("wrong writer reconciled", err)
	}
	if err = tx.QueryRow(ctx, `SELECT photo.reconcile_v30_archive_begin_generation($1,$2,$3,$4,$5,$6,$7,$8,$9)`, args...).Scan(&accepted); err != nil || !accepted {
		t.Fatal("exact writer was not reconciled", err)
	}
	var uploadState, savedKey, savedMultipart, generationState, writerState, terminalEvidence, operatorEvidence string
	if err = tx.QueryRow(ctx, `SELECT u.state,u.object_key,u.multipart_id,g.state,w.state,
		g.terminal_evidence,g.operator_reconciliation_evidence_sha
		FROM photo.v30_archive_uploads u
		JOIN photo.v30_archive_begin_generations g ON g.tenant_id=u.tenant_id AND g.upload_id=u.id
		JOIN photo.v30_archive_writers w ON w.tenant_id=g.tenant_id AND w.upload_id=g.upload_id AND w.token=g.writer_token
		WHERE u.tenant_id=$1 AND u.id=$2`, tenantID, *upload).Scan(&uploadState, &savedKey, &savedMultipart, &generationState, &writerState, &terminalEvidence, &operatorEvidence); err != nil {
		t.Fatal(err)
	}
	if uploadState != "uploading" || savedKey != objectKey || savedMultipart != "multipart-token" || generationState != "bound" || writerState != "closed" || terminalEvidence != "s3-create-receipt" || operatorEvidence != rawSHA([]byte("quiescence")) {
		t.Fatal("archive reconciliation transition incomplete", uploadState, savedKey, generationState, writerState)
	}
	legacySignature := "photo.reconcile_v30_archive_legacy_begin(uuid,uuid,uuid,uuid,text,text,boolean,text)"
	for _, role := range []string{"photo_api", "photo_worker", "photo_scheduler", "photo_gateway", "photo_portal", "photo_maintenance"} {
		var allowed bool
		if err = tx.QueryRow(ctx, `SELECT has_function_privilege($1,$2,'EXECUTE')`, role, legacySignature).Scan(&allowed); err != nil || allowed {
			t.Fatal("runtime role can settle legacy Begin", role, err)
		}
	}
	legacy, err := FreezeArchiveUpload(ctx, tenantID, who, "operator-legacy", ArchiveUploadRequest{
		ProjectID: project, Filename: "legacy.zip", Bytes: 23, SHA256: rawSHA([]byte("different-archive")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `SET LOCAL ROLE photo_api`); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `SELECT outcome,upload_id,token,state,multipart_id
		FROM photo.reserve_v30_archive_upload($1,$2,$3,$4,$5,'photos')`,
		tenantID, actor, session, "operator-legacy", string(legacy.RequestBody())).Scan(&outcome, &upload, &reservation, &state, &multipart); err != nil {
		t.Fatal(err)
	}
	if outcome != "created" || upload == nil || reservation == nil {
		t.Fatal("legacy reservation not created", outcome)
	}
	var legacyWriter *uuid.UUID
	if err = tx.QueryRow(ctx, `SELECT photo.start_v30_archive_begin($1,$2,$3,$4,$5)`,
		tenantID, actor, session, *upload, *reservation).Scan(&legacyWriter); err != nil || legacyWriter == nil {
		t.Fatal("legacy writer not opened", err)
	}
	if _, err = tx.Exec(ctx, `RESET ROLE`); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `SELECT photo.reconcile_v30_archive_legacy_begin($1,$2,$3,$4,'release',NULL,true,$5)`,
		tenantID, *upload, *legacyWriter, *reservation, rawSHA([]byte("legacy quiescent and absent"))).Scan(&accepted); err != nil || !accepted {
		t.Fatal("legacy absent writer not released", err)
	}
	if _, err = tx.Exec(ctx, `SET LOCAL ROLE photo_api`); err != nil {
		t.Fatal(err)
	}
	var recovered *string
	if err = tx.QueryRow(ctx, `SELECT photo.recover_v30_archive_begin($1,$2,$3,$4,$5)`,
		tenantID, actor, session, *upload, *reservation).Scan(&recovered); err != nil || recovered != nil {
		t.Fatal("legacy absence marker was treated as multipart ID", recovered, err)
	}
	var nextGeneration, nextWriter uuid.UUID
	var nextKey string
	if err = tx.QueryRow(ctx, `SELECT generation_id,writer_token,object_key
		FROM photo.start_v30_archive_begin_generation_checked($1,$2,$3,$4,$5,$6)`,
		tenantID, actor, session, *upload, *reservation, legacy.RequestSHA()).Scan(&nextGeneration, &nextWriter, &nextKey); err != nil {
		t.Fatal("released legacy writer still blocked new generation", err)
	}
	if nextGeneration == uuid.Nil || nextWriter == uuid.Nil || nextKey == legacy.ObjectKey() {
		t.Fatal("new generation reused legacy key")
	}
	if _, err = tx.Exec(ctx, `RESET ROLE`); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `SELECT photo.reconcile_v30_archive_begin_generation($1,$2,$3,$4,$5,'release',NULL,true,$6)`,
		tenantID, *upload, nextGeneration, nextWriter, *reservation, rawSHA([]byte("generation quiescent and absent"))).Scan(&accepted); err != nil || !accepted {
		t.Fatal("absent generation not released", err)
	}
	var releasedState string
	var purgedAt *time.Time
	if err = tx.QueryRow(ctx, `SELECT state,purged_at FROM photo.v30_archive_begin_generations
		WHERE tenant_id=$1 AND upload_id=$2 AND generation_id=$3`,
		tenantID, *upload, nextGeneration).Scan(&releasedState, &purgedAt); err != nil || releasedState != "aborted" || purgedAt == nil {
		t.Fatal("released generation still fences tenant purge", releasedState, err)
	}
}
