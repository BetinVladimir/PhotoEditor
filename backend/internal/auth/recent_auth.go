package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
)

func recentAuthProofDenied() error { return domain.NewError("VALIDATION_FAILED", 422) }

type recentAuthScope struct {
	Who     Identity
	Purpose string
}
type RecentAuthGrant struct {
	Grant     string    `json:"grant"`
	ExpiresAt time.Time `json:"expires_at"`
}

func recentPurpose(purpose string) bool {
	return purpose == "delete_profile" || purpose == "resolve_ownership"
}
func recentReceiptValid(r OTPReceiptRequest) bool {
	nonce, err := base64.RawURLEncoding.DecodeString(r.Bootstrap)
	return idempotency.ValidKey(r.Key) && err == nil && len(nonce) == 32 && len(r.Bootstrap) == 43 && base64.RawURLEncoding.EncodeToString(nonce) == r.Bootstrap
}
func recentAuthError(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		if pg.Code == "55P03" || pg.Code == "42883" || pg.Code == "42P01" || pg.Code == "42501" {
			return domain.NewError("SERVICE_UNAVAILABLE", 503)
		}
		switch pg.Message {
		case "BILLING_AUTHORITY_INVALID":
			return denied()
		case "LEGAL_RECENT_AUTH_INVALID":
			return recentAuthProofDenied()
		case "LEGAL_RECENT_AUTH_CONFLICT":
			return domain.NewError("IDEMPOTENCY_CONFLICT", 409)
		case "LEGAL_RECENT_AUTH_INPUT_INVALID":
			return domain.NewError("VALIDATION_FAILED", 422)
		}
	}
	return err
}
func (s *Service) lockRecentIdentity(ctx context.Context, tx pgx.Tx, scope recentAuthScope, expectedEmail string) error {
	var email string
	err := tx.QueryRow(ctx, `SELECT email_normalized FROM photo.users WHERE id=$1 AND auth_epoch=$2 AND status='active' FOR SHARE`, scope.Who.UserID, scope.Who.Epoch).Scan(&email)
	if errors.Is(err, pgx.ErrNoRows) {
		return denied()
	}
	if err != nil {
		return err
	}
	if expectedEmail != "" && email != expectedEmail {
		return denied()
	}
	var session uuid.UUID
	err = tx.QueryRow(ctx, `SELECT id FROM photo.sessions WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL AND expires_at>clock_timestamp() FOR SHARE NOWAIT`, scope.Who.SessionID, scope.Who.UserID).Scan(&session)
	if errors.Is(err, pgx.ErrNoRows) {
		return denied()
	}
	return err
}
func (s *Service) recentOTPHash(id uuid.UUID, email string, scope recentAuthScope, code string) []byte {
	return MAC(s.Pepper, "recent-auth-otp-v1", id.String(), email, scope.Who.UserID.String(), scope.Who.SessionID.String(), strconv.FormatInt(scope.Who.Epoch, 10), scope.Purpose, code)
}
func (s *Service) recentReceiptBinding(scope recentAuthScope, id uuid.UUID, code string, r OTPReceiptRequest) []byte {
	return MAC(s.Pepper, "recent-auth-verify-receipt-binding-v1", scope.Who.UserID.String(), scope.Who.SessionID.String(), strconv.FormatInt(scope.Who.Epoch, 10), scope.Purpose, id.String(), code, r.Key, r.Bootstrap)
}
func (s *Service) recentReceiptKeys() KeyRing {
	const id = "recent-auth-receipt-v1"
	return KeyRing{Active: id, Keys: map[string][]byte{id: MAC(s.Pepper, "recent-auth-verify-receipt-encryption-v1")}}
}
func (s *Service) RequestRecentAuth(ctx context.Context, who Identity, purpose, ip string, r OTPReceiptRequest) (Challenge, error) {
	if err := s.valid(); err != nil {
		return Challenge{}, err
	}
	if !recentPurpose(purpose) || !recentReceiptValid(r) {
		return Challenge{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	if who.UserID == uuid.Nil || who.SessionID == uuid.Nil || who.Epoch < 0 {
		return Challenge{}, denied()
	}
	var email, locale string
	err := s.DB.QueryRow(ctx, `SELECT u.email_normalized,u.locale FROM photo.users u JOIN photo.sessions s ON s.user_id=u.id WHERE u.id=$1 AND s.id=$2 AND u.auth_epoch=$3 AND u.status='active' AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp()`, who.UserID, who.SessionID, who.Epoch).Scan(&email, &locale)
	if errors.Is(err, pgx.ErrNoRows) {
		return Challenge{}, denied()
	}
	if err != nil {
		return Challenge{}, err
	}
	hash, err := canonical.Hash(struct{ Purpose string }{purpose})
	if err != nil {
		return Challenge{}, err
	}
	receipt := otpRequestReceipt{Scope: MAC(s.Pepper, "recent-auth-request-receipt-v1", who.UserID.String(), who.SessionID.String(), strconv.FormatInt(who.Epoch, 10), r.Bootstrap), Key: r.Key, Hash: hash}
	scope := recentAuthScope{Who: who, Purpose: purpose}
	out, err := s.requestOTPScoped(ctx, email, locale, ip, &receipt, &scope)
	return out, recentAuthError(err)
}

// ParseRecentAuthGrant hashes the opaque credential server-side; clients never supply its hash.
func ParseRecentAuthGrant(value string) (uuid.UUID, []byte, error) {
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return uuid.Nil, nil, ErrInvalid
	}
	id, err := uuid.Parse(parts[0])
	if err != nil || id == uuid.Nil || id.String() != parts[0] {
		return uuid.Nil, nil, ErrInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(raw) != 32 || len(parts[1]) != 43 || base64.RawURLEncoding.EncodeToString(raw) != parts[1] {
		return uuid.Nil, nil, ErrInvalid
	}
	return id, HashSecret(value), nil
}

type recentChallengeState struct {
	Email                                    string
	MAC                                      []byte
	Expires                                  time.Time
	Consumed                                 *time.Time
	Attempts                                 int64
	Now                                      time.Time
	GrantID                                  *uuid.UUID
	GrantExpires, GrantUsed                  *time.Time
	ProofHash, ReceiptBinding, ReceiptCipher []byte
	ReceiptExpires                           *time.Time
}

func (s *Service) VerifyRecentAuth(ctx context.Context, who Identity, purpose string, id uuid.UUID, code string, r OTPReceiptRequest) (RecentAuthGrant, error) {
	if err := s.valid(); err != nil {
		return RecentAuthGrant{}, err
	}
	if !recentPurpose(purpose) || id == uuid.Nil || !ValidOTP(code) || !recentReceiptValid(r) {
		return RecentAuthGrant{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	if who.UserID == uuid.Nil || who.SessionID == uuid.Nil || who.Epoch < 0 {
		return RecentAuthGrant{}, denied()
	}
	scope := recentAuthScope{Who: who, Purpose: purpose}
	var result RecentAuthGrant
	var outcome error
	err := database.InTransaction(ctx, s.DB, func(tx pgx.Tx) error {
		result = RecentAuthGrant{}
		outcome = recentAuthProofDenied()
		var state recentChallengeState
		err := tx.QueryRow(ctx, `SELECT * FROM photo.account_lock_recent_auth_challenge($1,$2,$3,$4,$5)`, who.UserID, who.SessionID, who.Epoch, purpose, id).Scan(&state.Email, &state.MAC, &state.Expires, &state.Consumed, &state.Attempts, &state.Now, &state.GrantID, &state.GrantExpires, &state.GrantUsed, &state.ProofHash, &state.ReceiptBinding, &state.ReceiptCipher, &state.ReceiptExpires)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if state.Attempts >= 5 {
			return nil
		}
		binding := s.recentReceiptBinding(scope, id, code, r)
		if state.Consumed != nil {
			if state.GrantID == nil || state.GrantExpires == nil || state.GrantUsed != nil || !state.Now.Before(*state.GrantExpires) || state.ReceiptExpires == nil || !state.Now.Before(*state.ReceiptExpires) {
				return nil
			}
			if !Equal(binding, state.ReceiptBinding) {
				_, err = tx.Exec(ctx, `UPDATE photo.auth_challenges SET attempts=attempts+1 WHERE id=$1`, id)
				return err
			}
			plain, err := s.recentReceiptKeys().Open("recent-auth-receipt-v1", state.ReceiptCipher, binding)
			if err != nil {
				return recentAuthProofDenied()
			}
			var body struct {
				Grant string `json:"grant"`
			}
			if json.Unmarshal(plain, &body) != nil || !Equal(HashSecret(body.Grant), state.ProofHash) {
				return recentAuthProofDenied()
			}
			recoveredID, _, parseErr := ParseRecentAuthGrant(body.Grant)
			if parseErr != nil || recoveredID != *state.GrantID {
				return recentAuthProofDenied()
			}
			result = RecentAuthGrant{Grant: body.Grant, ExpiresAt: state.GrantExpires.UTC()}
			outcome = nil
			return nil
		}
		if !state.Now.Before(state.Expires) {
			return nil
		}
		if !Equal(state.MAC, s.recentOTPHash(id, state.Email, scope, code)) {
			_, err = tx.Exec(ctx, `UPDATE photo.auth_challenges SET attempts=attempts+1 WHERE id=$1`, id)
			return err
		}
		grantID, err := uuid.NewRandomFromReader(s.random())
		if err != nil {
			return err
		}
		nonce, err := NewSecret(s.random())
		if err != nil {
			return err
		}
		token := grantID.String() + "." + nonce
		plain, err := json.Marshal(struct {
			Grant string `json:"grant"`
		}{token})
		if err != nil {
			return err
		}
		_, ciphertext, err := s.recentReceiptKeys().Seal(plain, binding)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE photo.auth_challenges SET consumed_at=clock_timestamp() WHERE id=$1 AND consumed_at IS NULL`, id); err != nil {
			return err
		}
		var expires time.Time
		err = tx.QueryRow(ctx, `SELECT photo.account_finish_recent_auth_challenge($1,$2,$3,$4,$5,$6,$7,$8,$9)`, who.UserID, who.SessionID, who.Epoch, purpose, id, grantID, HashSecret(token), binding, ciphertext).Scan(&expires)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		result = RecentAuthGrant{Grant: token, ExpiresAt: expires.UTC()}
		outcome = nil
		return nil
	})
	if err != nil {
		return RecentAuthGrant{}, recentAuthError(err)
	}
	return result, outcome
}
