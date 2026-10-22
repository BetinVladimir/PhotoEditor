package v2render

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// AuthoredNativeReplayConsumerBinding owns the SQL-selected effective transport.
// It grants no qualification and must not replace immutable base authority.
type AuthoredNativeReplayConsumerBinding struct {
	raw                       []byte
	baseDigest, finalDigest   string
	baseProfile, finalProfile []byte
	entries                   []v2input.Entry
}

func (b AuthoredNativeReplayConsumerBinding) InputDigest() string     { return b.finalDigest }
func (b AuthoredNativeReplayConsumerBinding) BaseInputDigest() string { return b.baseDigest }
func (b AuthoredNativeReplayConsumerBinding) ProfileJCS() []byte      { return bytes.Clone(b.finalProfile) }
func (b AuthoredNativeReplayConsumerBinding) BaseProfileJCS() []byte {
	return bytes.Clone(b.baseProfile)
}
func (b AuthoredNativeReplayConsumerBinding) Entries() []v2input.Entry {
	return append([]v2input.Entry(nil), b.entries...)
}
func (b AuthoredNativeReplayConsumerBinding) CanonicalReadback() []byte { return bytes.Clone(b.raw) }

// BindAuthoredNativeReplayConsumerTransport runs in the caller's actual tenant
// transaction, before first closure publication/seal. The caller must roll back
// on any error; in particular the SQL post-write refusal deliberately raises.
func BindAuthoredNativeReplayConsumerTransport(ctx context.Context, tx pgx.Tx, tenant, task uuid.UUID, baseEntries, entries []v2input.Entry) (AuthoredNativeReplayConsumerBinding, error) {
	var zero AuthoredNativeReplayConsumerBinding
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if tx == nil || tenant == uuid.Nil || task == uuid.Nil || len(baseEntries) == 0 || len(entries) == 0 {
		return zero, v2input.ErrInvalid
	}
	baseRaw, err := canonical.Marshal(baseEntries)
	if err != nil {
		return zero, err
	}
	rawEntries, err := canonical.Marshal(entries)
	if err != nil {
		return zero, err
	}
	var raw []byte
	if err = tx.QueryRow(ctx, "SELECT photo.bind_authored_native_replay_consumer_transport($1,$2,$3,$4)", tenant, task, string(baseRaw), string(rawEntries)).Scan(&raw); err != nil {
		return zero, err
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	return decodeAuthoredNativeReplayConsumerBinding(raw)
}

func decodeAuthoredNativeReplayConsumerBinding(raw []byte) (AuthoredNativeReplayConsumerBinding, error) {
	var zero AuthoredNativeReplayConsumerBinding
	if len(raw) == 0 || len(raw) > 268435456 {
		return zero, v2input.ErrInvalid
	}
	var row struct {
		BaseSnapshot     json.RawMessage `json:"base_snapshot"`
		BaseInputDigest  string          `json:"base_input_digest"`
		FinalInputDigest string          `json:"final_input_digest"`
		EntriesJCS       string          `json:"entries_jcs"`
		Roots            json.RawMessage `json:"request_evidence_roots"`
		BaseProfile      json.RawMessage `json:"base_profile"`
		FinalProfile     json.RawMessage `json:"final_profile"`
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&row) != nil {
		return zero, v2input.ErrInvalid
	}
	var keys map[string]json.RawMessage
	if json.Unmarshal(raw, &keys) != nil || len(keys) != 7 {
		return zero, v2input.ErrInvalid
	}
	if !validReplayDigest(row.BaseInputDigest) || !validReplayDigest(row.FinalInputDigest) || len(row.BaseSnapshot) == 0 || bytes.Equal(row.BaseSnapshot, []byte("null")) || len(row.Roots) == 0 {
		return zero, v2input.ErrInvalid
	}
	for _, object := range []json.RawMessage{row.BaseSnapshot, row.BaseProfile, row.FinalProfile} {
		var fields map[string]json.RawMessage
		if json.Unmarshal(object, &fields) != nil || fields == nil {
			return zero, v2input.ErrInvalid
		}
	}
	var roots []json.RawMessage
	if json.Unmarshal(row.Roots, &roots) != nil || len(roots) == 0 {
		return zero, v2input.ErrInvalid
	}
	var entries []v2input.Entry
	if json.Unmarshal([]byte(row.EntriesJCS), &entries) != nil || len(entries) == 0 {
		return zero, v2input.ErrInvalid
	}
	check, err := canonical.Marshal(entries)
	if err != nil || string(check) != row.EntriesJCS {
		return zero, v2input.ErrInvalid
	}
	base, err := canonical.Bytes(row.BaseProfile)
	if err != nil {
		return zero, err
	}
	final, err := canonical.Bytes(row.FinalProfile)
	if err != nil {
		return zero, err
	}
	owned, err := canonical.Bytes(raw)
	if err != nil {
		return zero, err
	}
	return AuthoredNativeReplayConsumerBinding{bytes.Clone(owned), row.BaseInputDigest, row.FinalInputDigest, bytes.Clone(base), bytes.Clone(final), append([]v2input.Entry(nil), entries...)}, nil
}
func validReplayDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
