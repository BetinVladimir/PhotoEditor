package v2capture

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
)

type PublicService struct {
	DB          *pgxpool.Pool
	Credentials Credentials
}

type ChallengeRequest struct {
	DeviceID uuid.UUID `json:"device_id"`
}
type ChallengeResponse struct {
	ChallengeID     uuid.UUID `json:"challenge_id"`
	Nonce           string    `json:"nonce"`
	ExpiresAt       time.Time `json:"expires_at"`
	CredentialEpoch string    `json:"credential_epoch"`
}
type TokenRequest struct {
	ChallengeID uuid.UUID `json:"challenge_id"`
	Signature   string    `json:"signature"`
}
type TokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

func (s PublicService) rate(ctx context.Context, tenantID, deviceID uuid.UUID,
	ip, kind string) error {
	if s.DB == nil || len(s.Credentials.Pepper) < 32 {
		return domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	function := "photo.capture_challenge_rate"
	if kind == "token" {
		function = "photo.capture_token_rate"
	}
	ipHash := auth.MAC(s.Credentials.Pepper, "capture-rate-ip", tenantID.String(), ip)
	deviceHash := auth.MAC(s.Credentials.Pepper, "capture-rate-"+kind, tenantID.String(), deviceID.String())
	var allowed bool
	err := s.DB.QueryRow(ctx, "SELECT "+function+"($1,$2)", ipHash, deviceHash).Scan(&allowed)
	if err != nil {
		return mutationError(err)
	}
	if !allowed {
		return domain.NewError("RATE_LIMITED", 429)
	}
	return nil
}

func (s PublicService) CreateChallenge(ctx context.Context, tenantID, deviceID uuid.UUID,
	ip string, req ChallengeRequest) (ChallengeResponse, error) {
	var out ChallengeResponse
	if tenantID == uuid.Nil || deviceID == uuid.Nil || req.DeviceID != deviceID || ip == "" {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	if err := s.rate(ctx, tenantID, deviceID, ip, "challenge"); err != nil {
		return out, err
	}
	var projectID uuid.UUID
	var publicKey string
	var epoch int64
	err := s.DB.QueryRow(ctx, `SELECT project_id,public_key,credential_epoch
 FROM photo.capture_challenge_target($1,$2)`, tenantID, deviceID).Scan(&projectID, &publicKey, &epoch)
	unknown := errors.Is(err, pgx.ErrNoRows)
	if err != nil && !unknown {
		return out, mutationError(err)
	}
	if unknown {
		epoch = 1
	}
	challengeID := uuid.New()
	nonce, version, sealed, hash, err := s.Credentials.NewChallenge(tenantID, deviceID, challengeID, epoch)
	if err != nil {
		return out, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	out = ChallengeResponse{ChallengeID: challengeID, Nonce: nonce,
		ExpiresAt:       s.Credentials.now().Add(60 * time.Second),
		CredentialEpoch: strconv.FormatInt(epoch, 10)}
	if unknown {
		return out, nil
	}
	var stored bool
	err = s.DB.QueryRow(ctx, `SELECT photo.create_capture_challenge($1,$2,$3,$4,$5,$6,$7)`,
		tenantID, deviceID, challengeID, epoch, sealed, version, hash).Scan(&stored)
	if err != nil {
		return ChallengeResponse{}, mutationError(err)
	}
	if !stored {
		return out, nil
	} // revocation race: preserve public response shape
	return out, nil
}

func (s PublicService) ExchangeChallenge(ctx context.Context, tenantID, deviceID uuid.UUID,
	ip string, req TokenRequest) (TokenResponse, error) {
	var out TokenResponse
	if tenantID == uuid.Nil || deviceID == uuid.Nil || req.ChallengeID == uuid.Nil ||
		len(req.Signature) != 86 || ip == "" {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	if err := s.rate(ctx, tenantID, deviceID, ip, "token"); err != nil {
		return out, err
	}
	var projectID uuid.UUID
	var publicKey, version, storedHash string
	var epoch int64
	var sealed []byte
	err := s.DB.QueryRow(ctx, `SELECT project_id,public_key,credential_epoch,
 key_version,nonce_ciphertext,nonce_hash
 FROM photo.capture_challenge_payload($1,$2,$3)`, tenantID, deviceID, req.ChallengeID).
		Scan(&projectID, &publicKey, &epoch, &version, &sealed, &storedHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, domain.NewError("INVALID_CREDENTIALS", 401)
	}
	if err != nil {
		return out, mutationError(err)
	}
	nonce, err := s.Credentials.OpenChallenge(tenantID, deviceID, req.ChallengeID, epoch, version, sealed)
	if err != nil {
		return out, domain.NewError("INVALID_CREDENTIALS", 401)
	}
	decoded, _ := base64.RawURLEncoding.DecodeString(nonce)
	digest := sha256.Sum256(decoded)
	hash := hex.EncodeToString(digest[:])
	if subtle.ConstantTimeCompare([]byte(hash), []byte(storedHash)) != 1 {
		return out, domain.NewError("INVALID_CREDENTIALS", 401)
	}
	payload, err := SignedChallenge(deviceID, req.ChallengeID, nonce, epoch, s.Credentials.Origin)
	if err != nil || VerifyChallengeSignature(publicKey, req.Signature, payload) != nil {
		return out, domain.NewError("INVALID_CREDENTIALS", 401)
	}
	token, err := s.Credentials.IssueToken(tenantID, projectID, deviceID, epoch)
	if err != nil {
		return out, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	var consumed bool
	err = s.DB.QueryRow(ctx, `SELECT photo.consume_capture_challenge($1,$2,$3,$4,$5)`,
		tenantID, deviceID, req.ChallengeID, epoch, hash).Scan(&consumed)
	if err != nil {
		return out, mutationError(err)
	}
	if !consumed {
		return out, domain.NewError("INVALID_CREDENTIALS", 401)
	}
	return TokenResponse{AccessToken: token, ExpiresIn: 900}, nil
}
