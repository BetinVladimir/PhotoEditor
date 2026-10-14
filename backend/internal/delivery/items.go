package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
	"strconv"
	"strings"
)

type ItemsQuery struct {
	Revision     string
	AfterOrdinal int
	AfterPreset  uuid.UUID
	Limit        int
}

func (q ItemsQuery) Validate() error {
	revision, err := strconv.ParseInt(q.Revision, 10, 64)
	if err != nil || revision < 1 || revision > 9007199254740991 || strconv.FormatInt(revision, 10) != q.Revision || q.Limit < 1 || q.Limit > 200 || q.AfterOrdinal < -1 || q.AfterOrdinal >= 5000 || (q.AfterOrdinal == -1) != (q.AfterPreset == uuid.Nil) {
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	return nil
}

// Public metadata allowlist; never forward storage coordinates from a reader.
func ValidItemsResource(raw []byte, id uuid.UUID, q ItemsQuery) bool {
	if len(raw) < 2 || len(raw) > 1<<20 || id == uuid.Nil || q.Validate() != nil {
		return false
	}
	root, ok := closedJSONObject(raw)
	if !ok || len(root) != 5 {
		return false
	}
	for _, key := range []string{"package_id", "revision", "manifest", "items", "next"} {
		if _, ok := root[key]; !ok {
			return false
		}
	}
	var packageID uuid.UUID
	var revision string
	var items []json.RawMessage
	if json.Unmarshal(root["package_id"], &packageID) != nil || packageID != id || json.Unmarshal(root["revision"], &revision) != nil || revision != q.Revision || string(root["items"]) == "null" || json.Unmarshal(root["items"], &items) != nil || len(items) > q.Limit {
		return false
	}
	if string(root["manifest"]) != "null" {
		fields, ok := closedJSONObject(root["manifest"])
		var ref v2input.Ref
		if !ok || len(fields) != 2 || json.Unmarshal(root["manifest"], &ref) != nil || ref.ArtifactID == uuid.Nil || !shaPattern.MatchString(ref.SHA256) {
			return false
		}
	}
	previousOrdinal, previousPreset := q.AfterOrdinal, q.AfterPreset
	seen := map[uuid.UUID]bool{}
	for _, itemRaw := range items {
		item, ok := closedJSONObject(itemRaw)
		if !ok {
			return false
		}
		if len(item) != 8 {
			return false
		}
		for _, key := range []string{"id", "ordinal", "preset_id", "preset_sha", "required", "export_id", "state", "reason"} {
			if _, ok := item[key]; !ok {
				return false
			}
		}
		var row struct {
			ID       uuid.UUID  `json:"id"`
			Ordinal  *int       `json:"ordinal"`
			Preset   uuid.UUID  `json:"preset_id"`
			SHA      string     `json:"preset_sha"`
			Required *bool      `json:"required"`
			Export   *uuid.UUID `json:"export_id"`
			State    string     `json:"state"`
			Reason   *string    `json:"reason"`
		}
		encoded, err := json.Marshal(item)
		if err != nil || json.Unmarshal(encoded, &row) != nil || row.ID == uuid.Nil || seen[row.ID] || row.Ordinal == nil || *row.Ordinal < 0 || *row.Ordinal >= 5000 || row.Preset == uuid.Nil || !shaPattern.MatchString(row.SHA) || row.Required == nil {
			return false
		}
		if *row.Ordinal < previousOrdinal || (*row.Ordinal == previousOrdinal && bytes.Compare(row.Preset[:], previousPreset[:]) <= 0) {
			return false
		}
		if row.Export != nil && *row.Export == uuid.Nil {
			return false
		}
		switch row.State {
		case "pending", "ready":
			if row.Reason != nil {
				return false
			}
		case "failed", "omitted":
			if row.Reason == nil || strings.TrimSpace(*row.Reason) == "" || len([]rune(*row.Reason)) > 256 {
				return false
			}
		default:
			return false
		}
		if row.State == "ready" && row.Export == nil {
			return false
		}
		if row.State == "omitted" && (*row.Required || row.Export != nil) {
			return false
		}
		seen[row.ID] = true
		previousOrdinal, previousPreset = *row.Ordinal, row.Preset
	}
	if string(root["next"]) != "null" {
		var next struct {
			Ordinal int       `json:"ordinal"`
			Preset  uuid.UUID `json:"preset_id"`
		}
		fields, ok := closedJSONObject(root["next"])
		if !ok || len(fields) != 2 || json.Unmarshal(root["next"], &next) != nil || next.Ordinal < 0 || next.Ordinal >= 5000 || next.Preset == uuid.Nil || next.Ordinal != previousOrdinal || next.Preset != previousPreset || len(items) != q.Limit {
			return false
		}
	}
	return true
}

func ReadItems(ctx context.Context, tx ProofTransaction, who auth.Identity, tid, id uuid.UUID, q ItemsQuery) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if tx == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || tid == uuid.Nil || id == uuid.Nil {
		return nil, domain.NewError("VALIDATION_FAILED", 422)
	}
	if err := q.Validate(); err != nil {
		return nil, err
	}
	revision, _ := strconv.ParseInt(q.Revision, 10, 64)
	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT photo.get_v30_delivery_items($1,$2,$3,$4,$5,$6,$7,$8)`, tid, who.UserID, who.SessionID, id, revision, q.AfterOrdinal, q.AfterPreset, q.Limit).Scan(&raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, domain.NewError("NOT_FOUND", 404)
	}
	var envelope struct {
		Stale bool `json:"stale"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, domain.NewError("DELIVERY_DOCUMENT_INVALID", 500)
	}
	if envelope.Stale {
		return nil, domain.NewError("DELIVERY_REVISION_STALE", 409)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !ValidItemsResource(raw, id, q) {
		return nil, domain.NewError("DELIVERY_DOCUMENT_INVALID", 500)
	}
	return append(json.RawMessage(nil), raw...), nil
}
func (s Service) Items(ctx context.Context, who auth.Identity, tid, id uuid.UUID, q ItemsQuery) (json.RawMessage, error) {
	if err := q.Validate(); err != nil {
		return nil, err
	}
	if s.DB == nil {
		return nil, domain.NewError("DELIVERY_UNAVAILABLE", 503)
	}
	var result json.RawMessage
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, _ domain.Role) error {
		var err error
		result, err = ReadItems(ctx, tx, who, tid, id, q)
		return err
	})
	return result, err
}
