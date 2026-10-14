package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/exportpreset"
	"strconv"
)

type CreateCatalogPreset struct {
	ID        uuid.UUID
	Revision  string
	SHA256    string
	ConfigJCS []byte
}
type CreateCatalog struct {
	Snapshot       uuid.UUID
	SnapshotDigest string
	ItemCount      int
	Presets        []CreateCatalogPreset
}

// The returned observation must be durably pinned before any object IO. It is
// not a source closure or admission receipt and must be rechecked at commit.
func (i CreateIntent) ResolveCatalog(ctx context.Context, tx ProofTransaction, who auth.Identity, tid uuid.UUID) (CreateCatalog, error) {
 return i.resolveCatalog(ctx,tx,who,tid,false)
}
// ResolveWatermarkCatalog observes authorized frozen preset candidates only;
// it grants no pin/publication/task admission authority.
func(i CreateIntent)ResolveWatermarkCatalog(ctx context.Context,tx ProofTransaction,who auth.Identity,tid uuid.UUID)(CreateCatalog,error){return i.resolveCatalog(ctx,tx,who,tid,true)}
func(i CreateIntent)resolveCatalog(ctx context.Context,tx ProofTransaction,who auth.Identity,tid uuid.UUID,watermark bool)(CreateCatalog,error){
	if err := ctx.Err(); err != nil {
		return CreateCatalog{}, err
	}
	if tx == nil || tid == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || i.identity.Run == uuid.Nil || tid != i.tenant || who.UserID != i.actor {
		return CreateCatalog{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	request := i.Request()
	ids := append(append([]uuid.UUID{}, request.Policy.RequiredPresets...), request.Policy.OptionalPresets...)
	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT photo.preflight_v30_delivery_create($1,$2,$3,$4,$5,$6)`, tid, request.ProjectID, request.SnapshotID, who.UserID, who.SessionID, ids).Scan(&raw); err != nil {
		return CreateCatalog{}, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return CreateCatalog{}, domain.NewError("DELIVERY_SOURCE_STALE", 409)
	}
	return i.decodeCatalog(raw,watermark)
}
func (i CreateIntent) DecodeCatalog(raw []byte) (CreateCatalog, error) {
 return i.decodeCatalog(raw,false)
}
func(i CreateIntent)DecodeWatermarkCatalog(raw []byte)(CreateCatalog,error){return i.decodeCatalog(raw,true)}
func(i CreateIntent)decodeCatalog(raw []byte,watermark bool)(CreateCatalog,error){
	invalid := func() (CreateCatalog, error) {
		return CreateCatalog{}, domain.NewError("DELIVERY_CATALOG_INVALID", 500)
	}
	var wire struct {
		Snapshot uuid.UUID `json:"snapshot_id"`
		Digest   string    `json:"snapshot_digest"`
		Count    int       `json:"item_count"`
		Presets  []struct {
			ID       uuid.UUID           `json:"id"`
			Revision string              `json:"revision"`
			SHA      string              `json:"sha256"`
			Config   exportpreset.Config `json:"config"`
		} `json:"presets"`
	}
	if len(raw)<2 || len(raw)>8<<20 { return invalid() }
	decoder:=json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&wire)!=nil || decoder.Decode(new(any))!=io.EOF || wire.Snapshot != i.request.SnapshotID || !shaPattern.MatchString(wire.Digest) || wire.Count < 1 || wire.Count > 5000 {
		return invalid()
	}
	ids := append(append([]uuid.UUID{}, i.request.Policy.RequiredPresets...), i.request.Policy.OptionalPresets...)
	if len(wire.Presets) != len(ids) || len(ids) < 1 {
		return invalid()
	}
	out := CreateCatalog{Snapshot: wire.Snapshot, SnapshotDigest: wire.Digest, ItemCount: wire.Count, Presets: make([]CreateCatalogPreset, 0, len(ids))}
	for index, preset := range wire.Presets {
		n, err := strconv.ParseInt(preset.Revision, 10, 64)
		if err != nil || n < 1 || strconv.FormatInt(n, 10) != preset.Revision || preset.ID != ids[index] || !shaPattern.MatchString(preset.SHA) || exportpreset.ValidateConfig(preset.Config) != nil {
			return invalid()
		}
		if preset.Config.Watermark != nil && !watermark {
			return CreateCatalog{}, domain.NewError("DELIVERY_WATERMARK_UNAVAILABLE", 503)
		}
		if preset.Config.MetadataPolicy != i.request.Policy.MetadataPolicy {
			return CreateCatalog{}, domain.NewError("DELIVERY_METADATA_UNAVAILABLE", 503)
		}
		body, err := canonical.Marshal(preset.Config)
		if err != nil || digest(body) != preset.SHA {
			return invalid()
		}
		out.Presets = append(out.Presets, CreateCatalogPreset{ID: preset.ID, Revision: preset.Revision, SHA256: preset.SHA, ConfigJCS: body})
	}
	return out, nil
}
