package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"reflect"
	"testing"
)

func probeNativeTargetDefectProtection(t *testing.T, ctx context.Context, p TransferDraftPreparer, who auth.Identity, original detectorDraftPlan, manifest nativeTargetPreparationManifest, ordinal int, owner *pgxpool.Pool) {
	t.Helper()
	factory := (SQLNativeParentArtifactCatalogFactory{API: p.Reader.DB, Bucket: p.Reader.Bucket}).ForReference
	upstream, err := p.restoreNativeTargetDefectPrerequisites(ctx, who, original, manifest, ordinal, factory)
	if err != nil {
		t.Fatal(err)
	}
	anchor, err := compileNativeTargetAnchor(ctx, who, original.request, original.requestKey, original.published, original.anchor.snapshotRef, original.anchor.snapshotBody, manifest, ordinal, "defects.propose")
	if err != nil {
		t.Fatal(err)
	}
	saved, err := p.Reader.readNativeTargetDefectSaved(ctx, who, anchor)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := p.Reader.restoreNativeTargetDefectPlan(ctx, who, upstream.landmarks, upstream.skin, upstream.person, anchor, p.Configuration.Color, saved)
	if err != nil {
		t.Fatal(err)
	}
	dependencies, err := p.Reader.prepareNativeTargetDefectDependencies(ctx, who, original, plan.prepared.preparation, p.Configuration.Color, upstream.transports)
	if err != nil {
		t.Fatal(err)
	}
	var packet json.RawMessage
	prepared, err := p.Reader.prepareNativeTargetDefectProtection(ctx, who, plan, p.Configuration.Color)
	if err != nil {
		t.Fatal("prepare target protection", err)
	}
	published, err := p.Reader.publishNativeTargetDefectProtection(ctx, who, plan, p.Configuration.Color, prepared, p.Documents)
	if err != nil {
		t.Fatal("publish target protection", err)
	}
	ok, err := withNativeManualParentLock(ctx, p.Scheduler, original.request.TenantID, original.request.RunID, func(work context.Context) error {
		var builtErr error
		var built = upstream.transports.person // overwritten with exact defect transport in SQL
		builtErr = database.InTransaction(work, p.Scheduler, func(tx pgx.Tx) error {
			var e error
			built, e = p.Reader.recoverNativeTargetDefectTransport(work, tx, who, original, plan)
			return e
		})
		if builtErr != nil {
			return builtErr
		}
		packet, builtErr = p.Reader.marshalNativeTargetDefectCompletedEvidence(work, who, original, plan, dependencies, p.Configuration.Color, prepared.completed, built)
		return builtErr
	})
	if err != nil || !ok {
		t.Fatal("target protection packet", err)
	}
	register := func(value publishedNativeTargetDefectProtection, body []byte) error {
		ok, err := withNativeManualParentLock(ctx, p.Scheduler, original.request.TenantID, original.request.RunID, func(work context.Context) error {
			return database.InTransaction(work, p.Scheduler, func(tx pgx.Tx) error {
				return p.Reader.registerNativeTargetDefectProtectionInTransaction(work, tx, who, plan, value, body)
			})
		})
		if err == nil && !ok {
			t.Fatal("target protection parent lock missing")
		}
		return err
	}
	count := func() [3]int {
		t.Helper()
		var n [3]int
		if err := owner.QueryRow(ctx, "SELECT (SELECT count(*) FROM photo.artifacts WHERE tenant_id=$1),(SELECT count(*) FROM photo.v2_draft_uploads WHERE tenant_id=$1),(SELECT count(*) FROM photo.semantic_native_protections WHERE tenant_id=$1)", plan.shell.Tenant).Scan(&n[0], &n[1], &n[2]); err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := count()
	for i := 0; i < 2; i++ {
		if err := register(published, packet); err != nil {
			t.Fatal("target protection replay registration", err)
		}
	}
	verified, err := p.Reader.readRegisteredNativeTargetDefectProtection(ctx, who, plan, p.Configuration.Color, published)
	if err != nil {
		t.Fatal("target protection read", err)
	}
	if verified.verified.Pixels != prepared.completed.catalog.Outputs[1] || string(verified.verified.Body) != prepared.body {
		t.Fatal("target protection content drift")
	}
	lock := func(value registeredNativeTargetDefectProtection) error {
		ok, err := withNativeManualParentLock(ctx, p.Scheduler, original.request.TenantID, original.request.RunID, func(work context.Context) error {
			return database.InTransaction(work, p.Scheduler, func(tx pgx.Tx) error {
				return p.Reader.lockNativeTargetDefectProtectionInTransaction(work, tx, who, plan, value, packet)
			})
		})
		if err == nil && !ok {
			t.Fatal("target protection lock missing")
		}
		return err
	}
	if err := lock(verified); err != nil {
		t.Fatal("target protection consumer lock", err)
	}
	if err := lock(verified); err != nil {
		t.Fatal("target protection consumer replay", err)
	}
	for _, kind := range []string{"version", "bytes", "media", "key", "pixels", "body", "document"} {
		changed := verified
		switch kind {
		case "version":
			changed.object.object.VersionID = "changed"
		case "bytes":
			changed.object.object.Bytes++
		case "media":
			changed.object.object.ContentType = "image/png"
		case "key":
			changed.object.object.Key = "changed"
		case "pixels":
			changed.verified.Pixels.ArtifactID = uuid.New()
		case "body":
			changed.verified.Body = json.RawMessage(`{}`)
		case "document":
			changed.published.preparation.document = uuid.New()
		}
		if err := lock(changed); err == nil {
			t.Fatal("substituted target protection consumer pin accepted", kind)
		}
	}
	for _, kind := range []string{"document", "root", "sha", "body", "report", "packet"} {
		changed := published
		body := append([]byte(nil), packet...)
		switch kind {
		case "document":
			changed.preparation.document = uuid.New()
		case "root":
			changed.root.ArtifactID = uuid.New()
		case "sha":
			changed.root.SHA256 = "changed"
		case "body":
			changed.preparation.body = "{}"
		case "report":
			changed.preparation.completed.evidence.Report = json.RawMessage(`{}`)
		case "packet":
			var x map[string]any
			if json.Unmarshal(body, &x) != nil {
				t.Fatal("packet decode")
			}
			x["result_id"] = uuid.New().String()
			body, err = json.Marshal(x)
			if err != nil {
				t.Fatal(err)
			}
		}
		if err := register(changed, body); err == nil {
			t.Fatal("substituted target protection registration accepted", kind)
		}
	}
	changed := prepared
	changed.document = uuid.New()
	if _, err := p.Reader.publishNativeTargetDefectProtection(ctx, who, plan, p.Configuration.Color, changed, p.Documents); err == nil {
		t.Fatal("substituted target protection publication accepted")
	}
	if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", who.SessionID); err != nil {
		t.Fatal(err)
	}
	revoked := register(published, packet)
	lockRevoked := lock(verified)
	_, readRevoked := p.Reader.readRegisteredNativeTargetDefectProtection(ctx, who, plan, p.Configuration.Color, published)
	if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=NULL WHERE id=$1", who.SessionID); err != nil {
		t.Fatal(err)
	}
	if revoked == nil || readRevoked == nil || lockRevoked == nil {
		t.Fatal("revoked target protection authority accepted")
	}
	repeated, err := p.Reader.readRegisteredNativeTargetDefectProtection(ctx, who, plan, p.Configuration.Color, published)
	if err != nil || !reflect.DeepEqual(repeated, verified) {
		t.Fatal("target protection read replay", err)
	}
	if after := count(); after != before {
		t.Fatal("target protection replay duplicated metadata", before, after)
	}
	t.Log("V27_TARGET_DEFECT_PROTECTION_PUBLICATION_REGISTRATION_REPLAY_REVOKE_OK")
}
