package v2portrait

import (
	"context"
	"github.com/jackc/pgx/v5"
	"os"
	"testing"
	"time"
)

// Inspect installed SQL ACLs and wire contracts rather than matching SQL text.
// No tenant fixture or writes are needed, but migrations 00840–00844 are required.
func TestLocalPortraitPostgresWorkerGatewayACLs(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("PHOTO_TEST_DATABASE_URL and migrations 00840–00844 required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal("cannot connect to integration database")
	}
	defer conn.Close(ctx)
	checks := []struct {
		role, signature string
		allowed         bool
	}{
		{"photo_scheduler", "photo.read_v28_local_recovery_owner(uuid,uuid,uuid,uuid)", true},
		{"photo_api", "photo.read_v28_local_recovery_owner(uuid,uuid,uuid,uuid)", false},
		{"photo_worker", "photo.read_v28_local_recovery_owner(uuid,uuid,uuid,uuid)", false},
		{"photo_gateway", "photo.read_v28_local_recovery_owner(uuid,uuid,uuid,uuid)", false},
		{"photo_api", "photo.v2_draft_artifact_scope_pre_v28_manual(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text)", false},
		{"photo_api", "photo.v2_draft_artifact_scope_pre_v28(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text)", false},
		{"photo_api", "photo.v2_draft_artifact_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text)", true},
		{"photo_scheduler", "photo.v2_draft_artifact_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text)", false},
		{"photo_worker", "photo.v28_local_portrait_lease_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)", true},
		{"photo_worker", "photo.v28_local_portrait_lease_ready_pre_qualification(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)", false},
		{"photo_worker", "photo.v28_local_qualification_pin_ready(uuid,uuid,uuid)", false},
		{"photo_worker", "photo.commit_task_success(uuid,integer,bigint,uuid,text)", true},
		{"photo_worker", "photo.commit_task_success_pre_v28(uuid,integer,bigint,uuid,text)", false},
		{"photo_gateway", "photo.authorize_gateway_task(uuid,integer,bigint,uuid,text)", true},
		{"photo_gateway", "photo.authorize_gateway_task_pre_v28(uuid,integer,bigint,uuid,text)", false},
		{"photo_scheduler", "photo.register_v2_task_input_batch_pre_v28(uuid,uuid,uuid,integer,jsonb)", false},
		{"photo_scheduler", "photo.seal_v2_task_inputs_pre_v28(uuid,uuid,uuid,jsonb,jsonb,integer,text,jsonb)", false},
		{"photo_scheduler", "photo.seal_v2_graph_pre_v28(uuid,uuid)", false},
	}
	for _, check := range checks {
		var allowed bool
		if err = conn.QueryRow(ctx, "SELECT has_function_privilege($1,$2,'EXECUTE')", check.role, check.signature).Scan(&allowed); err != nil {
			t.Fatal(err)
		}
		if allowed != check.allowed {
			t.Errorf("unexpected EXECUTE for %s on %s", check.role, check.signature)
		}
	}
	for _, pair := range [][2]string{
		{"photo.authorize_gateway_task(uuid,integer,bigint,uuid,text)", "photo.authorize_gateway_task_pre_v28(uuid,integer,bigint,uuid,text)"},
		{"photo.commit_task_success(uuid,integer,bigint,uuid,text)", "photo.commit_task_success_pre_v28(uuid,integer,bigint,uuid,text)"},
	} {
		var same bool
		if err = conn.QueryRow(ctx, "SELECT pg_get_function_result($1::regprocedure)=pg_get_function_result($2::regprocedure)", pair[0], pair[1]).Scan(&same); err != nil || !same {
			t.Fatal("wire return type changed", err)
		}
	}
}
