package v2render

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
	"strings"
)

type MultiOriginalObject struct {
	Ordinal int
	Role    string
	Ref     v2input.Ref
	Object  objectstore.Object
}

// Metadata is owned and comparable per occurrence. It is not cached authority.
type MultiOriginalInventory struct {
	binding AdoptedMultiBinding
	root    objectstore.Object
	bucket  string
	items   []MultiOriginalObject
}

func (i MultiOriginalInventory) owned() MultiOriginalInventory {
	i.items = append([]MultiOriginalObject(nil), i.items...)
	return i
}

func (i MultiOriginalInventory) Snapshot() (AdoptedMultiBinding, objectstore.Object, []MultiOriginalObject) {
	return i.binding, i.root, append([]MultiOriginalObject(nil), i.items...)
}
func (i MultiOriginalInventory) equal(other MultiOriginalInventory) bool {
	if i.binding != other.binding || i.root != other.root || i.bucket != other.bucket || len(i.items) != len(other.items) {
		return false
	}
	for n, item := range i.items {
		if item != other.items[n] {
			return false
		}
	}
	return true
}

type multiOriginalLocatorWire struct {
	Bucket  string `json:"bucket"`
	Key     string `json:"object_key"`
	Version string `json:"object_version"`
	Bytes   int64  `json:"bytes"`
	Media   string `json:"media_type"`
}
type multiOriginalItemWire struct {
	Ordinal int         `json:"ordinal"`
	Role    string      `json:"role"`
	Ref     v2input.Ref `json:"ref"`
	Bucket  string      `json:"bucket"`
	Key     string      `json:"object_key"`
	Version string      `json:"object_version"`
	Bytes   int64       `json:"bytes"`
	Media   string      `json:"media_type"`
}
type multiOriginalInventoryWire struct {
	SchemaVersion int                      `json:"schema_version"`
	Binding       AdoptedMultiBinding      `json:"binding"`
	Root          multiOriginalLocatorWire `json:"root_object"`
	Items         []multiOriginalItemWire  `json:"items"`
}

func decodeMultiOriginalInventory(raw []byte, expected AdoptedMultiBinding, producer uuid.UUID, bucket string) (MultiOriginalInventory, error) {
	var wire multiOriginalInventoryWire
	var empty MultiOriginalInventory
	if len(raw) < 2 || len(raw) > 8*1024*1024 || json.Unmarshal(raw, &wire) != nil {
		return empty, v2input.ErrInvalid
	}
	normal, err := canonical.Bytes(raw)
	if err != nil {
		return empty, err
	}
	exact, err := canonical.Marshal(wire)
 // Replacement identity is privately resolved by the caller, outside the
 // unchanged inventory wire schema. Never accept it from inventory JSON.
 expectedWire:=expected
 expectedWire.Replacement=MultiReplacementBindingIdentity{}
 if err != nil || !bytes.Equal(exact, normal) || wire.SchemaVersion != 1 || wire.Binding != expectedWire || wire.Root.Bucket != bucket {
		return empty, v2input.ErrInvalid
	}
	wire.Binding.Replacement=expected.Replacement
	inv := MultiOriginalInventory{binding: wire.Binding, bucket: bucket, root: objectstore.Object{Key: wire.Root.Key, VersionID: wire.Root.Version, Bytes: wire.Root.Bytes, ContentType: wire.Root.Media, SHA256: expected.Result.SHA256}}
	for _, item := range wire.Items {
		if item.Bucket != bucket {
			return empty, v2input.ErrInvalid
		}
		inv.items = append(inv.items, MultiOriginalObject{Ordinal: item.Ordinal, Role: item.Role, Ref: item.Ref, Object: objectstore.Object{Key: item.Key, VersionID: item.Version, Bytes: item.Bytes, ContentType: item.Media, SHA256: item.Ref.SHA256}})
	}
	if err := validateMultiOriginalInventory(inv, expected, producer, bucket); err != nil {
		return empty, err
	}
	return inv, nil
}
func validateMultiOriginalInventory(inv MultiOriginalInventory, expected AdoptedMultiBinding, producer uuid.UUID, bucket string) error {
	if bucket == "" || inv.bucket != bucket || inv.binding != expected || !validAdoptedMultiRootObject(inv.root, expected.Result) || len(inv.items) < 4 || len(inv.items) > 3930 {
		return v2input.ErrInvalid
	}
	if err := validateAdoptedMultiBinding(expected, producer, expected.Operation, expected.Result); err != nil {
		return err
	}
	frames, _, calibration, err := expected.Inputs()
	if err != nil {
		return err
	}
	sourceIDs := map[uuid.UUID]bool{expected.Result.ArtifactID: true, calibration.ArtifactID: true}
	for _, frame := range frames {
		sourceIDs[frame.Source.ArtifactID] = true
	}
	required := make([]map[string]bool, len(frames))
	seen := map[uuid.UUID]MultiOriginalObject{}
	for n := range required {
		required[n] = map[string]bool{}
	}
	for index, item := range inv.items {
		if item.Ordinal < 0 || item.Ordinal >= len(frames) || !validLayerArtifactRef(item.Ref) {
			return v2input.ErrInvalid
		}
		if index > 0 {
			prior := inv.items[index-1]
			if prior.Ordinal > item.Ordinal || prior.Ordinal == item.Ordinal && prior.Role >= item.Role {
				return v2input.ErrInvalid
			}
		}
		media, limit, legacy, ok := multiOriginalObjectLimit(item.Role)
		if !ok || !validMultiChildObject(item.Object, item.Ref, media, limit, legacy) {
			return v2input.ErrInvalid
		}
		if item.Role == "WorkingImage" {
			if item.Ref != frames[item.Ordinal].Source {
				return v2input.ErrInvalid
			}
		} else if sourceIDs[item.Ref.ArtifactID] {
			return v2input.ErrInvalid
		}
		if prior, ok := seen[item.Ref.ArtifactID]; ok {
			kind := func(role string) string {
				if strings.HasPrefix(role, "InverseField/") {
					return "InverseField"
				}
				return role
			}
			if prior.Ref != item.Ref || prior.Object != item.Object || kind(prior.Role) != kind(item.Role) {
				return v2input.ErrInvalid
			}
		} else {
			seen[item.Ref.ArtifactID] = item
		}
		required[item.Ordinal][item.Role] = true
	}
	for _, roles := range required {
		if !roles["WorkingImage"] || !roles["WorkingPixels"] {
			return v2input.ErrInvalid
		}
	}
	return nil
}

func (s SQLAdoptedMultiSource) LoadOriginalInventory(ctx context.Context, who auth.Identity, tenantID, project, producer uuid.UUID, expected AdoptedMultiBinding, bucket string) (MultiOriginalInventory, error) {
	var empty MultiOriginalInventory
	if s.DB == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || tenantID == uuid.Nil || project == uuid.Nil || bucket == "" {
		return empty, v2input.ErrInvalid
	}
	if err := validateAdoptedMultiBinding(expected, producer, expected.Operation, expected.Result); err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	var raw []byte
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_api" {
			return v2input.ErrInvalid
		}
		return tx.QueryRow(ctx, `SELECT photo.v29_adopted_multi_original_inventory($1,$2,$3,$4,$5,$6,$7,$8)`, tenantID, project, producer, who.UserID, who.SessionID, expected.Operation, expected.Result.ArtifactID, expected.Result.SHA256).Scan(&raw)
	})
	if err != nil {
		return empty, err
	}
	if raw == nil {
		return empty, v2input.ErrExpired
	}
	return decodeMultiOriginalInventory(raw, expected, producer, bucket)
}
