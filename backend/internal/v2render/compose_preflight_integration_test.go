package v2render

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestIntegrationCompositionPreflightRoleIsolation(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated database required")
	}
	layer, base, proposal := compositionInputs()
	compiled, err := CompileComposition(layer, base, nil, proposal, nil, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := canonical.Marshal(map[string]any{"backend": "cpu", "renderer_version": "v2.2", "resource_estimate": compiled.Budget.Resources})
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"photo_scheduler", "photo_api"} {
		config, err := pgxpool.ParseConfig(dsn)
		if err != nil {
			t.Fatal(err)
		}
		config.ConnConfig.RuntimeParams["role"] = role
		pool, err := pgxpool.NewWithConfig(context.Background(), config)
		if err != nil {
			t.Fatal(err)
		}
		err = (SQLOriginalCompositionPreflight{DB: pool}).Check(context.Background(), uuid.New(), uuid.New(), uuid.New(), auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, compiled.JSON, 2, 1, profile)
		pool.Close()
		want := v2input.ErrExpired
		if role == "photo_api" {
			want = v2input.ErrInvalid
		}
		if !errors.Is(err, want) {
			t.Fatalf("role %s: %v", role, err)
		}
	}
}
