package collection

import (
	"bytes"
	"context"
	"encoding/json"
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
	"photoeditor/backend/internal/pagination"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/tenant"
)

type BulkRequest struct {
	ExpectedRevision    int64           `json:"expected_revision"`
	SelectionSnapshotID uuid.UUID       `json:"selection_snapshot_id"`
	Action              string          `json:"action"`
	Params              json.RawMessage `json:"params"`
}
type BulkAccepted struct {
	BulkOperationID uuid.UUID `json:"bulk_operation_id"`
	RunID           uuid.UUID `json:"run_id"`
	Status          string    `json:"status"`
}
type BulkCounts struct {
	Total     int64 `json:"total"`
	Succeeded int64 `json:"succeeded"`
	Failed    int64 `json:"failed"`
	Skipped   int64 `json:"skipped"`
}
type BulkItemResult struct {
	ItemID    uuid.UUID       `json:"item_id"`
	Ordinal   int64           `json:"ordinal"`
	Status    string          `json:"status"`
	ErrorCode *string         `json:"error_code,omitempty"`
	Result    json.RawMessage `json:"result"`
}
type BulkStatus struct {
	ID                   uuid.UUID                       `json:"id"`
	CollectionID         uuid.UUID                       `json:"collection_id"`
	SnapshotID           uuid.UUID                       `json:"snapshot_id"`
	RunID                uuid.UUID                       `json:"run_id"`
	Action               string                          `json:"action"`
	Status               string                          `json:"status"`
	CancelRequested      bool                            `json:"cancel_requested"`
	ExpectedNextRevision string                          `json:"expected_next_revision"`
	Counts               BulkCounts                      `json:"counts"`
	Results              pagination.Page[BulkItemResult] `json:"results"`
}

func strictParams(raw json.RawMessage, out any) error {
	if len(raw) == 0 || len(raw) > 65536 {
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	canonicalRaw, err := canonical.Bytes(raw)
	if err != nil {
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	d := json.NewDecoder(bytes.NewReader(canonicalRaw))
	d.DisallowUnknownFields()
	if err = d.Decode(out); err != nil {
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	round, err := canonical.Marshal(out)
	if err != nil || !bytes.Equal(round, canonicalRaw) {
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	return nil
}
func normalizeBulk(req BulkRequest) (BulkRequest, *uuid.UUID, error) {
	req.Action = strings.ToLower(strings.TrimSpace(req.Action))
	if req.ExpectedRevision < 1 || req.SelectionSnapshotID == uuid.Nil {
		return req, nil, domain.NewError("VALIDATION_FAILED", 422)
	}
	var target *uuid.UUID
	switch req.Action {
	case "rating":
		var p struct {
			Rating int `json:"rating"`
		}
		if strictParams(req.Params, &p) != nil || p.Rating < 0 || p.Rating > 5 {
			return req, nil, domain.NewError("VALIDATION_FAILED", 422)
		}
	case "flag":
		var p struct {
			Flag string `json:"flag"`
		}
		if strictParams(req.Params, &p) != nil || (p.Flag != "none" && p.Flag != "pick" && p.Flag != "reject") {
			return req, nil, domain.NewError("VALIDATION_FAILED", 422)
		}
	case "tag":
		var p struct {
			Tag  string `json:"tag"`
			Mode string `json:"mode"`
		}
		if strictParams(req.Params, &p) != nil || p.Tag != strings.TrimSpace(p.Tag) || p.Tag == "" || len([]rune(p.Tag)) > 64 || (p.Mode != "add" && p.Mode != "remove") {
			return req, nil, domain.NewError("VALIDATION_FAILED", 422)
		}
	case "hide", "remove":
		var p struct{}
		if strictParams(req.Params, &p) != nil {
			return req, nil, domain.NewError("VALIDATION_FAILED", 422)
		}
	case "publish":
		var p struct {
			Publish bool `json:"publish"`
		}
		if strictParams(req.Params, &p) != nil {
			return req, nil, domain.NewError("VALIDATION_FAILED", 422)
		}
	case "copy", "move":
		var p struct {
			TargetCollectionID uuid.UUID `json:"target_collection_id"`
		}
		if strictParams(req.Params, &p) != nil || p.TargetCollectionID == uuid.Nil {
			return req, nil, domain.NewError("VALIDATION_FAILED", 422)
		}
		target = &p.TargetCollectionID
	case "retry_media":
		var p struct {
			Variants []string `json:"variants"`
		}
		if strictParams(req.Params, &p) != nil || len(p.Variants) < 1 || len(p.Variants) > 4 {
			return req, nil, domain.NewError("VALIDATION_FAILED", 422)
		}
		seen := map[string]bool{}
		for _, v := range p.Variants {
			if (v != "thumbnail" && v != "preview") || seen[v] {
				return req, nil, domain.NewError("VALIDATION_FAILED", 422)
			}
			seen[v] = true
		}
	default:
		return req, nil, domain.NewError("VALIDATION_FAILED", 422)
	}
	bytes, err := canonical.Bytes(req.Params)
	if err != nil {
		return req, nil, domain.NewError("VALIDATION_FAILED", 422)
	}
	req.Params = bytes
	return req, target, nil
}

func lockBulkCollections(ctx context.Context, tx pgx.Tx, who auth.Identity, tid, source uuid.UUID, target *uuid.UUID, role domain.Role) (Collection, error) {
	ids := []uuid.UUID{source}
	if target != nil && *target != source {
		ids = append(ids, *target)
	}
	sortUUIDs(ids)
	rows, err := tx.Query(ctx, `SELECT id FROM photo.collections WHERE tenant_id=$1 AND id=ANY($2::uuid[]) AND deleted_at IS NULL ORDER BY id FOR UPDATE`, tid, ids)
	if err != nil {
		return Collection{}, err
	}
	count := 0
	for rows.Next() {
		count++
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return Collection{}, err
	}
	rows.Close()
	if count != len(ids) {
		return Collection{}, domain.NewError("NOT_FOUND", 404)
	}
	sourceCollection, err := authorize(ctx, tx, who, tid, source, role, domain.Editor, false)
	if err != nil {
		return Collection{}, err
	}
	if target != nil {
		targetCollection, err := authorize(ctx, tx, who, tid, *target, role, domain.Editor, false)
		if err != nil {
			return Collection{}, err
		}
		if targetCollection.ProjectID != sourceCollection.ProjectID || !writableCollectionState(targetCollection.State) {
			return Collection{}, domain.NewError("VALIDATION_FAILED", 422)
		}
	}
	return sourceCollection, nil
}
func sortUUIDs(ids []uuid.UUID) {
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && bytes.Compare(ids[j][:], ids[j-1][:]) < 0; j-- {
			ids[j], ids[j-1] = ids[j-1], ids[j]
		}
	}
}

func (s Service) StartBulk(ctx context.Context, who auth.Identity, tid, collectionID uuid.UUID, key string, raw BulkRequest) (idempotency.Response, error) {
	var out idempotency.Response
	req, target, err := normalizeBulk(raw)
	if err != nil {
		return out, err
	}
	if target != nil && *target == collectionID {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	now := time.Now().UTC()
	err = tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		if err := currentEditorProject(ctx, tx, who, tid, collectionID, role); err != nil {
			return err
		}
		if target != nil {
			var pid uuid.UUID
			e := tx.QueryRow(ctx, `SELECT project_id FROM photo.collections WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`, tid, *target).Scan(&pid)
			if errors.Is(e, pgx.ErrNoRows) {
				return domain.NewError("NOT_FOUND", 404)
			}
			if e != nil {
				return e
			}
			if _, e = project.Authorize(ctx, tx, tid, pid, who.UserID, role, domain.Editor, false); e != nil {
				return e
			}
		}
		out, err = idempotency.Use(ctx, tx, idempotency.Scope{Tenant: tid, Principal: who.UserID, PrincipalType: "user", Route: "Bulk.Start", Key: key}, req, now.Add(30*24*time.Hour), func() (idempotency.Response, error) {
			collection, e := lockBulkCollections(ctx, tx, who, tid, collectionID, target, role)
			if e != nil {
				return idempotency.Response{}, e
			}
			if !writableCollectionState(collection.State) || collection.Revision != strconv.FormatInt(req.ExpectedRevision, 10) {
				return idempotency.Response{}, domain.NewError("REVISION_CONFLICT", 409)
			}
			var snapshotCount int64
			var digest string
			e = tx.QueryRow(ctx, `SELECT item_count,digest FROM photo.snapshots WHERE tenant_id=$1 AND id=$2 AND kind='selection' AND collection_id=$3 AND source_revision=$4 AND principal_type='user' AND principal_id=$5 AND expires_at>clock_timestamp()`, tid, req.SelectionSnapshotID, collectionID, req.ExpectedRevision, who.UserID).Scan(&snapshotCount, &digest)
			if errors.Is(e, pgx.ErrNoRows) {
				return idempotency.Response{}, domain.NewError("SNAPSHOT_STALE", 409)
			}
			if e != nil {
				return idempotency.Response{}, e
			}
			if snapshotCount < 1 || snapshotCount > 5000 {
				return idempotency.Response{}, domain.NewError("VALIDATION_FAILED", 422)
			}
			config := map[string]any{"schema_version": 1, "pipeline": "bulk", "pipeline_version": 3, "source_collection_id": collectionID, "source_revision": req.ExpectedRevision, "input_snapshot_id": req.SelectionSnapshotID, "input_snapshot_digest": digest, "action": req.Action, "params": json.RawMessage(req.Params)}
			configBytes, e := canonical.Marshal(config)
			if e != nil {
				return idempotency.Response{}, e
			}
			configHash, e := canonical.Hash(config)
			if e != nil {
				return idempotency.Response{}, e
			}
			runID, bulkID := uuid.New(), uuid.New()
			var targetRevision *int64
			if target != nil {
				var value int64
				if e = tx.QueryRow(ctx, `SELECT revision FROM photo.collections WHERE tenant_id=$1 AND id=$2`, tid, *target).Scan(&value); e != nil {
					return idempotency.Response{}, e
				}
				targetRevision = &value
			}
			_, e = tx.Exec(ctx, `INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) VALUES($1,$2,'bulk','queued',$3,$4,$5,$6,false)`, runID, tid, req.SelectionSnapshotID, configBytes, configHash, who.UserID)
			if e != nil {
				return idempotency.Response{}, e
			}
			_, e = tx.Exec(ctx, `INSERT INTO photo.bulk_operations(id,tenant_id,collection_id,snapshot_id,action,params,expected_next_revision,status,created_by,run_id,target_collection_id,target_expected_next_revision) VALUES($1,$2,$3,$4,$5,$6,$7,'queued',$8,$9,$10,$11)`, bulkID, tid, collectionID, req.SelectionSnapshotID, req.Action, req.Params, req.ExpectedRevision, who.UserID, runID, target, targetRevision)
			if e != nil {
				return idempotency.Response{}, e
			}
			if e = audit(ctx, tx, who, tid, collectionID, "bulk.start"); e != nil {
				return idempotency.Response{}, e
			}
			accepted := BulkAccepted{bulkID, runID, "queued"}
			body, e := canonical.Marshal(accepted)
			return idempotency.Response{Status: 202, Body: body, ResourceID: &bulkID}, e
		})
		if err != nil { return err }
		if isBulkAbandoned(out, collectionID, key) {
            if err = bindBulkAbandoned(ctx, tx, who, tid, collectionID, "Bulk.Start", key, out); err != nil { return err }
            return domain.NewError("BULK_ABANDONED", 409)
        }
		if err = bindCollectionReceipt(ctx, tx, who, tid, collectionID, "Bulk.Start", key, out, `SELECT EXISTS(SELECT 1 FROM photo.bulk_operations WHERE tenant_id=$1 AND id=$2 AND collection_id=$3)`); err != nil { return err }
		if err != nil { return err }
		currentRole, err := tenant.AuthorizeInTransaction(ctx, tx, who, tid, domain.Editor)
		if err != nil { return err }
		if err = currentEditorProject(ctx, tx, who, tid, collectionID, currentRole); err != nil { return err }
		if target != nil { return currentEditorProject(ctx, tx, who, tid, *target, currentRole) }
		return nil
	})
	return out, err
}

func (s Service) GetBulk(ctx context.Context, who auth.Identity, tid, bulkID uuid.UUID, limit int, raw string) (BulkStatus, error) {
	var out BulkStatus
	if limit < 1 || limit > 100 {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		var revision int64
		e := tx.QueryRow(ctx, `SELECT id,collection_id,snapshot_id,run_id,action,status,cancel_requested,expected_next_revision FROM photo.bulk_operations WHERE tenant_id=$1 AND id=$2`, tid, bulkID).Scan(&out.ID, &out.CollectionID, &out.SnapshotID, &out.RunID, &out.Action, &out.Status, &out.CancelRequested, &revision)
		if errors.Is(e, pgx.ErrNoRows) {
			return domain.NewError("NOT_FOUND", 404)
		}
		if e != nil {
			return e
		}
		out.ExpectedNextRevision = strconv.FormatInt(revision, 10)
		if _, e = authorize(ctx, tx, who, tid, out.CollectionID, role, domain.Viewer, false); e != nil {
			return e
		}
		if e = tx.QueryRow(ctx, `SELECT s.item_count,count(r.id) FILTER(WHERE r.status='succeeded'),count(r.id) FILTER(WHERE r.status='failed'),count(r.id) FILTER(WHERE r.status='skipped') FROM photo.snapshots s LEFT JOIN photo.bulk_item_results r ON r.tenant_id=s.tenant_id AND r.bulk_operation_id=$3 WHERE s.tenant_id=$1 AND s.id=$2 GROUP BY s.item_count`, tid, out.SnapshotID, bulkID).Scan(&out.Counts.Total, &out.Counts.Succeeded, &out.Counts.Failed, &out.Counts.Skipped); e != nil {
			return e
		}
		out.Results = pagination.Page[BulkItemResult]{Items: []BulkItemResult{}}
		var after *int64
		if raw != "" {
			scope := pagination.Cursor{Tenant: tid.String(), Principal: who.UserID.String(), FilterHash: bulkID.String(), Sort: "bulk-result:ordinal"}
			c, e := s.Cursors.Decode(raw, scope)
			if e != nil {
				return domain.NewError("INVALID_CURSOR", 400)
			}
			value, e := strconv.ParseInt(c.LastPosition, 10, 64)
			if e != nil {
				return domain.NewError("INVALID_CURSOR", 400)
			}
			after = &value
		}
		rows, e := tx.Query(ctx, `SELECT r.snapshot_ordinal,si.item_id,r.status,r.error_code,r.result
 FROM photo.bulk_item_results r
 LEFT JOIN photo.snapshot_items si ON si.tenant_id=r.tenant_id
  AND si.snapshot_id=$5 AND si.ordinal=r.snapshot_ordinal
 WHERE r.tenant_id=$1 AND r.bulk_operation_id=$2
  AND ($3::bigint IS NULL OR r.snapshot_ordinal>$3)
 ORDER BY r.snapshot_ordinal LIMIT $4`, tid, bulkID, after, limit+1, out.SnapshotID)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var item BulkItemResult
			if e = rows.Scan(&item.Ordinal, &item.ItemID, &item.Status, &item.ErrorCode, &item.Result); e != nil {
				return e
			}
			if item.ItemID == uuid.Nil {
				return domain.NewError("BULK_RESULT_SOURCE_INVALID", 409)
			}
			out.Results.Items = append(out.Results.Items, item)
		}
		return rows.Err()
	})
	if err != nil {
		return out, err
	}
	if len(out.Results.Items) > limit {
		out.Results.Items = out.Results.Items[:limit]
		last := out.Results.Items[limit-1]
		scope := pagination.Cursor{Tenant: tid.String(), Principal: who.UserID.String(), FilterHash: bulkID.String(), Sort: "bulk-result:ordinal", LastPosition: strconv.FormatInt(last.Ordinal, 10), LastID: bulkID.String()}
		cursor, e := s.Cursors.Encode(scope)
		if e != nil {
			return out, e
		}
		out.Results.NextCursor = &cursor
	}
	return out, nil
}

func (s Service) CancelBulk(ctx context.Context, who auth.Identity, tid, bulkID uuid.UUID) (BulkStatus, error) {
	var collectionID uuid.UUID
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		var runID uuid.UUID
		e := tx.QueryRow(ctx, `SELECT collection_id,run_id FROM photo.bulk_operations WHERE tenant_id=$1 AND id=$2`, tid, bulkID).Scan(&collectionID, &runID)
		if errors.Is(e, pgx.ErrNoRows) {
			return domain.NewError("NOT_FOUND", 404)
		}
		if e != nil {
			return e
		}
		if _, e = authorize(ctx, tx, who, tid, collectionID, role, domain.Editor, false); e != nil {
			return e
		}
		var runStatus string
		if e = tx.QueryRow(ctx, `SELECT status FROM photo.runs WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tid, runID).Scan(&runStatus); e != nil {
			return e
		}
		var status string
		if e = tx.QueryRow(ctx, `SELECT status FROM photo.bulk_operations WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tid, bulkID).Scan(&status); e != nil {
			return e
		}
		// Locks may wait while membership or project access is withdrawn.
		currentRole, authorityErr := tenant.AuthorizeInTransaction(ctx, tx, who, tid, domain.Editor)
		if authorityErr != nil { return authorityErr }
		if authorityErr = currentEditorProject(ctx, tx, who, tid, collectionID, currentRole); authorityErr != nil { return authorityErr }
		switch status {
		case "completed", "completed_with_errors", "failed", "cancelled":
			return nil
		case "queued":
			status = "cancelled"
			_, e = tx.Exec(ctx, `UPDATE photo.runs SET status='cancelled',cancel_requested=true,revision=revision+1 WHERE tenant_id=$1 AND id=$2`, tid, runID)
		case "running", "cancelling":
			status = "cancelling"
			_, e = tx.Exec(ctx, `UPDATE photo.runs SET status='cancelling',cancel_requested=true,revision=revision+1 WHERE tenant_id=$1 AND id=$2 AND status NOT IN ('cancelled','completed','completed_with_errors','failed')`, tid, runID)
		default:
			return domain.NewError("REVISION_CONFLICT", 409)
		}
		if e != nil {
			return e
		}
		_, e = tx.Exec(ctx, `UPDATE photo.bulk_operations SET status=$3,cancel_requested=true WHERE tenant_id=$1 AND id=$2`, tid, bulkID, status)
		if e != nil {
			return e
		}
		return audit(ctx, tx, who, tid, collectionID, "bulk.cancel")
	})
	if err != nil {
		return BulkStatus{}, err
	}
	return s.GetBulk(ctx, who, tid, bulkID, 50, "")
}
