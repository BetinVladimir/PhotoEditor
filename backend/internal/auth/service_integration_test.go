package auth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testService(t *testing.T) *Service {
	t.Helper()
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires isolated PostgreSQL fixture")
	}
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_auth"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	key := make([]byte, 32)
	rand.Read(key)
	return &Service{DB: pool, Pepper: key, MailKeys: KeyRing{Active: "test", Keys: map[string][]byte{"test": key}}, Signer: TokenSigner{Active: "test", Private: priv, Public: map[string]ed25519.PublicKey{"test": pub}, Issuer: "test", Audience: "api"}, Registration: true}
}
func requestCode(t *testing.T, s *Service, email string) (Challenge, string) {
	t.Helper()
	c, err := s.RequestOTP(context.Background(), email, "en", uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	var id uuid.UUID
	var kid string
	var ciphertext []byte
	err = s.DB.QueryRow(context.Background(), `SELECT id,key_id,payload_cipher FROM photo.mail_payloads WHERE challenge_id=$1`, c.ID).Scan(&id, &kid, &ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := s.MailKeys.Open(kid, ciphertext, []byte(id.String()+":payload"))
	if err != nil {
		t.Fatal(err)
	}
	var p map[string]string
	if err = json.Unmarshal(plain, &p); err != nil {
		t.Fatal(err)
	}
	return c, p["code"]
}
func TestIntegrationOTPAndRefresh(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	c, code := requestCode(t, s, uuid.NewString()+"@example.test")
	token, err := s.VerifyOTP(ctx, c.ID, code, "native")
	if err != nil {
		t.Fatal(err)
	}
	identity, err := s.AuthenticateAccess(ctx, token.Access)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.VerifyOTP(ctx, c.ID, code, "native"); err == nil {
		t.Fatal("OTP reused")
	}
	next, err := s.RotateRefresh(ctx, token.Refresh, "", "native")
	if err != nil {
		t.Fatal(err)
	}
	if next.Refresh == token.Refresh {
		t.Fatal("not rotated")
	}
	if _, err = s.AuthenticateAccess(ctx, next.Access); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RotateRefresh(ctx, token.Refresh, "", "native"); err == nil {
		t.Fatal("refresh replay accepted")
	}
	if _, err = s.AuthenticateAccess(ctx, next.Access); err == nil {
		t.Fatal("descendant not revoked")
	}
	var count int
	err = s.DB.QueryRow(ctx, `SELECT count(*) FROM photo.sessions WHERE user_id=$1 AND revoked_at IS NULL`, identity.UserID).Scan(&count)
	if err != nil || count != 0 {
		t.Fatal(count, err)
	}
}
func TestIntegrationOTPAttemptsPersist(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	c, code := requestCode(t, s, uuid.NewString()+"@example.test")
	wrong := "000000"
	if code == wrong {
		wrong = "000001"
	}
	for i := 0; i < 5; i++ {
		if _, err := s.VerifyOTP(ctx, c.ID, wrong, "native"); err == nil {
			t.Fatal("bad OTP accepted")
		}
	}
	if _, err := s.VerifyOTP(ctx, c.ID, code, "native"); err == nil {
		t.Fatal("attempt cap bypass")
	}
	var attempts int
	if err := s.DB.QueryRow(ctx, "SELECT attempts FROM photo.auth_challenges WHERE id=$1", c.ID).Scan(&attempts); err != nil || attempts != 5 {
		t.Fatal(attempts, err)
	}
}
func TestIntegrationCSRFAndLogout(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	c, code := requestCode(t, s, uuid.NewString()+"@example.test")
	token, err := s.VerifyOTP(ctx, c.ID, code, "web")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.RotateRefresh(ctx, token.Refresh, "invalid", "web"); err == nil {
		t.Fatal("CSRF")
	}
	next, err := s.RotateRefresh(ctx, token.Refresh, token.CSRF, "web")
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.AuthenticateAccess(ctx, next.Access)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RevokeSession(ctx, id.UserID, id.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.AuthenticateAccess(ctx, next.Access); err == nil {
		t.Fatal("logout")
	}
}
func TestIntegrationUnknownAndRate(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	s.Registration = false
	email := uuid.NewString() + "@example.test"
	ip := uuid.NewString()
	for i := 0; i < 6; i++ {
		c, err := s.RequestOTP(ctx, email, "en", ip)
		if i == 5 {
			if err == nil {
				t.Fatal("rate cap")
			}
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		var count int
		if err = s.DB.QueryRow(ctx, "SELECT count(*) FROM photo.mail_payloads WHERE challenge_id=$1", c.ID).Scan(&count); err != nil || count != 0 {
			t.Fatal("unknown email sent mail", err)
		}
	}
	var count int
	if err := s.DB.QueryRow(ctx, "SELECT count FROM photo.auth_rate_limits WHERE scope_hash=$1 AND window_start=$2", MAC(s.Pepper, "otp-rate:email", email), s.now().Truncate(time.Hour)).Scan(&count); err != nil || count != 6 {
		t.Fatal("rate rollback", count, err)
	}
}
