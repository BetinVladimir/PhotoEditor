package auth

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/domain"
)

type Service struct {
	DB           *pgxpool.Pool
	Pepper       []byte
	MailKeys     KeyRing
	Signer       TokenSigner
	Registration bool
	Now          func() time.Time
	Random       io.Reader
}
type Challenge struct {
	ID        uuid.UUID `json:"challenge_id"`
	ExpiresIn int       `json:"expires_in"`
}
type Tokens struct {
	User           User      `json:"user"`
	Access         string    `json:"access_token"`
	Refresh        string    `json:"refresh_token,omitempty"`
	CSRF           string    `json:"csrf_token"`
	SessionID      uuid.UUID `json:"session_id"`
	ExpiresIn      int       `json:"expires_in"`
	RefreshExpires time.Time `json:"-"`
}
type Identity struct {
	UserID, SessionID uuid.UUID
	Epoch             int64
}
type SessionView struct {
	ID         uuid.UUID `json:"id"`
	ClientType string    `json:"client_type"`
	CreatedAt  time.Time `json:"created_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	Current    bool      `json:"current"`
}

func (s *Service) ListSessions(ctx context.Context, user, current uuid.UUID, after *uuid.UUID, limit int) ([]SessionView, bool, error) {
	if err := s.valid(); err != nil {
		return nil, false, err
	}
	if user == uuid.Nil || current == uuid.Nil || limit < 1 || limit > 100 {
		return nil, false, domain.NewError("VALIDATION_FAILED", 422)
	}
	rows, err := s.DB.Query(ctx, `SELECT id,client_type,created_at,expires_at FROM photo.sessions
		WHERE user_id=$1 AND revoked_at IS NULL AND expires_at>$2 AND ($3::uuid IS NULL OR id>$3)
		ORDER BY id LIMIT $4`, user, s.now(), after, limit+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	items := make([]SessionView, 0, limit)
	for rows.Next() {
		var item SessionView
		if err = rows.Scan(&item.ID, &item.ClientType, &item.CreatedAt, &item.ExpiresAt); err != nil {
			return nil, false, err
		}
		item.Current = item.ID == current
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, false, err
	}
	more := len(items) > limit
	if more {
		items = items[:limit]
	}
	return items, more, nil
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
func (s *Service) random() io.Reader {
	if s.Random != nil {
		return s.Random
	}
	return rand.Reader
}
func (s *Service) valid() error {
	if s.DB == nil || len(s.Pepper) < 32 {
		return errors.New("auth service unconfigured")
	}
	return nil
}
func denied() error          { return domain.NewError("INVALID_CREDENTIALS", 401) }
func localeOK(s string) bool { return s == "ru" || s == "bg" || s == "en" }

// RequestOTP commits rate counters even for rejected requests; returning a domain
// error inside the transaction would roll the limiter back.
func (s *Service) RequestOTP(ctx context.Context, raw, locale, ip string) (Challenge, error) {
	return s.requestOTP(ctx,raw,locale,ip,nil)
}

func (s *Service) requestOTP(ctx context.Context, raw, locale, ip string, receipt *otpRequestReceipt) (Challenge, error) {
	if err := s.valid(); err != nil {
		return Challenge{}, err
	}
	email, err := NormalizeEmail(raw)
	if err != nil || !localeOK(locale) {
		return Challenge{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	var out Challenge
	var outcome error
	err = database.InTransaction(ctx, s.DB, func(tx pgx.Tx) (txErr error) {
		outcome = nil
		if receipt!=nil {
			prior,e:=lockOTPRequestReceipt(ctx,tx,*receipt);if e!=nil{return e}
			if prior!=nil{out=*prior;return nil}
			defer func(){if txErr==nil&&outcome==nil {txErr=saveOTPRequestReceipt(ctx,tx,*receipt,out,s.now())}}()
		}
		// Recovery does not depend on fresh entropy: only a new request allocates
		// a challenge. Complete transaction retries allocate a new candidate.
		id,e := uuid.NewRandomFromReader(s.random());if e!=nil{return e}
		code,e := NewOTP(s.random());if e!=nil{return e}
		out=Challenge{id,300}
		// Serialize same email before examining cooldown; IP and email counters have a fixed lock order.
		if _, e := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "otp:"+email); e != nil {
			return e
		}
        if err:=ctx.Err();err!=nil{return err}
        now:=s.now()
        expires:=now.Add(5*time.Minute)
		limited := false
		for _, r := range []struct {
			kind, value string
			max         int
		}{{"ip", ip, 20}, {"email", email, 5}} {
			var n int
			e := tx.QueryRow(ctx, `INSERT INTO photo.auth_rate_limits(scope_hash,window_start,count,expires_at) VALUES($1,$2,1,$3) ON CONFLICT(scope_hash,window_start) DO UPDATE SET count=LEAST(photo.auth_rate_limits.count+1,1000000) RETURNING count`, MAC(s.Pepper, "otp-rate:"+r.kind, r.value), now.Truncate(time.Hour), now.Truncate(time.Hour).Add(2*time.Hour)).Scan(&n)
			if e != nil {
				return e
			}
			limited = limited || n > r.max
		}
		if limited {
			outcome = domain.NewError("RATE_LIMITED", 429)
			return nil
		}
		var last time.Time
		e = tx.QueryRow(ctx, `SELECT created_at FROM photo.auth_challenges WHERE email_normalized=$1 ORDER BY created_at DESC LIMIT 1`, email).Scan(&last)
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		// A cooldown request still returns an opaque accepted ID and cannot invalidate the previous code.
		if e == nil && now.Before(last.Add(time.Minute)) {
			return nil
		}
		_, e = tx.Exec(ctx, `INSERT INTO photo.auth_challenges(id,created_at,email_normalized,code_hmac,expires_at,locale,request_ip_hash) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, now, email, MAC(s.Pepper, "otp", id.String(), email, code), expires, locale, MAC(s.Pepper, "ip", ip))
		if e != nil {
			return e
		}
		var allowed bool
		e = tx.QueryRow(ctx, `SELECT COALESCE((SELECT status='active' FROM photo.users WHERE email_normalized=$1),$2)`, email, s.Registration).Scan(&allowed)
		if e != nil {
			return e
		}
		if !allowed {
			return nil
		}
		payloadID, e := uuid.NewRandomFromReader(s.random())
		if e != nil {
			return e
		}
		aad := []byte(payloadID.String())
		kid, recipient, e := s.MailKeys.Seal([]byte(email), append(aad, []byte(":recipient")...))
		if e != nil {
			return e
		}
		body, e := json.Marshal(map[string]string{"kind": "otp", "code": code, "locale": locale})
		if e != nil {
			return e
		}
		kid2, cipher, e := s.MailKeys.Seal(body, append([]byte(payloadID.String()), []byte(":payload")...))
		if e != nil {
			return e
		}
		if kid2 != kid {
			return errors.New("mail key changed during request")
		}
		_, e = tx.Exec(ctx, `INSERT INTO photo.mail_payloads(id,challenge_id,recipient_cipher,payload_cipher,key_id,expires_at,message_id) VALUES($1,$2,$3,$4,$5,$6,$7)`, payloadID, id, recipient, cipher, kid, expires, "<"+id.String()+"@photoeditor.local>")
		if e != nil {
			return e
		}
		_, e = tx.Exec(ctx, `INSERT INTO photo.auth_mail_outbox(event_id,payload_id) VALUES($1,$2)`, uuid.New(), payloadID)
		return e
	})
	if err != nil {
		return Challenge{}, err
	}
	return out, outcome
}

func (s *Service) newSession(ctx context.Context, tx pgx.Tx, user, family uuid.UUID, parent *uuid.UUID, client string, epoch int64, expires time.Time) (Tokens, error) {
	sid, err := uuid.NewRandomFromReader(s.random())
	if err != nil {
		return Tokens{}, err
	}
	refresh, err := NewSecret(s.random())
	if err != nil {
		return Tokens{}, err
	}
	csrf, err := NewSecret(s.random())
	if err != nil {
		return Tokens{}, err
	}
	access, err := s.Signer.Issue(user, sid, epoch)
	if err != nil {
		return Tokens{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,parent_id,client_type,expires_at,csrf_hash) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, sid, user, HashSecret(refresh), family, parent, client, expires, HashSecret(csrf))
	if err != nil {
		return Tokens{}, err
	}
	var profile User
	var revision int64
	err = tx.QueryRow(ctx, "SELECT id,email_normalized,locale,theme,timezone,revision FROM photo.users WHERE id=$1", user).Scan(&profile.ID, &profile.Email, &profile.Locale, &profile.Theme, &profile.Timezone, &revision)
	if err != nil {
		return Tokens{}, err
	}
	profile.Revision = strconv.FormatInt(revision, 10)
	return Tokens{User: profile, Access: access, Refresh: refresh, CSRF: csrf, SessionID: sid, ExpiresIn: 300, RefreshExpires: expires}, nil
}
func (s *Service) VerifyOTP(ctx context.Context, id uuid.UUID, code, client string) (Tokens, error) {
	return s.verifyOTP(ctx,id,code,client,nil)
}

func (s *Service) verifyOTP(ctx context.Context, id uuid.UUID, code, client string, receipt *OTPReceiptRequest) (Tokens, error) {
	if err := s.valid(); err != nil {
		return Tokens{}, err
	}
	if id == uuid.Nil || !ValidOTP(code) || (client != "web" && client != "native") {
		return Tokens{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	var result Tokens
	var outcome error
	err := database.InTransaction(ctx, s.DB, func(tx pgx.Tx) error {
		outcome = denied()
		result = Tokens{}
		var email, locale string
		var mac []byte
		var expires time.Time
		var consumed *time.Time
		var attempts int
		var binding,cipher []byte
		var receiptExpires *time.Time
		var receiptEpoch *int64
		err := tx.QueryRow(ctx, `SELECT email_normalized,locale,code_hmac,expires_at,consumed_at,attempts,verify_receipt_binding,verify_receipt_cipher,verify_receipt_expires_at,verify_receipt_epoch FROM photo.auth_challenges WHERE id=$1 FOR UPDATE`, id).Scan(&email, &locale, &mac, &expires, &consumed, &attempts,&binding,&cipher,&receiptExpires,&receiptEpoch)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		now := s.now()
		if err:=ctx.Err();err!=nil{return err}
		if consumed != nil && receipt != nil && receiptExpires != nil && receiptEpoch != nil && now.Before(*receiptExpires) && Equal(binding,s.otpReceiptBinding(id,code,client,*receipt)) {
			var replayErr error
			result,replayErr=s.openOTPReceipt(ctx,tx,cipher,binding,*receiptEpoch,*receiptExpires)
			if replayErr==nil{outcome=nil}
			return replayErr
		}
		if consumed != nil || !now.Before(expires) || attempts >= 5 {
			return nil
		}
		if !Equal(mac, MAC(s.Pepper, "otp", id.String(), email, code)) {
			_, err = tx.Exec(ctx, `UPDATE photo.auth_challenges SET attempts=attempts+1 WHERE id=$1`, id)
			return err
		}
		// Consume before account lookup so a disabled account's valid code is single use too.
		if _, err = tx.Exec(ctx, `UPDATE photo.auth_challenges SET consumed_at=$2 WHERE id=$1`, id, now); err != nil {
			return err
		}
		var user uuid.UUID
		var epoch int64
		err = tx.QueryRow(ctx, `SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,$2,$3)`, email, locale, s.Registration).Scan(&user, &epoch)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		result, err = s.newSession(ctx, tx, user, uuid.New(), nil, client, epoch, now.Add(30*24*time.Hour))
		if err==nil && receipt!=nil {
			binding=s.otpReceiptBinding(id,code,client,*receipt)
			cipher,err=s.sealOTPReceipt(result,binding)
			if err==nil {
                tag,saveErr:=tx.Exec(ctx,`UPDATE photo.auth_challenges SET verify_receipt_binding=$2,verify_receipt_cipher=$3,verify_receipt_expires_at=$4,verify_receipt_epoch=$5 WHERE id=$1 AND consumed_at IS NOT NULL`,id,binding,cipher,now.Add(2*time.Minute),epoch)
                err=saveErr
                if err==nil&&tag.RowsAffected()!=1{err=errors.New("OTP receipt persistence lost challenge")}
            }
		}
		if err == nil {
			outcome = nil
		}
		return err
	})
	if err != nil {
		return Tokens{}, err
	}
	return result, outcome
}
func (s *Service) RotateRefresh(ctx context.Context, raw, csrf, client string) (Tokens, error) {
	if err := s.valid(); err != nil {
		return Tokens{}, err
	}
	if len(raw) != 43 || (client != "web" && client != "native") {
		return Tokens{}, denied()
	}
	var result Tokens
	var outcome error
	err := database.InTransaction(ctx, s.DB, func(tx pgx.Tx) error {
		result = Tokens{}
		outcome = denied()
		var family uuid.UUID
		err := tx.QueryRow(ctx, `SELECT family_id FROM photo.sessions WHERE refresh_hash=$1`, HashSecret(raw)).Scan(&family)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "session:"+family.String()); err != nil {
			return err
		}
		var sid, user uuid.UUID
		var storedClient, status string
		var expires time.Time
		var used, revoked *time.Time
		var csrfHash []byte
		var epoch int64
		err = tx.QueryRow(ctx, `SELECT s.id,s.user_id,s.client_type,s.expires_at,s.used_at,s.revoked_at,s.csrf_hash,u.auth_epoch,u.status FROM photo.sessions s JOIN photo.users u ON u.id=s.user_id WHERE refresh_hash=$1 FOR UPDATE OF s`, HashSecret(raw)).Scan(&sid, &user, &storedClient, &expires, &used, &revoked, &csrfHash, &epoch, &status)
		if err != nil {
			return err
		}
		now := s.now()
		if storedClient != client || (client == "web" && !Equal(csrfHash, HashSecret(csrf))) {
			return nil
		}
		if used != nil {
			_, err = tx.Exec(ctx, `UPDATE photo.sessions SET revoked_at=COALESCE(revoked_at,$2) WHERE family_id=$1`, family, now)
			return err
		}
		if revoked != nil || !now.Before(expires) || status != "active" {
			return nil
		}
		if _, err = tx.Exec(ctx, `UPDATE photo.sessions SET used_at=$2 WHERE id=$1`, sid, now); err != nil {
			return err
		}
		result, err = s.newSession(ctx, tx, user, family, &sid, client, epoch, expires)
		if err == nil {
			outcome = nil
		}
		return err
	})
	if err != nil {
		return Tokens{}, err
	}
	return result, outcome
}
func (s *Service) AuthenticateAccess(ctx context.Context, raw string) (Identity, error) {
	claims, err := s.Signer.Verify(raw)
	if err != nil {
		return Identity{}, denied()
	}
	user, _ := uuid.Parse(claims.Subject)
	sid, _ := uuid.Parse(claims.SessionID)
	var alive bool
	err = s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM photo.sessions s JOIN photo.users u ON u.id=s.user_id WHERE s.id=$1 AND s.user_id=$2 AND s.revoked_at IS NULL AND s.expires_at>$3 AND u.status='active' AND u.auth_epoch=$4)`, sid, user, s.now(), claims.Epoch).Scan(&alive)
	if err != nil {
		return Identity{}, err
	}
	if !alive {
		return Identity{}, denied()
	}
	return Identity{user, sid, claims.Epoch}, nil
}
func (s *Service) RevokeSession(ctx context.Context, user, sid uuid.UUID) error {
	return database.InTransaction(ctx, s.DB, func(tx pgx.Tx) error {
		var family uuid.UUID
		err := tx.QueryRow(ctx, `SELECT family_id FROM photo.sessions WHERE id=$1 AND user_id=$2`, sid, user).Scan(&family)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.NewError("NOT_FOUND", 404)
		}
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "session:"+family.String()); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE photo.sessions SET revoked_at=COALESCE(revoked_at,$2) WHERE family_id=$1`, family, s.now())
		return err
	})
}

// RenewCSRF is called only by the origin-checked CSRF bootstrap endpoint.
func (s *Service) RenewCSRF(ctx context.Context, refresh string) (string, time.Time, error) {
	if len(refresh) != 43 {
		return "", time.Time{}, denied()
	}
	token, err := NewSecret(s.random())
	if err != nil {
		return "", time.Time{}, err
	}
	var expires time.Time
	err = s.DB.QueryRow(ctx, `UPDATE photo.sessions s SET csrf_hash=$2 FROM photo.users u WHERE s.refresh_hash=$1 AND s.user_id=u.id AND u.status='active' AND s.client_type='web' AND s.revoked_at IS NULL AND s.used_at IS NULL AND s.expires_at>$3 RETURNING s.expires_at`, HashSecret(refresh), HashSecret(token), s.now()).Scan(&expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", time.Time{}, denied()
	}
	return token, expires, err
}
func (s *Service) ValidateSessionCSRF(ctx context.Context, sid uuid.UUID, token string) error {
	var hash []byte
	err := s.DB.QueryRow(ctx, `SELECT csrf_hash FROM photo.sessions WHERE id=$1 AND revoked_at IS NULL AND expires_at>$2`, sid, s.now()).Scan(&hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return denied()
	}
	if err != nil {
		return err
	}
	if !Equal(hash, HashSecret(token)) {
		return domain.NewError("CSRF_INVALID", 403)
	}
	return nil
}
