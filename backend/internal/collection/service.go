package collection

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/pagination"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/tenant"
)

type Service struct {
	DB      *pgxpool.Pool
	Cursors pagination.Codec
	Pepper  []byte
}

type Collection struct {
	ID                    uuid.UUID  `json:"id"`
	TenantID              uuid.UUID  `json:"tenant_id"`
	ProjectID             uuid.UUID  `json:"project_id"`
	Name                  string     `json:"name"`
	Description           string     `json:"description"`
	State                 string     `json:"state"`
	Revision              string     `json:"revision"`
	CoverItemID           *uuid.UUID `json:"cover_item_id,omitempty"`
	SortMode              string     `json:"sort_mode"`
	SourceCollectionID    *uuid.UUID `json:"source_collection_id,omitempty"`
	SourceRevision        *int64     `json:"source_revision,omitempty"`
	SourceRunID           *uuid.UUID `json:"source_run_id,omitempty"`
	AppliedStyleVersionID *uuid.UUID `json:"applied_style_version_id,omitempty"`
	CreatedAt             time.Time  `json:"created_at"`
}

type Item struct {
	ID              uuid.UUID       `json:"id"`
	CollectionID    uuid.UUID       `json:"collection_id"`
	AssetID         uuid.UUID       `json:"asset_id"`
	AssetVersionID  uuid.UUID       `json:"asset_version_id"`
	SectionID       *uuid.UUID      `json:"section_id,omitempty"`
	Position        int64           `json:"position"`
	Visibility      string          `json:"visibility"`
	ModerationState string          `json:"moderation_state"`
	Rating          int             `json:"rating"`
	Flag            string          `json:"flag"`
	ColorLabel      *string         `json:"color_label,omitempty"`
	Tags            json.RawMessage `json:"tags"`
	CreatedAt       time.Time       `json:"created_at"`
}

type ItemAdd struct {
	ExpectedRevision int64      `json:"expected_revision"`
	AssetVersionID   uuid.UUID  `json:"asset_version_id"`
	SectionID        *uuid.UUID `json:"section_id,omitempty"`
	Position         *int64     `json:"position,omitempty"`
}

type ItemPatch struct {
	Rating     *int               `json:"rating,omitempty"`
	Flag       *string            `json:"flag,omitempty"`
	ColorLabel OptionalColorLabel `json:"color_label,omitempty"`
	Tags       *[]string          `json:"tags,omitempty"`
	SectionID  OptionalUUID       `json:"section_id,omitempty"`
	Position   *int64             `json:"position,omitempty"`
}

type ItemModeration struct {
	ExpectedRevision int64  `json:"expected_revision"`
	Decision         string `json:"decision"`
}

type Section struct {
	ID           uuid.UUID `json:"id"`
	CollectionID uuid.UUID `json:"collection_id"`
	Name         string    `json:"name"`
	Position     int64     `json:"position"`
	Revision     string    `json:"revision"`
	CreatedAt    time.Time `json:"created_at"`
}

type SectionCreate struct {
	ExpectedRevision int64  `json:"expected_revision"`
	Name             string `json:"name"`
	Position         int64  `json:"position"`
}

type SectionPatch struct {
	Name     *string `json:"name,omitempty"`
	Position *int64  `json:"position,omitempty"`
}

type PublicationRequest struct {
	ExpectedRevision  int64       `json:"expected_revision"`
	ItemIDs           []uuid.UUID `json:"item_ids"`
	Publish           bool        `json:"publish"`
	PublishCollection bool        `json:"publish_collection"`
}

type Create struct {
	ProjectID    uuid.UUID    `json:"project_id"`
	Name         string       `json:"name"`
	Description  string       `json:"description,omitempty"`
	InitialShare *ShareCreate `json:"initial_share,omitempty"`
}

type CollectionCreated struct {
	Collection   Collection    `json:"collection"`
	InitialShare *ShareCreated `json:"initial_share,omitempty"`
}

type DeleteRequest struct {
	ExpectedRevision  int64     `json:"expected_revision"`
	ConfirmResourceID uuid.UUID `json:"confirm_resource_id"`
}

type JobAccepted struct {
	JobID  uuid.UUID `json:"job_id"`
	Status string    `json:"status"`
}

type Patch struct {
	Name        *string      `json:"name,omitempty"`
	Description *string      `json:"description,omitempty"`
	CoverItemID OptionalUUID `json:"cover_item_id,omitempty"`
	SortMode    *string      `json:"sort_mode,omitempty"`
	State       *string      `json:"state,omitempty"`
}

type OptionalUUID struct {
	Set   bool
	Value *uuid.UUID
}

// OptionalColorLabel distinguishes an omitted patch field from an explicit
// null, which clears the label without changing the independent pick flag.
type OptionalColorLabel struct {
	Set   bool
	Value *string
}

func (v *OptionalColorLabel) UnmarshalJSON(raw []byte) error {
	v.Set = true
	if string(raw) == "null" {
		v.Value = nil
		return nil
	}
	var label string
	if err := json.Unmarshal(raw, &label); err != nil || !validColorLabel(label) {
		return errors.New("invalid color label")
	}
	v.Value = &label
	return nil
}

func validColorLabel(label string) bool {
	switch label {
	case "Red", "Yellow", "Green", "Blue", "Purple":
		return true
	default:
		return false
	}
}

func (v *OptionalUUID) UnmarshalJSON(raw []byte) error {
	v.Set = true
	if string(raw) == "null" {
		v.Value = nil
		return nil
	}
	var id uuid.UUID
	if err := json.Unmarshal(raw, &id); err != nil || id == uuid.Nil {
		return errors.New("invalid uuid")
	}
	v.Value = &id
	return nil
}

const fields = `c.id,c.tenant_id,c.project_id,c.name,c.description,c.state,c.revision,c.cover_item_id,c.sort_mode,c.source_collection_id,c.source_revision,c.source_run_id,c.applied_style_version_id,c.created_at`

type scanner interface{ Scan(...any) error }

func scan(row scanner) (Collection, error) {
	var out Collection
	var revision int64
	err := row.Scan(&out.ID, &out.TenantID, &out.ProjectID, &out.Name, &out.Description, &out.State, &revision,
		&out.CoverItemID, &out.SortMode, &out.SourceCollectionID, &out.SourceRevision, &out.SourceRunID, &out.AppliedStyleVersionID, &out.CreatedAt)
	out.Revision = strconv.FormatInt(revision, 10)
	return out, err
}

func valid(name, description, sortMode, state string) bool {
	return strings.TrimSpace(name) != "" && len([]rune(name)) <= 120 && len([]rune(description)) <= 2000 &&
		(sortMode == "manual" || sortMode == "captured_at" || sortMode == "filename") &&
		(state == "draft" || state == "published" || state == "archived")
}

func authorize(ctx context.Context, tx pgx.Tx, who auth.Identity, tid, id uuid.UUID, role, required domain.Role, lock bool) (Collection, error) {
	suffix := ""
	if lock {
		suffix = " FOR UPDATE OF c"
	}
	out, err := scan(tx.QueryRow(ctx, `SELECT `+fields+` FROM photo.collections c WHERE c.tenant_id=$1 AND c.id=$2 AND c.deleted_at IS NULL`+suffix, tid, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return out, domain.NewError("NOT_FOUND", 404)
	}
	if err != nil {
		return out, err
	}
	if _, err = project.Authorize(ctx, tx, tid, out.ProjectID, who.UserID, role, required, false); err != nil {
		return Collection{}, err
	}
	return out, nil
}

func (s Service) Get(ctx context.Context, who auth.Identity, tid, id uuid.UUID) (Collection, error) {
	var out Collection
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		var err error
		out, err = authorize(ctx, tx, who, tid, id, role, domain.Viewer, false)
		return err
	})
	return out, err
}

func (s Service) Create(ctx context.Context, who auth.Identity, tid uuid.UUID, key string, req Create) (idempotency.Response, error) {
	var out idempotency.Response
	if req.ProjectID == uuid.Nil || !valid(req.Name, req.Description, "manual", "draft") {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	now := time.Now().UTC()
	var prepared *preparedShare
	if req.InitialShare != nil {
		if len(s.Pepper) < 32 {
			return out, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
		}
		value, err := prepareShare(*req.InitialShare, now)
		if err != nil {
			return out, err
		}
		prepared = &value
	}
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		if _, err := project.Authorize(ctx, tx, tid, req.ProjectID, who.UserID, role, domain.Editor, false); err != nil {
			return err
		}
		var err error
		out, err = idempotency.Use(ctx, tx, idempotency.Scope{Tenant: tid, Principal: who.UserID, PrincipalType: "user", Route: "Collection.Create", Key: key}, req, now.Add(24*time.Hour), func() (idempotency.Response, error) {
			item, err := scan(tx.QueryRow(ctx, `INSERT INTO photo.collections AS c(tenant_id,project_id,name,description) VALUES($1,$2,$3,$4) RETURNING `+fields,
				tid, req.ProjectID, strings.TrimSpace(req.Name), req.Description))
			if err != nil {
				return idempotency.Response{}, err
			}
			if err = audit(ctx, tx, who, tid, item.ID, "collection.create"); err != nil {
				return idempotency.Response{}, err
			}
			created := CollectionCreated{Collection: item}
			if prepared != nil {
				share, err := s.insertShare(ctx, tx, tid, item.ID, who.UserID, *prepared)
				if err != nil {
					return idempotency.Response{}, err
				}
				item, err = scan(tx.QueryRow(ctx, `UPDATE photo.collections AS c SET state='published',revision=revision+1 WHERE tenant_id=$1 AND id=$2 RETURNING `+fields, tid, item.ID))
				if err != nil {
					return idempotency.Response{}, err
				}
				created.Collection = item
				created.InitialShare = &ShareCreated{Share: share}
			}
			body, err := canonical.Marshal(created)
			return idempotency.Response{Status: 201, Body: body, ResourceID: &item.ID}, err
		})
		if err == nil && !out.Replayed && prepared != nil {
			var created CollectionCreated
			if err = json.Unmarshal(out.Body, &created); err != nil {
				return err
			}
			if created.InitialShare == nil {
				return domain.NewError("INTERNAL_ERROR", 500)
			}
			created.InitialShare.Secret = s.shareSecret(created.InitialShare.ID)
			out.Body, err = canonical.Marshal(created)
		}
		return err
	})
	return out, err
}

func (s Service) Update(ctx context.Context, who auth.Identity, tid, id uuid.UUID, expected int64, patch Patch) (Collection, error) {
	var out Collection
	if expected < 1 {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		current, err := authorize(ctx, tx, who, tid, id, role, domain.Editor, true)
		if err != nil {
			return err
		}
		if current.Revision != strconv.FormatInt(expected, 10) {
			return domain.NewError("REVISION_CONFLICT", 409)
		}
		if current.State == "archived" {
			return domain.NewError("REVISION_CONFLICT", 409)
		}
		if patch.Name != nil {
			current.Name = *patch.Name
		}
		if patch.Description != nil {
			current.Description = *patch.Description
		}
		if patch.SortMode != nil {
			current.SortMode = *patch.SortMode
		}
		if patch.State != nil && *patch.State != current.State {
			return domain.NewError("VALIDATION_FAILED", 422)
		}
		if patch.CoverItemID.Set {
			current.CoverItemID = patch.CoverItemID.Value
		}
		if !valid(current.Name, current.Description, current.SortMode, current.State) {
			return domain.NewError("VALIDATION_FAILED", 422)
		}
		if current.CoverItemID != nil {
			var exists bool
			if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT FROM photo.collection_items WHERE tenant_id=$1 AND collection_id=$2 AND id=$3)`, tid, id, *current.CoverItemID).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				return domain.NewError("NOT_FOUND", 404)
			}
		}
		out, err = scan(tx.QueryRow(ctx, `UPDATE photo.collections AS c SET name=$3,description=$4,cover_item_id=$5,sort_mode=$6,state=$7,revision=revision+1
 WHERE tenant_id=$1 AND id=$2 RETURNING `+fields, tid, id, strings.TrimSpace(current.Name), current.Description, current.CoverItemID, current.SortMode, current.State))
		if err != nil {
			return err
		}
		return audit(ctx, tx, who, tid, id, "collection.update")
	})
	return out, err
}

func (s Service) Archive(ctx context.Context, who auth.Identity, tid, id uuid.UUID, expected int64, req DeleteRequest) (JobAccepted, error) {
	var out JobAccepted
	if expected < 1 || req.ExpectedRevision != expected || req.ConfirmResourceID != id {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		current, err := authorize(ctx, tx, who, tid, id, role, domain.Editor, true)
		if err != nil {
			return err
		}
		if current.Revision != strconv.FormatInt(expected, 10) || current.State == "archived" {
			return domain.NewError("REVISION_CONFLICT", 409)
		}
		if _, err = tx.Exec(ctx, `UPDATE photo.collections SET state='archived',revision=revision+1 WHERE tenant_id=$1 AND id=$2`, tid, id); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE photo.share_links SET revoked_at=coalesce(revoked_at,clock_timestamp()),policy_version=CASE WHEN revoked_at IS NULL THEN policy_version+1 ELSE policy_version END WHERE tenant_id=$1 AND collection_id=$2`, tid, id); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE photo.share_sessions SET revoked_at=coalesce(revoked_at,clock_timestamp()) WHERE tenant_id=$1 AND collection_id=$2`, tid, id); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE photo.download_jobs SET cancel_requested=true WHERE tenant_id=$1 AND collection_id=$2 AND principal_type='guest' AND status NOT IN ('ready','failed','cancelled','expired')`, tid, id); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE photo.upload_sessions SET expires_at=least(expires_at,clock_timestamp()) WHERE tenant_id=$1 AND collection_id=$2 AND principal_type='guest' AND state IN ('initializing','uploading')`, tid, id); err != nil {
			return err
		}
		out = JobAccepted{JobID: uuid.New(), Status: "queued"}
		if _, err = tx.Exec(ctx, `INSERT INTO photo.deletion_jobs(id,tenant_id,resource_type,resource_id,status,requested_by) VALUES($1,$2,'collection',$3,'queued',$4)`, out.JobID, tid, id, who.UserID); err != nil {
			return err
		}
		return audit(ctx, tx, who, tid, id, "collection.archive")
	})
	return out, err
}

func (s Service) List(ctx context.Context, who auth.Identity, tid uuid.UUID, limit int, raw, projectRaw, state, search string) (pagination.Page[Collection], error) {
	out := pagination.Page[Collection]{Items: []Collection{}}
	if limit < 1 || limit > 100 || len([]rune(search)) > 200 || (state != "" && state != "draft" && state != "published" && state != "archived") {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	var projectID *uuid.UUID
	if projectRaw != "" {
		id, err := uuid.Parse(projectRaw)
		if err != nil {
			return out, domain.NewError("VALIDATION_FAILED", 422)
		}
		projectID = &id
	}
	filterHash, err := canonical.Hash(map[string]string{"project_id": projectRaw, "state": state, "search": search})
	if err != nil {
		return out, err
	}
	scope := pagination.Cursor{Tenant: tid.String(), Principal: who.UserID.String(), FilterHash: filterHash, Sort: "collection:created_at,id"}
	var afterID *uuid.UUID
	var afterTime *time.Time
	if raw != "" {
		cursor, err := s.Cursors.Decode(raw, scope)
		if err != nil {
			return out, domain.NewError("INVALID_CURSOR", 400)
		}
		id, err := uuid.Parse(cursor.LastID)
		if err != nil {
			return out, domain.NewError("INVALID_CURSOR", 400)
		}
		position, err := time.Parse(time.RFC3339Nano, cursor.LastPosition)
		if err != nil {
			return out, domain.NewError("INVALID_CURSOR", 400)
		}
		afterID, afterTime = &id, &position
	}
	err = tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		rows, err := tx.Query(ctx, `SELECT `+fields+` FROM photo.collections c JOIN photo.projects p ON p.tenant_id=c.tenant_id AND p.id=c.project_id
 WHERE c.tenant_id=$1 AND c.deleted_at IS NULL AND p.archived_at IS NULL AND ($2::uuid IS NULL OR c.project_id=$2) AND ($3='' OR c.state=$3)
 AND strpos(lower(c.name),lower($4))>0 AND ($5 OR p.visibility='workspace' OR EXISTS(SELECT FROM photo.project_acl a WHERE a.tenant_id=p.tenant_id AND a.project_id=p.id AND a.user_id=$6))
 AND ($7::uuid IS NULL OR (c.created_at,c.id)>($8::timestamptz,$7::uuid)) ORDER BY c.created_at,c.id LIMIT $9`,
			tid, projectID, state, search, role.Allows(domain.Admin), who.UserID, afterID, afterTime, limit+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			item, err := scan(rows)
			if err != nil {
				return err
			}
			out.Items = append(out.Items, item)
		}
		return rows.Err()
	})
	if err != nil {
		return out, err
	}
	if len(out.Items) > limit {
		out.Items = out.Items[:limit]
		last := out.Items[limit-1]
		scope.LastID, scope.LastPosition = last.ID.String(), last.CreatedAt.Format(time.RFC3339Nano)
		cursor, err := s.Cursors.Encode(scope)
		if err != nil {
			return out, err
		}
		out.NextCursor = &cursor
	}
	return out, nil
}

func audit(ctx context.Context, tx pgx.Tx, who auth.Identity, tid, id uuid.UUID, action string) error {
	_, err := tx.Exec(ctx, `INSERT INTO photo.audit_log(tenant_id,actor_type,actor_id,action,resource_type,resource_id,request_id,details)
 VALUES($1,'user',$2,$3,'collection',$4,$5,'{}')`, tid, who.UserID, action, id, uuid.New())
	return err
}

const itemFields = `i.id,i.collection_id,i.asset_id,i.asset_version_id,i.section_id,i.position,i.visibility,i.moderation_state,i.rating,i.flag,i.color_label,i.tags,i.created_at`

func scanItem(row scanner) (Item, error) {
	var out Item
	err := row.Scan(&out.ID, &out.CollectionID, &out.AssetID, &out.AssetVersionID, &out.SectionID, &out.Position,
		&out.Visibility, &out.ModerationState, &out.Rating, &out.Flag, &out.ColorLabel, &out.Tags, &out.CreatedAt)
	return out, err
}

func validTags(tags []string) bool {
	if len(tags) > 100 {
		return false
	}
	seen := make(map[string]bool, len(tags))
	for _, tag := range tags {
		if tag != strings.TrimSpace(tag) || tag == "" || len([]rune(tag)) > 64 || seen[tag] {
			return false
		}
		seen[tag] = true
	}
	return true
}

func verifySection(ctx context.Context, tx pgx.Tx, tid, collectionID uuid.UUID, sectionID *uuid.UUID) error {
	if sectionID == nil {
		return nil
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT FROM photo.sections WHERE tenant_id=$1 AND collection_id=$2 AND id=$3)`, tid, collectionID, *sectionID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return domain.NewError("NOT_FOUND", 404)
	}
	return nil
}

func (s Service) AddItem(ctx context.Context, who auth.Identity, tid, collectionID uuid.UUID, key string, req ItemAdd) (idempotency.Response, error) {
	var out idempotency.Response
	if req.ExpectedRevision < 1 || req.AssetVersionID == uuid.Nil || (req.Position != nil && (*req.Position < 0 || *req.Position > 9007199254740991)) {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		var projectID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT project_id FROM photo.collections WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`, tid, collectionID).Scan(&projectID); errors.Is(err, pgx.ErrNoRows) {
			return domain.NewError("NOT_FOUND", 404)
		} else if err != nil {
			return err
		}
		if _, err := project.Authorize(ctx, tx, tid, projectID, who.UserID, role, domain.Editor, false); err != nil {
			return err
		}
		var err error
		out, err = idempotency.Use(ctx, tx, idempotency.Scope{Tenant: tid, Principal: who.UserID, PrincipalType: "user", Route: "Collection.AddItem", Key: key}, req, time.Now().Add(24*time.Hour), func() (idempotency.Response, error) {
			collection, err := authorize(ctx, tx, who, tid, collectionID, role, domain.Editor, true)
			if err != nil {
				return idempotency.Response{}, err
			}
			if collection.State == "archived" || collection.Revision != strconv.FormatInt(req.ExpectedRevision, 10) {
				return idempotency.Response{}, domain.NewError("REVISION_CONFLICT", 409)
			}
			if err = verifySection(ctx, tx, tid, collectionID, req.SectionID); err != nil {
				return idempotency.Response{}, err
			}
			var assetID uuid.UUID
			err = tx.QueryRow(ctx, `SELECT v.asset_id FROM photo.asset_versions v JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
 JOIN photo.artifacts m ON m.tenant_id=v.tenant_id AND m.id=v.master_artifact_id
 WHERE v.tenant_id=$1 AND v.id=$2 AND a.project_id=$3 AND a.deleted_at IS NULL AND a.upload_state='verified'
 AND m.state='ready' AND (m.expires_at IS NULL OR m.expires_at>clock_timestamp())`, tid, req.AssetVersionID, collection.ProjectID).Scan(&assetID)
			if errors.Is(err, pgx.ErrNoRows) {
				return idempotency.Response{}, domain.NewError("NOT_FOUND", 404)
			}
			if err != nil {
				return idempotency.Response{}, err
			}
			var duplicate bool
			if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT FROM photo.collection_items WHERE tenant_id=$1 AND collection_id=$2 AND asset_id=$3)`, tid, collectionID, assetID).Scan(&duplicate); err != nil {
				return idempotency.Response{}, err
			}
			if duplicate {
				return idempotency.Response{}, domain.NewError("REVISION_CONFLICT", 409)
			}
			position := int64(0)
			if req.Position != nil {
				position = *req.Position
			} else if err = tx.QueryRow(ctx, `SELECT coalesce(max(position)+1,0) FROM photo.collection_items WHERE tenant_id=$1 AND collection_id=$2`, tid, collectionID).Scan(&position); err != nil {
				return idempotency.Response{}, err
			}
			item, err := scanItem(tx.QueryRow(ctx, `INSERT INTO photo.collection_items AS i(tenant_id,collection_id,asset_id,asset_version_id,section_id,position)
 VALUES($1,$2,$3,$4,$5,$6) RETURNING `+itemFields, tid, collectionID, assetID, req.AssetVersionID, req.SectionID, position))
			if err != nil {
				return idempotency.Response{}, err
			}
			if _, err = tx.Exec(ctx, `UPDATE photo.collections SET revision=revision+1 WHERE tenant_id=$1 AND id=$2`, tid, collectionID); err != nil {
				return idempotency.Response{}, err
			}
			if err = audit(ctx, tx, who, tid, collectionID, "collection.item_add"); err != nil {
				return idempotency.Response{}, err
			}
			body, err := canonical.Marshal(item)
			return idempotency.Response{Status: 201, Body: body, ResourceID: &item.ID}, err
		})
		return err
	})
	return out, err
}

func (s Service) UpdateItem(ctx context.Context, who auth.Identity, tid, collectionID, itemID uuid.UUID, expected int64, patch ItemPatch) (Item, error) {
	var out Item
	if expected < 1 {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		collection, err := authorize(ctx, tx, who, tid, collectionID, role, domain.Editor, true)
		if err != nil {
			return err
		}
		if collection.State == "archived" || collection.Revision != strconv.FormatInt(expected, 10) {
			return domain.NewError("REVISION_CONFLICT", 409)
		}
		current, err := scanItem(tx.QueryRow(ctx, `SELECT `+itemFields+` FROM photo.collection_items i WHERE i.tenant_id=$1 AND i.collection_id=$2 AND i.id=$3`, tid, collectionID, itemID))
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.NewError("NOT_FOUND", 404)
		}
		if err != nil {
			return err
		}
		if patch.Rating != nil {
			current.Rating = *patch.Rating
		}
		if patch.Flag != nil {
			current.Flag = *patch.Flag
		}
		if patch.ColorLabel.Set {
			current.ColorLabel = patch.ColorLabel.Value
		}
		if patch.Tags != nil {
			if !validTags(*patch.Tags) {
				return domain.NewError("VALIDATION_FAILED", 422)
			}
			current.Tags, err = canonical.Marshal(*patch.Tags)
			if err != nil {
				return err
			}
		}
		if patch.SectionID.Set {
			current.SectionID = patch.SectionID.Value
		}
		if patch.Position != nil {
			current.Position = *patch.Position
		}
		if current.Rating < 0 || current.Rating > 5 || (current.Flag != "none" && current.Flag != "pick" && current.Flag != "reject") || (current.ColorLabel != nil && !validColorLabel(*current.ColorLabel)) || current.Position < 0 || current.Position > 9007199254740991 {
			return domain.NewError("VALIDATION_FAILED", 422)
		}
		if err = verifySection(ctx, tx, tid, collectionID, current.SectionID); err != nil {
			return err
		}
		out, err = scanItem(tx.QueryRow(ctx, `UPDATE photo.collection_items AS i SET section_id=$4,position=$5,rating=$6,flag=$7,tags=$8,color_label=$9
 WHERE tenant_id=$1 AND collection_id=$2 AND id=$3 RETURNING `+itemFields, tid, collectionID, itemID, current.SectionID, current.Position, current.Rating, current.Flag, current.Tags, current.ColorLabel))
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE photo.collections SET revision=revision+1 WHERE tenant_id=$1 AND id=$2`, tid, collectionID); err != nil {
			return err
		}
		return audit(ctx, tx, who, tid, collectionID, "collection.item_update")
	})
	return out, err
}

// ModerateApplyItem accepts or rejects exactly one immutable version produced
// by the apply run that owns the destination collection. Accepting the version
// also activates the durable media-derivative trigger in migration 00156.
func (s Service) ModerateApplyItem(ctx context.Context, who auth.Identity, tid, collectionID, itemID uuid.UUID, req ItemModeration) (Item, error) {
	var out Item
	if req.ExpectedRevision < 1 || req.Decision != "accept" && req.Decision != "reject" {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	target := map[string]string{"accept": "accepted", "reject": "rejected"}[req.Decision]
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		collection, err := authorize(ctx, tx, who, tid, collectionID, role, domain.Editor, true)
		if err != nil {
			return err
		}
		if collection.State == "archived" || collection.Revision != strconv.FormatInt(req.ExpectedRevision, 10) {
			return domain.NewError("REVISION_CONFLICT", 409)
		}
		if collection.SourceRunID == nil {
			return domain.NewError("NOT_FOUND", 404)
		}
		var versionID uuid.UUID
		var versionState, itemState, runKind string
		err = tx.QueryRow(ctx, `SELECT i.asset_version_id,v.review_state,i.moderation_state,r.kind
 FROM photo.collection_items i JOIN photo.asset_versions v ON v.tenant_id=i.tenant_id AND v.id=i.asset_version_id AND v.asset_id=i.asset_id
 JOIN photo.runs r ON r.tenant_id=i.tenant_id AND r.id=i.created_by_run_id AND r.id=v.created_by_run_id
	 WHERE i.tenant_id=$1 AND i.collection_id=$2 AND i.id=$3 AND r.id=$4 FOR UPDATE OF i,v`, tid, collectionID, itemID, *collection.SourceRunID).Scan(&versionID, &versionState, &itemState, &runKind)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.NewError("NOT_FOUND", 404)
		}
		if err != nil {
			return err
		}
		if runKind != "apply" {
			return domain.NewError("VALIDATION_FAILED", 422)
		}
		if versionState == target && itemState == target {
			out, err = scanItem(tx.QueryRow(ctx, `SELECT `+itemFields+` FROM photo.collection_items i WHERE i.tenant_id=$1 AND i.collection_id=$2 AND i.id=$3`, tid, collectionID, itemID))
			return err
		}
		if versionState != "pending_review" || itemState != "pending_review" {
			return domain.NewError("REVISION_CONFLICT", 409)
		}
		if _, err = tx.Exec(ctx, `UPDATE photo.asset_versions SET review_state=$3 WHERE tenant_id=$1 AND id=$2 AND review_state='pending_review'`, tid, versionID, target); err != nil {
			return err
		}
		out, err = scanItem(tx.QueryRow(ctx, `UPDATE photo.collection_items AS i SET moderation_state=$4,visibility='hidden'
 WHERE tenant_id=$1 AND collection_id=$2 AND id=$3 AND moderation_state='pending_review' RETURNING `+itemFields, tid, collectionID, itemID, target))
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE photo.collections SET revision=revision+1 WHERE tenant_id=$1 AND id=$2`, tid, collectionID); err != nil {
			return err
		}
		return audit(ctx, tx, who, tid, collectionID, "collection.item_"+req.Decision)
	})
	return out, err
}

func (s Service) RemoveItem(ctx context.Context, who auth.Identity, tid, collectionID, itemID uuid.UUID, expected int64) error {
	if expected < 1 {
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	return tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		collection, err := authorize(ctx, tx, who, tid, collectionID, role, domain.Editor, true)
		if err != nil {
			return err
		}
		if collection.State == "archived" || collection.Revision != strconv.FormatInt(expected, 10) {
			return domain.NewError("REVISION_CONFLICT", 409)
		}
		if collection.CoverItemID != nil && *collection.CoverItemID == itemID {
			return domain.NewError("REVISION_CONFLICT", 409)
		}
		tag, err := tx.Exec(ctx, `DELETE FROM photo.collection_items WHERE tenant_id=$1 AND collection_id=$2 AND id=$3`, tid, collectionID, itemID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return domain.NewError("NOT_FOUND", 404)
		}
		if _, err = tx.Exec(ctx, `UPDATE photo.collections SET revision=revision+1 WHERE tenant_id=$1 AND id=$2`, tid, collectionID); err != nil {
			return err
		}
		return audit(ctx, tx, who, tid, collectionID, "collection.item_remove")
	})
}

func (s Service) ListItems(ctx context.Context, who auth.Identity, tid, collectionID uuid.UUID, limit int, raw, sectionRaw, flag, tagFilter string, ratingRaw string) (pagination.Page[Item], error) {
	out := pagination.Page[Item]{Items: []Item{}}
	if limit < 1 || limit > 100 || (flag != "" && flag != "none" && flag != "pick" && flag != "reject") || len([]rune(tagFilter)) > 64 {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	var sectionID *uuid.UUID
	if sectionRaw != "" {
		id, err := uuid.Parse(sectionRaw)
		if err != nil {
			return out, domain.NewError("VALIDATION_FAILED", 422)
		}
		sectionID = &id
	}
	var rating *int
	if ratingRaw != "" {
		value, err := strconv.Atoi(ratingRaw)
		if err != nil || value < 0 || value > 5 {
			return out, domain.NewError("VALIDATION_FAILED", 422)
		}
		rating = &value
	}
	filterHash, err := canonical.Hash(map[string]string{"collection_id": collectionID.String(), "section_id": sectionRaw, "flag": flag, "tag": tagFilter, "rating": ratingRaw})
	if err != nil {
		return out, err
	}
	scope := pagination.Cursor{Tenant: tid.String(), Principal: who.UserID.String(), FilterHash: filterHash, Sort: "item:position,id"}
	var afterID *uuid.UUID
	var afterPosition *int64
	if raw != "" {
		cursor, err := s.Cursors.Decode(raw, scope)
		if err != nil {
			return out, domain.NewError("INVALID_CURSOR", 400)
		}
		id, err := uuid.Parse(cursor.LastID)
		if err != nil {
			return out, domain.NewError("INVALID_CURSOR", 400)
		}
		position, err := strconv.ParseInt(cursor.LastPosition, 10, 64)
		if err != nil {
			return out, domain.NewError("INVALID_CURSOR", 400)
		}
		afterID, afterPosition = &id, &position
	}
	err = tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		collection, err := authorize(ctx, tx, who, tid, collectionID, role, domain.Viewer, false)
		if err != nil {
			return err
		}
		out.Revision = collection.Revision
		rows, err := tx.Query(ctx, `SELECT `+itemFields+` FROM photo.collection_items i WHERE i.tenant_id=$1 AND i.collection_id=$2
 AND ($3::uuid IS NULL OR i.section_id=$3) AND ($4='' OR i.flag=$4) AND ($5::integer IS NULL OR i.rating=$5)
 AND ($6='' OR i.tags ? $6) AND ($7::uuid IS NULL OR (i.position,i.id)>($8::bigint,$7::uuid)) ORDER BY i.position,i.id LIMIT $9`, tid, collectionID, sectionID, flag, rating, tagFilter, afterID, afterPosition, limit+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			item, err := scanItem(rows)
			if err != nil {
				return err
			}
			out.Items = append(out.Items, item)
		}
		return rows.Err()
	})
	if err != nil {
		return out, err
	}
	if len(out.Items) > limit {
		out.Items = out.Items[:limit]
		last := out.Items[limit-1]
		scope.LastID, scope.LastPosition = last.ID.String(), strconv.FormatInt(last.Position, 10)
		cursor, err := s.Cursors.Encode(scope)
		if err != nil {
			return out, err
		}
		out.NextCursor = &cursor
	}
	return out, nil
}

const sectionFields = `s.id,s.collection_id,s.name,s.position,s.revision,s.created_at`

func scanSection(row scanner) (Section, error) {
	var out Section
	var revision int64
	err := row.Scan(&out.ID, &out.CollectionID, &out.Name, &out.Position, &revision, &out.CreatedAt)
	out.Revision = strconv.FormatInt(revision, 10)
	return out, err
}

func validSection(name string, position int64) bool {
	return strings.TrimSpace(name) != "" && len([]rune(name)) <= 120 && position >= 0 && position <= 9007199254740991
}

// currentEditorProject performs the current-policy read required even for an
// idempotent replay. It intentionally takes no domain row lock; Use must own
// the idempotency row before the callback locks the collection.
func currentEditorProject(ctx context.Context, tx pgx.Tx, who auth.Identity, tid, collectionID uuid.UUID, role domain.Role) error {
	var projectID uuid.UUID
	err := tx.QueryRow(ctx, `SELECT project_id FROM photo.collections WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`, tid, collectionID).Scan(&projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.NewError("NOT_FOUND", 404)
	}
	if err != nil {
		return err
	}
	_, err = project.Authorize(ctx, tx, tid, projectID, who.UserID, role, domain.Editor, false)
	return err
}

func (s Service) CreateSection(ctx context.Context, who auth.Identity, tid, collectionID uuid.UUID, key string, req SectionCreate) (idempotency.Response, error) {
	var out idempotency.Response
	if req.ExpectedRevision < 1 || !validSection(req.Name, req.Position) {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		if err := currentEditorProject(ctx, tx, who, tid, collectionID, role); err != nil {
			return err
		}
		var err error
		out, err = idempotency.Use(ctx, tx, idempotency.Scope{Tenant: tid, Principal: who.UserID, PrincipalType: "user", Route: "Collection.CreateSection", Key: key}, req, time.Now().Add(24*time.Hour), func() (idempotency.Response, error) {
			collection, err := authorize(ctx, tx, who, tid, collectionID, role, domain.Editor, true)
			if err != nil {
				return idempotency.Response{}, err
			}
			if collection.State == "archived" || collection.Revision != strconv.FormatInt(req.ExpectedRevision, 10) {
				return idempotency.Response{}, domain.NewError("REVISION_CONFLICT", 409)
			}
			section, err := scanSection(tx.QueryRow(ctx, `INSERT INTO photo.sections AS s(tenant_id,collection_id,name,position) VALUES($1,$2,$3,$4) RETURNING `+sectionFields,
				tid, collectionID, strings.TrimSpace(req.Name), req.Position))
			if err != nil {
				return idempotency.Response{}, err
			}
			if _, err = tx.Exec(ctx, `UPDATE photo.collections SET revision=revision+1 WHERE tenant_id=$1 AND id=$2`, tid, collectionID); err != nil {
				return idempotency.Response{}, err
			}
			if err = audit(ctx, tx, who, tid, collectionID, "collection.section_create"); err != nil {
				return idempotency.Response{}, err
			}
			body, err := canonical.Marshal(section)
			return idempotency.Response{Status: 201, Body: body, ResourceID: &section.ID}, err
		})
		return err
	})
	return out, err
}

func (s Service) UpdateSection(ctx context.Context, who auth.Identity, tid, collectionID, sectionID uuid.UUID, expected int64, patch SectionPatch) (Section, error) {
	var out Section
	if expected < 1 {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		collection, err := authorize(ctx, tx, who, tid, collectionID, role, domain.Editor, true)
		if err != nil {
			return err
		}
		if collection.State == "archived" || collection.Revision != strconv.FormatInt(expected, 10) {
			return domain.NewError("REVISION_CONFLICT", 409)
		}
		current, err := scanSection(tx.QueryRow(ctx, `SELECT `+sectionFields+` FROM photo.sections s WHERE s.tenant_id=$1 AND s.collection_id=$2 AND s.id=$3 FOR UPDATE`, tid, collectionID, sectionID))
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.NewError("NOT_FOUND", 404)
		}
		if err != nil {
			return err
		}
		if patch.Name != nil {
			current.Name = *patch.Name
		}
		if patch.Position != nil {
			current.Position = *patch.Position
		}
		if !validSection(current.Name, current.Position) {
			return domain.NewError("VALIDATION_FAILED", 422)
		}
		out, err = scanSection(tx.QueryRow(ctx, `UPDATE photo.sections AS s SET name=$4,position=$5,revision=revision+1 WHERE tenant_id=$1 AND collection_id=$2 AND id=$3 RETURNING `+sectionFields,
			tid, collectionID, sectionID, strings.TrimSpace(current.Name), current.Position))
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE photo.collections SET revision=revision+1 WHERE tenant_id=$1 AND id=$2`, tid, collectionID); err != nil {
			return err
		}
		return audit(ctx, tx, who, tid, collectionID, "collection.section_update")
	})
	return out, err
}

func (s Service) DeleteSection(ctx context.Context, who auth.Identity, tid, collectionID, sectionID uuid.UUID, expected int64) error {
	if expected < 1 {
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	return tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		collection, err := authorize(ctx, tx, who, tid, collectionID, role, domain.Editor, true)
		if err != nil {
			return err
		}
		if collection.State == "archived" || collection.Revision != strconv.FormatInt(expected, 10) {
			return domain.NewError("REVISION_CONFLICT", 409)
		}
		var found uuid.UUID
		err = tx.QueryRow(ctx, `SELECT id FROM photo.sections WHERE tenant_id=$1 AND collection_id=$2 AND id=$3 FOR UPDATE`, tid, collectionID, sectionID).Scan(&found)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.NewError("NOT_FOUND", 404)
		}
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE photo.collection_items SET section_id=NULL WHERE tenant_id=$1 AND collection_id=$2 AND section_id=$3`, tid, collectionID, sectionID); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `DELETE FROM photo.sections WHERE tenant_id=$1 AND collection_id=$2 AND id=$3`, tid, collectionID, sectionID); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE photo.collections SET revision=revision+1 WHERE tenant_id=$1 AND id=$2`, tid, collectionID); err != nil {
			return err
		}
		return audit(ctx, tx, who, tid, collectionID, "collection.section_delete")
	})
}

func sortedUniqueIDs(ids []uuid.UUID, maximum int) ([]uuid.UUID, bool) {
	if len(ids) < 1 || len(ids) > maximum {
		return nil, false
	}
	out := append([]uuid.UUID(nil), ids...)
	slices.SortFunc(out, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	for index, id := range out {
		if id == uuid.Nil || index > 0 && id == out[index-1] {
			return nil, false
		}
	}
	return out, true
}

func (s Service) Publish(ctx context.Context, who auth.Identity, tid, collectionID uuid.UUID, req PublicationRequest) (Collection, error) {
	var out Collection
	ids, ok := sortedUniqueIDs(req.ItemIDs, 500)
	if req.ExpectedRevision < 1 || !ok {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		collection, err := authorize(ctx, tx, who, tid, collectionID, role, domain.Editor, true)
		if err != nil {
			return err
		}
		if collection.State == "archived" || collection.Revision != strconv.FormatInt(req.ExpectedRevision, 10) {
			return domain.NewError("REVISION_CONFLICT", 409)
		}
		rows, err := tx.Query(ctx, `SELECT i.id,i.asset_version_id,a.upload_state,v.review_state,m.state,(m.expires_at IS NULL OR m.expires_at>clock_timestamp())
 FROM photo.collection_items i JOIN photo.assets a ON a.tenant_id=i.tenant_id AND a.id=i.asset_id
 JOIN photo.asset_versions v ON v.tenant_id=i.tenant_id AND v.id=i.asset_version_id AND v.asset_id=i.asset_id
 JOIN photo.artifacts m ON m.tenant_id=v.tenant_id AND m.id=v.master_artifact_id
 WHERE i.tenant_id=$1 AND i.collection_id=$2 AND i.id=ANY($3::uuid[]) ORDER BY i.id FOR UPDATE OF i`, tid, collectionID, ids)
		if err != nil {
			return err
		}
		type selected struct{ id, version uuid.UUID }
		items := make([]selected, 0, len(ids))
		for rows.Next() {
			var item selected
			var upload, reviewState, artifactState string
			var alive bool
			if err = rows.Scan(&item.id, &item.version, &upload, &reviewState, &artifactState, &alive); err != nil {
				rows.Close()
				return err
			}
			if req.Publish && (upload != "verified" || reviewState != "accepted" || artifactState != "ready" || !alive) {
				rows.Close()
				return domain.NewError("REVISION_CONFLICT", 409)
			}
			items = append(items, item)
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		if len(items) != len(ids) {
			return domain.NewError("NOT_FOUND", 404)
		}
		state, visibility := "revoked", "hidden"
		if req.Publish {
			state, visibility = "published", "visible"
		}
		for _, item := range items {
			_, err = tx.Exec(ctx, `INSERT INTO photo.publications(tenant_id,item_id,asset_version_id,state,published_by,published_at)
 VALUES($1,$2,$3,$4,$5,clock_timestamp()) ON CONFLICT(tenant_id,item_id) DO UPDATE SET state=EXCLUDED.state,published_by=EXCLUDED.published_by,published_at=EXCLUDED.published_at,policy_version=photo.publications.policy_version+1`,
				tid, item.id, item.version, state, who.UserID)
			if err != nil {
				return err
			}
		}
		if _, err = tx.Exec(ctx, `UPDATE photo.collection_items SET visibility=$4 WHERE tenant_id=$1 AND collection_id=$2 AND id=ANY($3::uuid[])`, tid, collectionID, ids, visibility); err != nil {
			return err
		}
		newState := collection.State
		if req.PublishCollection {
			if req.Publish {
				newState = "published"
			} else {
				newState = "draft"
			}
		}
		out, err = scan(tx.QueryRow(ctx, `UPDATE photo.collections AS c SET state=$3,revision=revision+1 WHERE tenant_id=$1 AND id=$2 RETURNING `+fields, tid, collectionID, newState))
		if err != nil {
			return err
		}
		return audit(ctx, tx, who, tid, collectionID, "collection.publish")
	})
	return out, err
}
