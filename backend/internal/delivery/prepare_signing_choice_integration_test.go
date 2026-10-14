package delivery

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMigratedDeliveryFrozenSigningChoice(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" { t.Skip("isolated migrated PostgreSQL required") }
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil { t.Fatal(err) }
	defer db.Close()
	policy := uuid.New()
	for _, test := range []struct { name string; choice map[string]any; valid bool }{
		{"off", map[string]any{"provenance":"off"}, true},
		{"off_hidden_policy", map[string]any{"provenance":"off", "signing_policy_id":policy.String()}, false},
		{"required", map[string]any{"provenance":"required", "signing_policy_id":policy.String(), "signing_policy_sha":strings.Repeat("a",64)}, true},
		{"required_missing_sha", map[string]any{"provenance":"required", "signing_policy_id":policy.String()}, false},
		{"required_zero_policy", map[string]any{"provenance":"required", "signing_policy_id":uuid.Nil.String(), "signing_policy_sha":strings.Repeat("a",64)}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			body, err := json.Marshal(test.choice)
			if err != nil { t.Fatal(err) }
			var accepted bool
			if err := db.QueryRow(ctx, `SELECT photo.v31_delivery_frozen_signing_choice_valid($1::jsonb)`, string(body)).Scan(&accepted); err != nil || accepted != test.valid {
				t.Fatalf("accepted=%v err=%v", accepted, err)
			}
		})
	}
}
