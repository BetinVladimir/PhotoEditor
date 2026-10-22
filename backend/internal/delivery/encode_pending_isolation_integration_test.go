package delivery

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Checks the installed queue predicates; full candidate behavior is exercised
// later with succeeded Delivery child runs in the module lifecycle suite.
func TestDeliverySpecialAdoptionsStayOutOfGenericPendingQueue(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated PostgreSQL required")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, signature := range []string{"photo.v2_pending_adoptions()", "photo.v30_delivery_pending_encodes(integer)", "photo.next_v30_metadata_adoption(uuid)"} {
		var definition string
		var scheduler, api, worker bool
		err = db.QueryRow(ctx, `SELECT pg_get_functiondef($1::regprocedure),
    has_function_privilege('photo_scheduler',$1,'EXECUTE'),
    has_function_privilege('photo_api',$1,'EXECUTE'),
    has_function_privilege('photo_worker',$1,'EXECUTE')`, signature).
			Scan(&definition, &scheduler, &api, &worker)
		if err != nil || !scheduler || api || worker {
			t.Fatalf("pending queue ACL drift for %s: %v", signature, err)
		}
		if signature == "photo.v2_pending_adoptions()" {
			if !strings.Contains(definition, "photo.v2_pending_adoptions_pre_relight_review_recovery()") {
				t.Fatal("generic recovery wrapper lost prior queue")
			}
			var prior string
			if err = db.QueryRow(ctx, `SELECT pg_get_functiondef('photo.v2_pending_adoptions_pre_relight_review_recovery()'::regprocedure)`).Scan(&prior); err != nil {
				t.Fatal(err)
			}
			definition += prior
			for _, required := range []string{"v29_perspective_replacement_detection_jcs", "v30_delivery_export", "export.encode", "metadata.write"} {
				if !strings.Contains(definition, required) {
					t.Fatalf("generic queue lacks %s exclusion", required)
				}
			}
			if !strings.Contains(definition, "NOT (r.config_snapshot ? 'v30_delivery_export'") {
				t.Fatal("generic queue does not exclude dedicated Delivery adoptions")
			}
		} else if signature == "photo.v30_delivery_pending_encodes(integer)" {
			for _, required := range []string{"v30_delivery_export", "export.encode", "operation_version", "renderer_version"} {
				if !strings.Contains(definition, required) {
					t.Fatalf("dedicated queue lacks %s gate", required)
				}
			}
		} else {
			for _, required := range []string{"metadata.write", "v30_metadata_result_ready", "v2-adopt"} {
				if !strings.Contains(definition, required) {
					t.Fatalf("metadata queue lacks %s gate", required)
				}
			}
		}
	}
}
