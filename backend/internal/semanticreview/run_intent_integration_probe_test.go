package semanticreview

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
)

// Called after the real adopted assembly plan has initialized review and
// committed its independently generated resource identifier.
func assertInitializedRunIntent(t *testing.T, ctx context.Context, owner, api *pgxpool.Pool, who auth.Identity, tenant, run, workflow, intent uuid.UUID, revision string) {
	t.Helper()
	if intent == workflow {
		t.Fatal("fixture failed to exercise independent intent identity")
	}
	result, err := ReadRunIntent(ctx, api, who, tenant, run)
	if err != nil || result.RunID != run || result.WorkflowID != workflow || result.IntentID == nil || *result.IntentID != intent || result.Revision != revision || result.State != "waiting_review" {
		t.Fatal("initialized parent resolver", result, err)
	}
	assertRunIntentProjectACL(t, ctx, owner, api, who, tenant, run, workflow)
	for _, input := range []struct {
		who         auth.Identity
		tenant, run uuid.UUID
	}{
		{who, uuid.New(), run}, {who, tenant, uuid.New()},
		{auth.Identity{UserID: who.UserID, SessionID: uuid.New()}, tenant, run},
	} {
		denied, err := ReadRunIntent(ctx, api, input.who, input.tenant, input.run)
		if err == nil || denied != (RunIntent{}) {
			t.Fatal("resolver exposed foreign or unauthorized resource", denied, err)
		}
	}
	var revoked *time.Time
	if err := owner.QueryRow(ctx, "SELECT revoked_at FROM photo.sessions WHERE id=$1", who.SessionID).Scan(&revoked); err != nil || revoked != nil {
		t.Fatal("resolver session fixture unavailable", err)
	}
	if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=now() WHERE id=$1", who.SessionID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := owner.Exec(context.Background(), "UPDATE photo.sessions SET revoked_at=NULL WHERE id=$1", who.SessionID); err != nil {
			t.Error("restore resolver fixture session", err)
		}
	}()
	denied, err := ReadRunIntent(ctx, api, who, tenant, run)
	if err == nil || denied != (RunIntent{}) {
		t.Fatal("revoked session exposed initialized intent", denied, err)
	}
	t.Log("V27_INITIALIZED_RUN_INTENT_AUTHORITY_OK")
}

func assertRunIntentProjectACL(t *testing.T, ctx context.Context, owner, api *pgxpool.Pool, who auth.Identity, tenant, run, workflow uuid.UUID) {
	t.Helper()
	var project uuid.UUID
	var visibility, role string
	var archived *time.Time
	var grants int
	if err := owner.QueryRow(ctx, `SELECT w.project_id,p.visibility,p.archived_at,m.role,(SELECT count(*) FROM photo.project_acl a WHERE a.tenant_id=w.tenant_id AND a.project_id=w.project_id AND a.user_id=$3) FROM photo.v2_workflows w JOIN photo.projects p ON p.tenant_id=w.tenant_id AND p.id=w.project_id JOIN photo.memberships m ON m.tenant_id=w.tenant_id AND m.user_id=$3 WHERE w.tenant_id=$1 AND w.id=$2`, tenant, workflow, who.UserID).Scan(&project, &visibility, &archived, &role, &grants); err != nil || archived != nil || grants != 0 {
		t.Fatal("project ACL fixture not isolated", err)
	}
	// Keep the fixture's last owner intact; exercise ACL as a separate editor.
	who = auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	defer func() {
		for _, step := range []struct {
			sql  string
			args []any
		}{
			{"DELETE FROM photo.project_acl WHERE tenant_id=$1 AND project_id=$2 AND user_id=$3", []any{tenant, project, who.UserID}},
			{"UPDATE photo.projects SET visibility=$3,archived_at=NULL WHERE tenant_id=$1 AND id=$2", []any{tenant, project, visibility}},
			{"DELETE FROM photo.sessions WHERE id=$1", []any{who.SessionID}},
			{"DELETE FROM photo.memberships WHERE tenant_id=$1 AND user_id=$2", []any{tenant, who.UserID}},
			{"DELETE FROM photo.users WHERE id=$1", []any{who.UserID}},
		} {
			if _, err := owner.Exec(context.Background(), step.sql, step.args...); err != nil {
				t.Error("restore isolated ACL fixture", err)
			}
		}
	}()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := owner.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("INSERT INTO photo.users(id,email_normalized) VALUES($1,$2)", who.UserID, who.UserID.String()+"@resolver.test")
	exec("INSERT INTO photo.memberships(tenant_id,user_id,role) VALUES($1,$2,'editor')", tenant, who.UserID)
	exec("INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,$4,'native',now()+interval '1 hour')", who.SessionID, who.UserID, []byte(who.SessionID.String()), uuid.New())
	exec("UPDATE photo.projects SET visibility='restricted' WHERE tenant_id=$1 AND id=$2", tenant, project)
	denied := func() {
		t.Helper()
		result, err := ReadRunIntent(ctx, api, who, tenant, run)
		if err == nil || result != (RunIntent{}) {
			t.Fatal("project authority leaked intent", result, err)
		}
	}
	denied()
	exec("INSERT INTO photo.project_acl(tenant_id,project_id,user_id,role) VALUES($1,$2,$3,'viewer')", tenant, project, who.UserID)
	denied()
	exec("UPDATE photo.project_acl SET role='editor' WHERE tenant_id=$1 AND project_id=$2 AND user_id=$3", tenant, project, who.UserID)
	if result, err := ReadRunIntent(ctx, api, who, tenant, run); err != nil || result.IntentID == nil {
		t.Fatal("editor project grant refused", err)
	}
	exec("UPDATE photo.projects SET archived_at=now() WHERE tenant_id=$1 AND id=$2", tenant, project)
	denied()
	t.Log("V27_RUN_INTENT_RESTRICTED_ACL_ARCHIVE_OK")
}
