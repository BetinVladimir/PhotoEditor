package auth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type recentEntropyFailure struct{}

func (recentEntropyFailure) Read([]byte) (int, error) {
	return 0, errors.New("fixture entropy unavailable")
}
func recentAdmin(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires isolated PostgreSQL fixture")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Skip("isolated admin unavailable:", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
func recentIdentity(t *testing.T, admin *pgxpool.Pool) Identity {
	t.Helper()
	ctx := context.Background()
	var who Identity
	email := uuid.NewString() + "@recent-auth.invalid"
	if err := admin.QueryRow(ctx, "INSERT INTO photo.users(email_normalized) VALUES($1) RETURNING id,auth_epoch", email).Scan(&who.UserID, &who.Epoch); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, "INSERT INTO photo.sessions(user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,gen_random_uuid(),'native',clock_timestamp()+interval '1 hour') RETURNING id", who.UserID, HashSecret(uuid.NewString())).Scan(&who.SessionID); err != nil {
		t.Fatal(err)
	}
	return who
}
func recentReceipt() OTPReceiptRequest {
	return OTPReceiptRequest{Key: uuid.NewString(), Bootstrap: base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32))}
}
func recentCode(t *testing.T, s *Service, who Identity, purpose string, r OTPReceiptRequest) (Challenge, string) {
	t.Helper()
	ctx := context.Background()
	challenge, err := s.RequestRecentAuth(ctx, who, purpose, uuid.NewString(), r)
	if err != nil {
		t.Fatal("recent challenge failed", err)
	}
	var id uuid.UUID
	var key string
	var ciphertext []byte
	if err = s.DB.QueryRow(ctx, "SELECT id,key_id,payload_cipher FROM photo.mail_payloads WHERE challenge_id=$1", challenge.ID).Scan(&id, &key, &ciphertext); err != nil {
		t.Fatal("recent challenge mail missing", err)
	}
	plain, err := s.MailKeys.Open(key, ciphertext, []byte(id.String()+":payload"))
	if err != nil {
		t.Fatal("mail decrypt failed")
	}
	var body struct{ Kind, Code, Locale, Purpose string }
	if json.Unmarshal(plain, &body) != nil || body.Kind != "recent_auth" || body.Purpose != purpose || !ValidOTP(body.Code) {
		t.Fatal("wrong sensitive mail contract")
	}
	return challenge, body.Code
}
func recentConsume(t *testing.T, admin *pgxpool.Pool, who Identity, purpose string, grant RecentAuthGrant) error {
	t.Helper()
	id, hash, err := ParseRecentAuthGrant(grant.Grant)
	if err != nil {
		return err
	}
	ctx := context.Background()
	tx, err := admin.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SET LOCAL ROLE photo_fn_owner"); err != nil {
		return err
	}
	var verified time.Time
	if err = tx.QueryRow(ctx, "SELECT photo.account_consume_recent_auth($1,$2,$3,$4,$5,$6)", who.UserID, who.SessionID, who.Epoch, purpose, id, hash).Scan(&verified); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func TestIntegrationRecentAuthPurposeRecoveryAndOneUse(t *testing.T) {
	s := testService(t)
	admin := recentAdmin(t)
	ctx := context.Background()
	who := recentIdentity(t, admin)
	request := recentReceipt()
	challenge, code := recentCode(t, s, who, "delete_profile", request)
	replay, err := s.RequestRecentAuth(ctx, who, "delete_profile", uuid.NewString(), request)
	if err != nil || replay != challenge {
		t.Fatal("challenge replay changed", err)
	}
	var count int
	if err = admin.QueryRow(ctx, "SELECT count(*) FROM photo.mail_payloads WHERE challenge_id=$1", challenge.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("duplicate mail effect")
	}
	if _, err = s.VerifyOTP(ctx, challenge.ID, code, "native"); err == nil {
		t.Fatal("purpose OTP minted login session")
	}
	receipt := recentReceipt()
	first, err := s.VerifyRecentAuth(ctx, who, "delete_profile", challenge.ID, code, receipt)
	if err != nil {
		t.Fatal("purpose verification failed", err)
	}
	if delta := time.Until(first.ExpiresAt); delta < 290*time.Second || delta > 301*time.Second {
		t.Fatal("wrong grant lifetime")
	}
	s.Random = recentEntropyFailure{}
	again, err := s.VerifyRecentAuth(ctx, who, "delete_profile", challenge.ID, code, receipt)
	if err != nil || again.Grant != first.Grant || !again.ExpiresAt.Equal(first.ExpiresAt) {
		t.Fatal("lost-response recovery changed grant", err)
	}
	if replay, err = s.RequestRecentAuth(ctx, who, "delete_profile", uuid.NewString(), request); err != nil || replay != challenge {
		t.Fatal("request recovery requires entropy", err)
	}
	s.Random = nil
	if err = admin.QueryRow(ctx, "SELECT count(*) FROM photo.sessions WHERE user_id=$1", who.UserID).Scan(&count); err != nil || count != 1 {
		t.Fatal("recent verification created another session")
	}
	var proofHash, ciphertext []byte
	if err = admin.QueryRow(ctx, "SELECT g.proof_hash,c.receipt_cipher FROM photo.account_recent_auth_challenges c JOIN photo.account_recent_auth_grants g ON g.grant_id=c.grant_id WHERE c.challenge_id=$1", challenge.ID).Scan(&proofHash, &ciphertext); err != nil {
		t.Fatal(err)
	}
	if !Equal(proofHash, HashSecret(first.Grant)) || bytes.Contains(ciphertext, []byte(first.Grant)) {
		t.Fatal("grant stored in plaintext or wrong hash")
	}
	if err = recentConsume(t, admin, who, "delete_profile", first); err != nil {
		t.Fatal("verified grant not consumable", err)
	}
	if err = recentConsume(t, admin, who, "delete_profile", first); err == nil {
		t.Fatal("grant consumed twice")
	}
	if _, err = s.VerifyRecentAuth(ctx, who, "delete_profile", challenge.ID, code, receipt); err == nil {
		t.Fatal("used grant replayed")
	}
}
func TestIntegrationRecentAuthRejectsWrongSessionPurposeCallerAndRecovery(t *testing.T) {
	s := testService(t)
	admin := recentAdmin(t)
	ctx := context.Background()
	who := recentIdentity(t, admin)
	challenge, code := recentCode(t, s, who, "delete_profile", recentReceipt())
	receipt := recentReceipt()
	first, err := s.VerifyRecentAuth(ctx, who, "delete_profile", challenge.ID, code, receipt)
	if err != nil {
		t.Fatal(err)
	}
	other := recentIdentity(t, admin)
	newSession := who
	if err = admin.QueryRow(ctx, "INSERT INTO photo.sessions(user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,gen_random_uuid(),'native',clock_timestamp()+interval '1 hour') RETURNING id", who.UserID, HashSecret(uuid.NewString())).Scan(&newSession.SessionID); err != nil {
		t.Fatal(err)
	}
	for _, wrong := range []Identity{other, newSession, {UserID: who.UserID, SessionID: who.SessionID, Epoch: who.Epoch + 1}} {
		if _, err = s.VerifyRecentAuth(ctx, wrong, "delete_profile", challenge.ID, code, receipt); err == nil {
			t.Fatal("foreign caller replayed grant")
		}
	}
	if _, err = s.VerifyRecentAuth(ctx, who, "resolve_ownership", challenge.ID, code, receipt); err == nil {
		t.Fatal("cross-purpose replay")
	}
	wrong := receipt
	wrong.Key = uuid.NewString()
	if _, err = s.VerifyRecentAuth(ctx, who, "delete_profile", challenge.ID, code, wrong); err == nil {
		t.Fatal("cross-key replay")
	}
	wrong = receipt
	wrong.Bootstrap = base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{4}, 32))
	if _, err = s.VerifyRecentAuth(ctx, who, "delete_profile", challenge.ID, code, wrong); err == nil {
		t.Fatal("cross-bootstrap replay")
	}
	if err = recentConsume(t, admin, who, "resolve_ownership", first); err == nil {
		t.Fatal("cross-purpose consume")
	}
	if _, err = s.VerifyRecentAuth(ctx, who, "delete_profile", challenge.ID, code, receipt); err != nil {
		t.Fatal("valid recovery lost after rejected callers", err)
	}
}
func TestIntegrationRecentAuthFailedAttemptsAndEntropyRollback(t *testing.T) {
	s := testService(t)
	admin := recentAdmin(t)
	ctx := context.Background()
	who := recentIdentity(t, admin)
	challenge, code := recentCode(t, s, who, "delete_profile", recentReceipt())
	receipt := recentReceipt()
	wrong := "000000"
	if code == wrong {
		wrong = "000001"
	}
	for attempt := 0; attempt < 5; attempt++ {
		if _, err := s.VerifyRecentAuth(ctx, who, "delete_profile", challenge.ID, wrong, receipt); err == nil {
			t.Fatal("incorrect OTP accepted")
		}
	}
	if _, err := s.VerifyRecentAuth(ctx, who, "delete_profile", challenge.ID, code, receipt); err == nil {
		t.Fatal("attempt budget bypassed")
	}
	second := recentIdentity(t, admin)
	fresh, freshCode := recentCode(t, s, second, "resolve_ownership", recentReceipt())
	s.Random = recentEntropyFailure{}
	if _, err := s.VerifyRecentAuth(ctx, second, "resolve_ownership", fresh.ID, freshCode, recentReceipt()); err == nil {
		t.Fatal("entropy failure ignored")
	}
	var consumed *time.Time
	if err := admin.QueryRow(ctx, "SELECT consumed_at FROM photo.auth_challenges WHERE id=$1", fresh.ID).Scan(&consumed); err != nil || consumed != nil {
		t.Fatal("failed grant consumed OTP")
	}
	s.Random = nil
	if _, err := s.VerifyRecentAuth(ctx, second, "resolve_ownership", fresh.ID, freshCode, recentReceipt()); err != nil {
		t.Fatal("OTP not usable after rollback", err)
	}
}
func TestIntegrationRecentAuthRevocationExpiryAndLegacyGrants(t *testing.T) {
	for _, mutation := range []string{"revoked", "epoch", "receipt-expired", "grant-expired"} {
		t.Run(mutation, func(t *testing.T) {
			s := testService(t)
			admin := recentAdmin(t)
			ctx := context.Background()
			who := recentIdentity(t, admin)
			challenge, code := recentCode(t, s, who, "delete_profile", recentReceipt())
			receipt := recentReceipt()
			grant, err := s.VerifyRecentAuth(ctx, who, "delete_profile", challenge.ID, code, receipt)
			if err != nil {
				t.Fatal(err)
			}
			switch mutation {
			case "revoked":
				_, err = admin.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", who.SessionID)
			case "epoch":
				_, err = admin.Exec(ctx, "UPDATE photo.users SET auth_epoch=auth_epoch+1 WHERE id=$1", who.UserID)
			case "receipt-expired":
				_, err = admin.Exec(ctx, "UPDATE photo.account_recent_auth_challenges SET receipt_expires_at=clock_timestamp()-interval '1 second' WHERE challenge_id=$1", challenge.ID)
			case "grant-expired":
				_, err = admin.Exec(ctx, "WITH moved AS (UPDATE photo.account_recent_auth_grants g SET verified_at=clock_timestamp()-interval '10 minutes',expires_at=clock_timestamp()-interval '5 minutes' FROM photo.account_recent_auth_challenges c WHERE c.grant_id=g.grant_id AND c.challenge_id=$1 RETURNING g.verified_at) UPDATE photo.account_recent_auth_challenges SET verified_at=(SELECT verified_at FROM moved) WHERE challenge_id=$1", challenge.ID)
			}
			if err != nil {
				t.Fatal("fixture mutation failed", err)
			}
			if _, err = s.VerifyRecentAuth(ctx, who, "delete_profile", challenge.ID, code, receipt); err == nil {
				t.Fatal("expired or revoked receipt replayed")
			}
			if mutation != "receipt-expired" && recentConsume(t, admin, who, "delete_profile", grant) == nil {
				t.Fatal("expired or revoked grant consumed")
			}
		})
	}
	admin := recentAdmin(t)
	ctx := context.Background()
	who := recentIdentity(t, admin)
	id := uuid.New()
	token := id.String() + "." + base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{5}, 32))
	if _, err := admin.Exec(ctx, "INSERT INTO photo.account_recent_auth_grants(grant_id,user_id,session_id,auth_epoch,purpose,proof_hash,otp_challenge_ref,expires_at) VALUES($1,$2,$3,$4,'delete_profile',$5,$6,clock_timestamp()+interval '5 minutes')", id, who.UserID, who.SessionID, who.Epoch, HashSecret(token), uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if recentConsume(t, admin, who, "delete_profile", RecentAuthGrant{Grant: token}) == nil {
		t.Fatal("legacy unverified grant accepted")
	}
}
func TestIntegrationRecentAuthConsumerUnavailableToAuthRuntime(t *testing.T) {
	s := testService(t)
	_, err := s.DB.Exec(context.Background(), "SELECT photo.account_consume_recent_auth($1,$2,0,'delete_profile',$3,$4)", uuid.New(), uuid.New(), uuid.New(), bytes.Repeat([]byte{1}, 32))
	var pg interface{ SQLState() string }
	if err == nil || !errors.As(err, &pg) || pg.SQLState() != "42501" {
		t.Fatal("auth runtime can consume internal grant")
	}
}
