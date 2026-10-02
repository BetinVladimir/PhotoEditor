package collection

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/tenant"
)

type SelectionFilter struct {
	SectionID *uuid.UUID `json:"section_id,omitempty"`
	Rating    *int       `json:"rating,omitempty"`
	Flag      *string    `json:"flag,omitempty"`
	Tag       *string    `json:"tag,omitempty"`
}

type SelectionRequest struct {
	CollectionRevision string           `json:"collection_revision"`
	Scope              string           `json:"scope"`
	ItemIDs            []uuid.UUID      `json:"item_ids,omitempty"`
	Filter             *SelectionFilter `json:"filter,omitempty"`
}

type SnapshotRef struct {
	ID        uuid.UUID `json:"id"`
	Digest    string    `json:"digest"`
	ItemCount int64     `json:"item_count"`
	ExpiresAt time.Time `json:"expires_at"`
}

type selectionEntry struct {
	ItemID         uuid.UUID `json:"item_id"`
	AssetVersionID uuid.UUID `json:"asset_version_id"`
	ArtifactID     uuid.UUID `json:"artifact_id"`
	SHA256         string    `json:"sha256"`
	Position       int64     `json:"position"`
	CapturedAt     *string   `json:"captured_at"`
	CameraHash     *string   `json:"camera_hash"`
}

func normalizeSelection(request SelectionRequest) (SelectionRequest, int64, error) {
	revision, err := strconv.ParseInt(request.CollectionRevision, 10, 64)
	if err != nil || revision < 1 || revision > 9007199254740991 {
		return request, 0, domain.NewError("VALIDATION_FAILED", 422)
	}
	request.CollectionRevision = strconv.FormatInt(revision, 10)
	request.Scope = strings.ToLower(strings.TrimSpace(request.Scope))
	switch request.Scope {
	case "selected":
		ids, ok := sortedUniqueIDs(request.ItemIDs, 5000)
		if !ok || request.Filter != nil {
			return request, 0, domain.NewError("VALIDATION_FAILED", 422)
		}
		request.ItemIDs = ids
	case "all":
		if len(request.ItemIDs) != 0 || request.Filter != nil {
			return request, 0, domain.NewError("VALIDATION_FAILED", 422)
		}
		request.ItemIDs = nil
	case "filter":
		if len(request.ItemIDs) != 0 || request.Filter == nil {
			return request, 0, domain.NewError("VALIDATION_FAILED", 422)
		}
		filter := request.Filter
		if filter.SectionID == nil && filter.Rating == nil && filter.Flag == nil && filter.Tag == nil {
			return request, 0, domain.NewError("VALIDATION_FAILED", 422)
		}
		if filter.SectionID != nil && *filter.SectionID == uuid.Nil || filter.Rating != nil && (*filter.Rating < 0 || *filter.Rating > 5) {
			return request, 0, domain.NewError("VALIDATION_FAILED", 422)
		}
		if filter.Flag != nil {
			value := strings.ToLower(strings.TrimSpace(*filter.Flag))
			if value != "none" && value != "pick" && value != "reject" {
				return request, 0, domain.NewError("VALIDATION_FAILED", 422)
			}
			filter.Flag = &value
		}
		if filter.Tag != nil {
			value := strings.TrimSpace(*filter.Tag)
			if value == "" || len([]rune(value)) > 64 {
				return request, 0, domain.NewError("VALIDATION_FAILED", 422)
			}
			filter.Tag = &value
		}
	default:
		return request, 0, domain.NewError("VALIDATION_FAILED", 422)
	}
	return request, revision, nil
}

func loadSelection(ctx context.Context, tx pgx.Tx, tid, collectionID uuid.UUID, request SelectionRequest) ([]selectionEntry, error) {
	var section *uuid.UUID
	var rating *int
	flag, tag := "", ""
	if request.Filter != nil {
		section, rating = request.Filter.SectionID, request.Filter.Rating
		if request.Filter.Flag != nil {
			flag = *request.Filter.Flag
		}
		if request.Filter.Tag != nil {
			tag = *request.Filter.Tag
		}
	}
	var ids any
	if request.Scope == "selected" {
		ids = request.ItemIDs
	}
	rows, err := tx.Query(ctx, `SELECT i.id,i.asset_version_id,v.master_artifact_id,v.content_hash,i.position,
 v.metadata->>'captured_at',CASE WHEN v.metadata->'ingest'->'sensor_alignment'->>'camera_id'
 ~ '^raw-[0-9a-f]{64}$' THEN substring(v.metadata->'ingest'->'sensor_alignment'->>'camera_id' from 5)
 ELSE v.metadata->>'camera_hash' END
 FROM photo.collection_items i JOIN photo.asset_versions v ON v.tenant_id=i.tenant_id AND v.id=i.asset_version_id AND v.asset_id=i.asset_id
 JOIN photo.assets a ON a.tenant_id=i.tenant_id AND a.id=i.asset_id AND a.deleted_at IS NULL AND a.upload_state='verified'
 JOIN photo.artifacts ar ON ar.tenant_id=v.tenant_id AND ar.id=v.master_artifact_id AND ar.sha256=v.content_hash AND ar.state='ready' AND (ar.expires_at IS NULL OR ar.expires_at>clock_timestamp())
 WHERE i.tenant_id=$1 AND i.collection_id=$2 AND ($3::uuid[] IS NULL OR i.id=ANY($3::uuid[]))
 AND ($4::uuid IS NULL OR i.section_id=$4) AND ($5::integer IS NULL OR i.rating=$5) AND ($6='' OR i.flag=$6) AND ($7='' OR i.tags ? $7)
 ORDER BY i.position,i.id LIMIT 5001`, tid, collectionID, ids, section, rating, flag, tag)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]selectionEntry, 0)
	for rows.Next() {
		var item selectionEntry
		if err = rows.Scan(&item.ItemID, &item.AssetVersionID, &item.ArtifactID, &item.SHA256, &item.Position,
			&item.CapturedAt, &item.CameraHash); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(out) < 1 || len(out) > 5000 {
		return nil, domain.NewError("VALIDATION_FAILED", 422)
	}
	if request.Scope == "selected" && len(out) != len(request.ItemIDs) {
		return nil, domain.NewError("SNAPSHOT_STALE", 409)
	}
	return out, nil
}

func (s Service) CreateSelection(ctx context.Context, who auth.Identity, tid, collectionID uuid.UUID, key string, raw SelectionRequest) (idempotency.Response, error) {
	var out idempotency.Response
	request, revision, err := normalizeSelection(raw)
	if err != nil {
		return out, err
	}
	now := time.Now().UTC()
	expires := now.Add(24 * time.Hour)
	err = tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		// Viewer selection is allowed, but project visibility must be checked before replay.
		var projectID uuid.UUID
		e := tx.QueryRow(ctx, `SELECT project_id FROM photo.collections WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`, tid, collectionID).Scan(&projectID)
		if errors.Is(e, pgx.ErrNoRows) {
			return domain.NewError("NOT_FOUND", 404)
		}
		if e != nil {
			return e
		}
		if _, e = project.Authorize(ctx, tx, tid, projectID, who.UserID, role, domain.Viewer, false); e != nil {
			return e
		}
		out, e = idempotency.Use(ctx, tx, idempotency.Scope{Tenant: tid, Principal: who.UserID, PrincipalType: "user", Route: "Selection.Create", Key: key}, request, now.Add(24*time.Hour), func() (idempotency.Response, error) {
			collection, e := authorize(ctx, tx, who, tid, collectionID, role, domain.Viewer, true)
			if e != nil {
				return idempotency.Response{}, e
			}
			if collection.State == "archived" || collection.Revision != strconv.FormatInt(revision, 10) {
				return idempotency.Response{}, domain.NewError("SNAPSHOT_STALE", 409)
			}
			if request.Filter != nil && request.Filter.SectionID != nil {
				if e = verifySection(ctx, tx, tid, collectionID, request.Filter.SectionID); e != nil {
					return idempotency.Response{}, e
				}
			}
			items, e := loadSelection(ctx, tx, tid, collectionID, request)
			if e != nil {
				return idempotency.Response{}, e
			}
			digest, e := canonical.Hash(items)
			if e != nil {
				return idempotency.Response{}, e
			}
			result := SnapshotRef{ID: uuid.New(), Digest: digest, ItemCount: int64(len(items)), ExpiresAt: expires}
			_, e = tx.Exec(ctx, `INSERT INTO photo.snapshots(id,tenant_id,kind,collection_id,source_revision,principal_type,principal_id,digest,item_count,expires_at) VALUES($1,$2,'selection',$3,$4,'user',$5,$6,$7,$8)`, result.ID, tid, collectionID, revision, who.UserID, digest, len(items), expires)
			if e != nil {
				return idempotency.Response{}, e
			}
			for ordinal, item := range items {
				payload, _ := canonical.Marshal(map[string]any{"position": item.Position,
					"captured_at": item.CapturedAt, "camera_hash": item.CameraHash})
				if _, e = tx.Exec(ctx, `INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,item_id,asset_version_id,artifact_id,sha256,payload) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, tid, result.ID, ordinal, item.ItemID, item.AssetVersionID, item.ArtifactID, item.SHA256, payload); e != nil {
					return idempotency.Response{}, e
				}
			}
			if e = audit(ctx, tx, who, tid, collectionID, "selection.create"); e != nil {
				return idempotency.Response{}, e
			}
			body, e := canonical.Marshal(result)
			return idempotency.Response{Status: 201, Body: body, ResourceID: &result.ID}, e
		})
		return e
	})
	return out, err
}
