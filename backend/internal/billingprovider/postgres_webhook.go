package billingprovider

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
)

type WebhookDatabase interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type PostgresWebhookRecorder struct {
	db                       WebhookDatabase
	account                  uuid.UUID
	externalAccount, version string
	keys                     auth.KeyRing
}

func NewPostgresWebhookRecorder(db WebhookDatabase, account uuid.UUID, externalAccount, version string, keys auth.KeyRing) (*PostgresWebhookRecorder, error) {
	if db == nil || account == uuid.Nil || !strings.HasPrefix(externalAccount, "acct_") || version == "" || keys.Active == "" || len(keys.Keys[keys.Active]) != 32 {
		return nil, errors.New("stripe inbox configuration invalid")
	}
	owned := auth.KeyRing{Active: keys.Active, Keys: map[string][]byte{}}
	for id, key := range keys.Keys {
		if id == "" || len(key) != 32 {
			return nil, errors.New("stripe inbox encryption key invalid")
		}
		owned.Keys[id] = bytes.Clone(key)
	}
	return &PostgresWebhookRecorder{db: db, account: account, externalAccount: externalAccount, version: version, keys: owned}, nil
}

type encryptedWebhook struct {
	Version    int    `json:"version"`
	KeyID      string `json:"key_id"`
	Ciphertext []byte `json:"ciphertext"`
}

func webhookPayloadAAD(account uuid.UUID, externalAccount, eventID string, hash []byte) []byte {
	data, _ := json.Marshal([]string{"billing-stripe-inbox-v1", account.String(), externalAccount, eventID, hex.EncodeToString(hash)})
	return data
}

// OpenWebhookPayload binds ciphertext to the account, event and immutable raw hash.
func OpenWebhookPayload(keys auth.KeyRing, account uuid.UUID, externalAccount, eventID string, hash, ciphertext []byte) ([]byte, error) {
	var encrypted encryptedWebhook
	decoder := json.NewDecoder(bytes.NewReader(ciphertext))
	decoder.DisallowUnknownFields()
	if len(hash) != 32 || decoder.Decode(&encrypted) != nil || encrypted.Version != 1 || encrypted.KeyID == "" {
		return nil, errors.New("stripe inbox ciphertext invalid")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return nil, errors.New("stripe inbox ciphertext invalid")
	}
	raw, err := keys.Open(encrypted.KeyID, encrypted.Ciphertext, webhookPayloadAAD(account, externalAccount, eventID, hash))
	if err != nil {
		return nil, errors.New("stripe inbox ciphertext invalid")
	}
	actual := sha256.Sum256(raw)
	if !bytes.Equal(actual[:], hash) {
		return nil, errors.New("stripe inbox payload hash invalid")
	}
	return raw, nil
}

func (s *PostgresWebhookRecorder) Record(ctx context.Context, entry WebhookInboxEntry) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if s == nil || s.db == nil || entry.ID == uuid.Nil || entry.AccountID != s.externalAccount || entry.State != WebhookInboxStatePending {
		return false, errors.New("stripe inbox input invalid")
	}
	var event struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Object  string `json:"object"`
		Account string `json:"account"`
		Version string `json:"api_version"`
		Live    *bool  `json:"livemode"`
		Created int64  `json:"created"`
		Data    struct {
			Object struct {
				ID string `json:"id"`
			} `json:"object"`
		} `json:"data"`
	}
	if json.Unmarshal(entry.RawPayload, &event) != nil || event.ID != entry.ProviderEventID || event.Type != entry.EventType || event.Object != "event" || event.Version != s.version || event.Live == nil || *event.Live || event.Created <= 0 || event.Data.Object.ID == "" || (event.Account != "" && event.Account != s.externalAccount) {
		return false, errors.New("stripe inbox envelope invalid")
	}
	hash := sha256.Sum256(entry.RawPayload)
	keyID, ciphertext, err := s.keys.Seal(entry.RawPayload, webhookPayloadAAD(s.account, s.externalAccount, event.ID, hash[:]))
	if err != nil {
		return false, errors.New("stripe inbox encryption failed")
	}
	encrypted, err := json.Marshal(encryptedWebhook{Version: 1, KeyID: keyID, Ciphertext: ciphertext})
	if err != nil {
		return false, errors.New("stripe inbox encryption failed")
	}
	var id uuid.UUID
	var created, quarantined bool
	err = s.db.QueryRow(ctx, `SELECT inbox_id,created,quarantined FROM photo.billing_ingest_stripe_webhook($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, entry.ID, s.account, s.externalAccount, s.version, event.ID, event.Type, event.Data.Object.ID, time.Unix(event.Created, 0).UTC(), encrypted, hash[:]).Scan(&id, &created, &quarantined)
	if err != nil {
		return false, err
	}
	if quarantined {
		return false, ErrWebhookPayloadConflict
	}
	return created, nil
}
