package tenant

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"sync"
	"testing"
	"time"
)

func tenantFixture(t *testing.T) (*Service, *pgxpool.Pool, auth.Identity, uuid.UUID, uuid.UUID) {
	t.Helper()
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated DB required")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	cfg, _ := pgxpool.ParseConfig(dsn)
	cfg.ConnConfig.RuntimeParams["role"] = "photo_api"
	api, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	var uid uuid.UUID
	var epoch int64
	err = admin.QueryRow(ctx, "SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)", uuid.NewString()+"@test.example").Scan(&uid, &epoch)
	if err != nil {
		t.Fatal(err)
	}
	var tid uuid.UUID
	err = admin.QueryRow(ctx, "SELECT id FROM photo.tenants WHERE owner_user_id=$1", uid).Scan(&tid)
	if err != nil {
		t.Fatal(err)
	}
	sid := uuid.New()
	_, err = admin.Exec(ctx, "INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,$4,'native',$5)", sid, uid, auth.HashSecret(uuid.NewString()), uuid.New(), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	var target uuid.UUID
	err = admin.QueryRow(ctx, "INSERT INTO photo.users(email_normalized) VALUES($1) RETURNING id", uuid.NewString()+"@test.example").Scan(&target)
	if err != nil {
		t.Fatal(err)
	}
	_, err = admin.Exec(ctx, "INSERT INTO photo.memberships(tenant_id,user_id,role) VALUES($1,$2,'editor')", tid, target)
	if err != nil {
		t.Fatal(err)
	}
	return &Service{DB: api}, admin, auth.Identity{UserID: uid, SessionID: sid}, tid, target
}
func TestIntegrationOwnerTransferRace(t *testing.T) {
	s, admin, who, tid, target := tenantFixture(t)
	ctx := context.Background()
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; errs <- s.TransferOwner(ctx, who, tid, target, 1) }()
	}
	close(start)
	wg.Wait()
	close(errs)
	success := 0
	for e := range errs {
		if e == nil {
			success++
		} else if c := domain.AsError(e).Code; c != "REVISION_CONFLICT" && c != "FORBIDDEN" {
			t.Fatal(e)
		}
	}
	if success != 1 {
		t.Fatal("successes", success)
	}
	var owners int
	var actual uuid.UUID
	if err := admin.QueryRow(ctx, "SELECT owner_user_id FROM photo.tenants WHERE id=$1", tid).Scan(&actual); err != nil || actual != target {
		t.Fatal(actual, err)
	}
	if err := admin.QueryRow(ctx, "SELECT count(*) FROM photo.memberships WHERE tenant_id=$1 AND role='owner' AND status='active'", tid).Scan(&owners); err != nil || owners != 1 {
		t.Fatal(owners, err)
	}
}
func TestIntegrationOwnerProtectionAndHiddenTenant(t *testing.T) {
	s, _, who, tid, _ := tenantFixture(t)
	ctx := context.Background()
	if _, err := s.UpdateMember(ctx, who, tid, who.UserID, 1, domain.Viewer, "active"); domain.AsError(err).Code != "OWNER_PROTECTED" {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, who, uuid.New()); domain.AsError(err).HTTPStatus != 404 {
		t.Fatal(err)
	}
}
