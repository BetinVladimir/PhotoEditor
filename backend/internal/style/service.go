package style

import (
	"context"
	"encoding/json"
	"errors"
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
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/pagination"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/tenant"
)

type Service struct {
	DB      *pgxpool.Pool
	Cursors pagination.Codec
	Objects DownloadSigner
	Bucket  string
}
type DownloadSigner interface {
	SignDownload(context.Context, objectstore.Object, string, time.Duration) (objectstore.ReadURL, error)
}
type Style struct {
	ID          uuid.UUID `json:"id"`
	TenantID    uuid.UUID `json:"tenant_id"`
	ProjectID   uuid.UUID `json:"project_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Revision    string    `json:"revision"`
	Archived    bool      `json:"archived"`
	CreatedAt   time.Time `json:"created_at"`
}
type Create struct {
	ProjectID   uuid.UUID `json:"project_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
}
type Patch struct {
	ExpectedRevision int64   `json:"expected_revision"`
	Name             *string `json:"name,omitempty"`
	Description      *string `json:"description,omitempty"`
	Archived         *bool   `json:"archived,omitempty"`
}
type TrainingSet struct {
	ID        uuid.UUID                     `json:"id"`
	StyleID   uuid.UUID                     `json:"style_id"`
	Revision  string                        `json:"revision"`
	State     string                        `json:"state"`
	CreatedAt time.Time                     `json:"created_at"`
	Pairs     pagination.Page[TrainingPair] `json:"pairs"`
}
type PairCreate struct {
	BeforeAssetVersionID uuid.UUID  `json:"before_asset_version_id"`
	AfterAssetVersionID  uuid.UUID  `json:"after_asset_version_id"`
	SessionID            string     `json:"session_id"`
	Confirmed            bool       `json:"confirmed"`
	Included             bool       `json:"included"`
	IgnoreMaskArtifactID *uuid.UUID `json:"ignore_mask_artifact_id,omitempty"`
}
type FeedbackRequest struct {
	TrainingSetID        uuid.UUID `json:"training_set_id"`
	ExpectedRevision     int64     `json:"expected_revision"`
	SourceAssetVersionID uuid.UUID `json:"source_asset_version_id"`
	ResultAssetVersionID uuid.UUID `json:"result_asset_version_id"`
	SessionID            string    `json:"session_id"`
	Confirmed            bool      `json:"confirmed"`
}
type PairPatch struct {
	ExpectedRevision     int64        `json:"expected_revision"`
	Confirmed            *bool        `json:"confirmed,omitempty"`
	Included             *bool        `json:"included,omitempty"`
	IgnoreMaskArtifactID OptionalUUID `json:"ignore_mask_artifact_id,omitempty"`
}
type OptionalUUID struct {
	Set   bool
	Value *uuid.UUID
}

func (v *OptionalUUID) UnmarshalJSON(raw []byte) error {
	v.Set = true
	if string(raw) == "null" {
		v.Value = nil
		return nil
	}
	var id uuid.UUID
	if err := json.Unmarshal(raw, &id); err != nil {
		return err
	}
	if id == uuid.Nil {
		return errors.New("nil uuid")
	}
	v.Value = &id
	return nil
}

type TrainingPair struct {
	ID                   uuid.UUID  `json:"id"`
	TrainingSetID        uuid.UUID  `json:"training_set_id"`
	BeforeAssetVersionID uuid.UUID  `json:"before_asset_version_id"`
	AfterAssetVersionID  uuid.UUID  `json:"after_asset_version_id"`
	SessionID            string     `json:"session_id"`
	Confirmed            bool       `json:"confirmed"`
	Included             bool       `json:"included"`
	ValidationStatus     string     `json:"validation_status"`
	IgnoreMaskArtifactID *uuid.UUID `json:"ignore_mask_artifact_id,omitempty"`
	Revision             string     `json:"revision"`
	CreatedAt            time.Time  `json:"created_at"`
}
type Version struct {
	ID               uuid.UUID       `json:"id"`
	StyleID          uuid.UUID       `json:"style_id"`
	VersionNo        int             `json:"version_no"`
	Config           json.RawMessage `json:"config"`
	ConfigHash       string          `json:"config_hash"`
	SchemaVersion    string          `json:"schema_version"`
	RendererVersion  string          `json:"renderer_version"`
	TrainingRunID    *uuid.UUID      `json:"training_run_id,omitempty"`
	ApprovalStatus   string          `json:"approval_status"`
	Metrics          json.RawMessage `json:"metrics"`
	ExportArtifactID *uuid.UUID      `json:"export_artifact_id,omitempty"`
	SourceSHA256     *string         `json:"source_file_sha256,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
}
type ArtifactDownload struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
	Filename  string    `json:"filename"`
	Bytes     int64     `json:"bytes"`
	SHA256    string    `json:"sha256"`
}
type AcceptStyleRequest struct {
	CandidateRunID           uuid.UUID `json:"candidate_run_id"`
	ExpectedStyleRevision    int64     `json:"expected_style_revision"`
	Decision                 string    `json:"decision"`
	AcknowledgeLowConfidence bool      `json:"acknowledge_low_confidence"`
}
type AnalysisOptions struct {
	AlignmentMode   string `json:"alignment_mode"`
	GeometryEnabled bool   `json:"geometry_enabled"`
	Seed            uint32 `json:"seed"`
}
type AnalyzeRequest struct {
	TrainingSetID       uuid.UUID       `json:"training_set_id"`
	TrainingSetRevision int64           `json:"training_set_revision"`
	AnalysisOptions     AnalysisOptions `json:"analysis_options"`
	HoldoutSnapshotID   *uuid.UUID      `json:"holdout_snapshot_id,omitempty"`
}
type RunAccepted struct {
	RunID  uuid.UUID `json:"run_id"`
	Status string    `json:"status"`
}
type HoldoutSnapshotRequest struct {
	ExpectedRevision int64 `json:"expected_revision"`
}
type HoldoutSnapshot struct {
	SnapshotID     uuid.UUID `json:"snapshot_id"`
	Digest         string    `json:"digest"`
	ItemCount      int       `json:"item_count"`
	SourceRevision int64     `json:"source_revision"`
}
type ImportRequest struct {
	ProjectID                uuid.UUID  `json:"project_id"`
	Name                     string     `json:"name"`
	UploadArtifactID         uuid.UUID  `json:"upload_artifact_id"`
	AssetsManifestArtifactID *uuid.UUID `json:"assets_manifest_artifact_id,omitempty"`
}
type analysisPairSnapshot struct {
	PairID     uuid.UUID    `json:"pair_id"`
	SessionID  string       `json:"session_id"`
	Before     artifactRef  `json:"before"`
	After      artifactRef  `json:"after"`
	IgnoreMask *artifactRef `json:"ignore_mask,omitempty"`
}
type artifactRef struct {
	ArtifactID uuid.UUID `json:"artifact_id"`
	SHA256     string    `json:"sha256"`
	Bucket     string    `json:"bucket"`
	Key        string    `json:"key"`
	VersionID  string    `json:"version_id"`
	Bytes      int64     `json:"bytes"`
	MediaType  string    `json:"media_type"`
}

func (a artifactRef) valid() bool {
	_, e := domain.ParseDigest(a.SHA256)
	return a.ArtifactID != uuid.Nil && e == nil && a.Bucket != "" && a.Key != "" && a.VersionID != "" && a.VersionID != "null" && a.Bytes > 1 && a.MediaType == "application/json"
}

func validateHoldoutSnapshot(ctx context.Context, tx pgx.Tx, tid, projectID, snapshotID uuid.UUID, trainingSessions map[string]bool) (string, error) {
	var declared int
	var digest string
	if err := tx.QueryRow(ctx, `SELECT item_count,digest FROM photo.snapshots WHERE tenant_id=$1 AND id=$2 AND kind='holdout' AND (expires_at IS NULL OR expires_at>clock_timestamp())`, tid, snapshotID).Scan(&declared, &digest); errors.Is(err, pgx.ErrNoRows) {
		return "", domain.NewError("NOT_FOUND", 404)
	} else if err != nil {
		return "", err
	}
	if declared < 1 || declared > 64 {
		return "", domain.NewError("HOLDOUT_PAIR_COUNT_INVALID", 422)
	}
	rows, err := tx.Query(ctx, `SELECT ordinal,item_id,sha256,payload FROM photo.snapshot_items WHERE tenant_id=$1 AND snapshot_id=$2 ORDER BY ordinal`, tid, snapshotID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	pairs := make([]analysisPairSnapshot, 0, declared)
	seenPair := map[uuid.UUID]bool{}
	seenSession := map[string]bool{}
	for rows.Next() {
		var ordinal int
		var item uuid.UUID
		var itemHash string
		var raw []byte
		if err = rows.Scan(&ordinal, &item, &itemHash, &raw); err != nil {
			return "", err
		}
		var p analysisPairSnapshot
		if json.Unmarshal(raw, &p) != nil || ordinal != len(pairs) || p.PairID != item || seenPair[p.PairID] || p.SessionID == "" || seenSession[p.SessionID] || trainingSessions[p.SessionID] || !p.Before.valid() || !p.After.valid() {
			return "", domain.NewError("HOLDOUT_SNAPSHOT_INVALID", 422)
		}
		actual, e := canonical.Hash(p)
		if e != nil || actual != itemHash {
			return "", domain.NewError("HOLDOUT_SNAPSHOT_INVALID", 422)
		}
		seenPair[p.PairID] = true
		seenSession[p.SessionID] = true
		pairs = append(pairs, p)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return "", err
	}
	rows.Close()
	for _, p := range pairs {
		for _, a := range []artifactRef{p.Before, p.After} {
			var ok bool
			e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT FROM photo.artifacts ar JOIN photo.asset_versions v ON v.tenant_id=ar.tenant_id AND v.master_artifact_id=ar.id JOIN photo.assets x ON x.tenant_id=v.tenant_id AND x.id=v.asset_id WHERE ar.tenant_id=$1 AND ar.id=$2 AND ar.sha256=$3 AND ar.bucket=$4 AND ar.object_key=$5 AND ar.object_version=$6 AND ar.bytes=$7 AND ar.media_type=$8 AND ar.state='ready' AND (ar.expires_at IS NULL OR ar.expires_at>clock_timestamp()) AND x.project_id=$9 AND x.upload_state='verified' AND x.deleted_at IS NULL)`, tid, a.ArtifactID, a.SHA256, a.Bucket, a.Key, a.VersionID, a.Bytes, a.MediaType, projectID).Scan(&ok)
			if e != nil {
				return "", e
			}
			if !ok {
				return "", domain.NewError("HOLDOUT_ARTIFACT_INVALID", 409)
			}
		}
	}
	actual, e := canonical.Hash(pairs)
	if e != nil || len(pairs) != declared || actual != digest {
		return "", domain.NewError("HOLDOUT_SNAPSHOT_INVALID", 422)
	}
	return digest, nil
}

const versionFields = `v.id,v.style_id,v.version_no,v.config_json,v.config_hash,v.schema_version,v.renderer_version,v.training_run_id,v.approval_status,v.metrics,v.export_artifact_id,v.source_file_sha256,v.created_at`

func scanVersion(row pgx.Row) (Version, error) {
	var out Version
	err := row.Scan(&out.ID, &out.StyleID, &out.VersionNo, &out.Config, &out.ConfigHash, &out.SchemaVersion, &out.RendererVersion, &out.TrainingRunID, &out.ApprovalStatus, &out.Metrics, &out.ExportArtifactID, &out.SourceSHA256, &out.CreatedAt)
	return out, err
}

const styleFields = `s.id,s.tenant_id,s.project_id,s.name,s.description,s.revision,s.archived_at IS NOT NULL,s.created_at`

func scanStyle(row pgx.Row) (Style, error) {
	var out Style
	var rev int64
	err := row.Scan(&out.ID, &out.TenantID, &out.ProjectID, &out.Name, &out.Description, &rev, &out.Archived, &out.CreatedAt)
	out.Revision = strconv.FormatInt(rev, 10)
	return out, err
}

const pairFields = `p.id,p.training_set_id,p.before_asset_version_id,p.after_asset_version_id,p.session_id,p.confirmed,p.included,p.validation_status,p.ignore_mask_artifact_id,p.revision,p.created_at`

func scanPair(row pgx.Row) (TrainingPair, error) {
	var out TrainingPair
	var rev int64
	err := row.Scan(&out.ID, &out.TrainingSetID, &out.BeforeAssetVersionID, &out.AfterAssetVersionID, &out.SessionID, &out.Confirmed, &out.Included, &out.ValidationStatus, &out.IgnoreMaskArtifactID, &rev, &out.CreatedAt)
	out.Revision = strconv.FormatInt(rev, 10)
	return out, err
}
func validText(name, description string) bool {
	return strings.TrimSpace(name) != "" && len([]rune(name)) <= 120 && len([]rune(description)) <= 2000
}
func audit(ctx context.Context, tx pgx.Tx, who auth.Identity, tid, id uuid.UUID, action string) error {
	_, err := tx.Exec(ctx, `INSERT INTO photo.audit_log(tenant_id,actor_type,actor_id,action,resource_type,resource_id,request_id,details) VALUES($1,'user',$2,$3,'style',$4,$5,'{}')`, tid, who.UserID, action, id, uuid.New())
	return err
}

func authorizeStyle(ctx context.Context, tx pgx.Tx, who auth.Identity, tid, id uuid.UUID, role, required domain.Role, lock bool) (Style, error) {
	suffix := ""
	if lock {
		suffix = " FOR UPDATE OF s"
	}
	out, err := scanStyle(tx.QueryRow(ctx, `SELECT `+styleFields+` FROM photo.styles s WHERE s.tenant_id=$1 AND s.id=$2`+suffix, tid, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return out, domain.NewError("NOT_FOUND", 404)
	}
	if err != nil {
		return out, err
	}
	if _, err = project.Authorize(ctx, tx, tid, out.ProjectID, who.UserID, role, required, false); err != nil {
		return out, err
	}
	return out, nil
}
func (s Service) Get(ctx context.Context, who auth.Identity, tid, id uuid.UUID) (Style, error) {
	var out Style
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		var e error
		out, e = authorizeStyle(ctx, tx, who, tid, id, role, domain.Viewer, false)
		return e
	})
	return out, err
}
func (s Service) ListVersions(ctx context.Context, who auth.Identity, tid, styleID uuid.UUID, limit int, raw string) (pagination.Page[Version], error) {
	out := pagination.Page[Version]{Items: []Version{}}
	if limit < 1 || limit > 100 {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	scope := pagination.Cursor{Tenant: tid.String(), Principal: who.UserID.String(), FilterHash: styleID.String(), Sort: "style-version:version_no,id"}
	var afterID *uuid.UUID
	var afterNo *int
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		style, e := authorizeStyle(ctx, tx, who, tid, styleID, role, domain.Viewer, false)
		if e != nil {
			return e
		}
		scope.Revision = style.Revision
		if raw != "" {
			c, e := s.Cursors.Decode(raw, scope)
			if e != nil {
				return domain.NewError("INVALID_CURSOR", 400)
			}
			id, e := uuid.Parse(c.LastID)
			if e != nil {
				return domain.NewError("INVALID_CURSOR", 400)
			}
			n, e := strconv.Atoi(c.LastPosition)
			if e != nil || n < 1 {
				return domain.NewError("INVALID_CURSOR", 400)
			}
			afterID, afterNo = &id, &n
		}
		rows, e := tx.Query(ctx, `SELECT `+versionFields+` FROM photo.style_versions v WHERE v.tenant_id=$1 AND v.style_id=$2 AND ($3::int IS NULL OR (v.version_no,v.id)>($3,$4::uuid)) ORDER BY v.version_no,v.id LIMIT $5`, tid, styleID, afterNo, afterID, limit+1)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			item, e := scanVersion(rows)
			if e != nil {
				return e
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
		scope.LastID = last.ID.String()
		scope.LastPosition = strconv.Itoa(last.VersionNo)
		next, e := s.Cursors.Encode(scope)
		if e != nil {
			return out, e
		}
		out.NextCursor = &next
	}
	return out, nil
}
func (s Service) Export(ctx context.Context, who auth.Identity, tid, styleID, versionID uuid.UUID) (ArtifactDownload, error) {
	var out ArtifactDownload
	if s.Objects == nil {
		return out, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	var object objectstore.Object
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		style, e := authorizeStyle(ctx, tx, who, tid, styleID, role, domain.Editor, false)
		if e != nil {
			return e
		}
		var config json.RawMessage
		var configHash, bucket, key string
		var version *string
		var expires *time.Time
		e = tx.QueryRow(ctx, `SELECT v.config_json,v.config_hash,a.bucket,a.object_key,a.object_version,a.sha256,a.bytes,a.media_type,a.expires_at FROM photo.style_versions v JOIN photo.artifacts a ON a.tenant_id=v.tenant_id AND a.id=v.export_artifact_id AND a.sha256=v.config_hash WHERE v.tenant_id=$1 AND v.style_id=$2 AND v.id=$3 AND a.state='ready'`, tid, styleID, versionID).Scan(&config, &configHash, &bucket, &key, &version, &object.SHA256, &object.Bytes, &object.ContentType, &expires)
		if errors.Is(e, pgx.ErrNoRows) {
			return domain.NewError("STYLE_PACKAGE_NOT_READY", 409)
		}
		if e != nil {
			return e
		}
		if version == nil || *version == "" || object.ContentType != "application/json" || expires != nil && !expires.After(time.Now()) {
			return domain.NewError("STYLE_PACKAGE_NOT_READY", 409)
		}
		actual, e := canonical.Hash(json.RawMessage(config))
		if e != nil || actual != configHash {
			return errors.New("stored style config hash mismatch")
		}
		if s.Bucket == "" || bucket != s.Bucket {
			return errors.New("style package bucket mismatch")
		}
		object.Key = key
		object.VersionID = *version
		out.Filename = "style-" + style.ID.String() + "-" + versionID.String() + ".json"
		return nil
	})
	if err != nil {
		return out, err
	}
	signed, err := s.Objects.SignDownload(ctx, object, out.Filename, 5*time.Minute)
	if err != nil {
		return ArtifactDownload{}, err
	}
	out.URL = signed.URL
	out.ExpiresAt = signed.ExpiresAt
	out.Bytes = object.Bytes
	out.SHA256 = object.SHA256
	return out, nil
}
func (s Service) AcceptCandidate(ctx context.Context, who auth.Identity, tid, styleID uuid.UUID, key string, req AcceptStyleRequest) (idempotency.Response, error) {
	var out idempotency.Response
	if req.CandidateRunID == uuid.Nil || req.ExpectedStyleRevision < 1 || req.Decision != "accept" {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		var e error
		out, e = idempotency.Use(ctx, tx, idempotency.Scope{Tenant: tid, Principal: who.UserID, PrincipalType: "user", Route: "Style.AcceptCandidate", Key: key}, req, time.Now().Add(30*24*time.Hour), func() (idempotency.Response, error) {
			current, e := authorizeStyle(ctx, tx, who, tid, styleID, role, domain.Editor, true)
			if e != nil {
				return idempotency.Response{}, e
			}
			if current.Revision != strconv.FormatInt(req.ExpectedStyleRevision, 10) {
				return idempotency.Response{}, domain.NewError("REVISION_CONFLICT", 409)
			}
			var candidateID, configArtifactID uuid.UUID
			var config, metrics json.RawMessage
			var storedHash string
			var low bool
			e = tx.QueryRow(ctx, `SELECT c.id,c.config_artifact_id,c.config_json,c.config_hash,c.metrics,c.low_confidence FROM photo.style_candidates c JOIN photo.artifacts a ON a.tenant_id=c.tenant_id AND a.id=c.config_artifact_id AND a.sha256=c.config_hash AND a.state='ready' AND a.object_version IS NOT NULL AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) WHERE c.tenant_id=$1 AND c.style_id=$2 AND c.training_run_id=$3 AND c.state='ready' FOR UPDATE OF c`, tid, styleID, req.CandidateRunID).Scan(&candidateID, &configArtifactID, &config, &storedHash, &metrics, &low)
			if errors.Is(e, pgx.ErrNoRows) {
				return idempotency.Response{}, domain.NewError("STYLE_CANDIDATE_NOT_READY", 409)
			}
			if e != nil {
				return idempotency.Response{}, e
			}
			if low && !req.AcknowledgeLowConfidence {
				return idempotency.Response{}, domain.NewError("LOW_CONFIDENCE_ACK_REQUIRED", 422)
			}
			actual, e := canonical.Hash(json.RawMessage(config))
			if e != nil || actual != storedHash {
				return idempotency.Response{}, errors.New("stored style candidate hash mismatch")
			}
			var imported struct {
				Schema    string `json:"schema_version"`
				SourceSHA string `json:"source_sha256"`
				Assets    []struct {
					PortableID string      `json:"portable_asset_id"`
					Artifact   artifactRef `json:"artifact"`
					SHA        string      `json:"sha256"`
				} `json:"assets"`
			}
			if json.Unmarshal(metrics, &imported) != nil {
				return idempotency.Response{}, errors.New("stored style report invalid")
			}
			if imported.Schema == "style-import-report/v1" {
				if _, hashErr := domain.ParseDigest(imported.SourceSHA); hashErr != nil {
					return idempotency.Response{}, errors.New("stored import source hash invalid")
				}
			}
			var versionNo int
			if e = tx.QueryRow(ctx, `SELECT coalesce(max(version_no),0)+1 FROM photo.style_versions WHERE tenant_id=$1 AND style_id=$2`, tid, styleID).Scan(&versionNo); e != nil {
				return idempotency.Response{}, e
			}
			var sourceSHA any
			if imported.Schema == "style-import-report/v1" {
				sourceSHA = imported.SourceSHA
			}
			item, e := scanVersion(tx.QueryRow(ctx, `INSERT INTO photo.style_versions AS v(tenant_id,style_id,version_no,config_json,config_hash,schema_version,renderer_version,training_run_id,approval_status,metrics,source_file_sha256,export_artifact_id) VALUES($1,$2,$3,$4,$5,'2.0.0','photo_renderer_v2',$6,'accepted',$7,$8,$9) RETURNING `+versionFields, tid, styleID, versionNo, config, storedHash, req.CandidateRunID, metrics, sourceSHA, configArtifactID))
			if e != nil {
				return idempotency.Response{}, e
			}
			if _, e = tx.Exec(ctx, `UPDATE photo.style_candidates SET state='accepted',accepted_version_id=$3 WHERE tenant_id=$1 AND id=$2 AND state='ready'`, tid, candidateID, item.ID); e != nil {
				return idempotency.Response{}, e
			}
			if imported.Schema == "style-import-report/v1" {
				seen := map[string]bool{}
				for _, a := range imported.Assets {
					if a.PortableID == "" || len(a.PortableID) > 120 || seen[a.PortableID] || a.Artifact.ArtifactID == uuid.Nil || a.Artifact.SHA256 != a.SHA {
						return idempotency.Response{}, errors.New("stored import asset binding invalid")
					}
					seen[a.PortableID] = true
					tag, e := tx.Exec(ctx, `INSERT INTO photo.style_asset_bindings(tenant_id,style_version_id,portable_asset_id,artifact_id,sha256) SELECT $1,$2,$3,x.id,x.sha256 FROM photo.artifacts x JOIN photo.tasks t ON t.tenant_id=x.tenant_id AND t.id=x.created_by_task_id WHERE x.tenant_id=$1 AND x.id=$4 AND x.sha256=$5 AND x.output_role='style_asset' AND x.state='ready' AND t.run_id=$6 AND t.operation='style.import'`, tid, item.ID, a.PortableID, a.Artifact.ArtifactID, a.SHA, req.CandidateRunID)
					if e != nil {
						return idempotency.Response{}, e
					}
					if tag.RowsAffected() != 1 {
						return idempotency.Response{}, errors.New("stored import asset provenance invalid")
					}
				}
			}
			if _, e = tx.Exec(ctx, `UPDATE photo.styles SET revision=revision+1 WHERE tenant_id=$1 AND id=$2`, tid, styleID); e != nil {
				return idempotency.Response{}, e
			}
			if e = audit(ctx, tx, who, tid, styleID, "style.version.accept"); e != nil {
				return idempotency.Response{}, e
			}
			body, e := canonical.Marshal(item)
			return idempotency.Response{Status: 201, Body: body, ResourceID: &item.ID}, e
		})
		return e
	})
	return out, err
}

// Analyze freezes the editable training set into immutable, version-pinned
// artifact references. The scheduler only consumes this snapshot; later edits
// to the training set can therefore never change a running analysis.
func (s Service) Analyze(ctx context.Context, who auth.Identity, tid, styleID uuid.UUID, key string, req AnalyzeRequest) (idempotency.Response, error) {
	var out idempotency.Response
	if req.TrainingSetID == uuid.Nil || req.TrainingSetRevision < 1 ||
		(req.AnalysisOptions.AlignmentMode != "similarity_only" && req.AnalysisOptions.AlignmentMode != "crop_aware") {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		var e error
		out, e = idempotency.Use(ctx, tx, idempotency.Scope{Tenant: tid, Principal: who.UserID, PrincipalType: "user", Route: "Style.Analyze", Key: key}, req, time.Now().Add(30*24*time.Hour), func() (idempotency.Response, error) {
			styleRecord, e := authorizeStyle(ctx, tx, who, tid, styleID, role, domain.Editor, true)
			if e != nil {
				return idempotency.Response{}, e
			}
			var revision int64
			var state string
			e = tx.QueryRow(ctx, `SELECT revision,state FROM photo.training_sets WHERE tenant_id=$1 AND id=$2 AND style_id=$3 FOR UPDATE`, tid, req.TrainingSetID, styleID).Scan(&revision, &state)
			if errors.Is(e, pgx.ErrNoRows) {
				return idempotency.Response{}, domain.NewError("NOT_FOUND", 404)
			}
			if e != nil {
				return idempotency.Response{}, e
			}
			if revision != req.TrainingSetRevision || state != "draft" {
				return idempotency.Response{}, domain.NewError("REVISION_CONFLICT", 409)
			}
			var holdoutSnapshotDigest *string
			var fitBackend, fitRuntimeLock, aggregateRuntimeLock string
			e = tx.QueryRow(ctx, `SELECT fit_lock_id,fit_backend,aggregate_lock_id FROM photo.select_style_analysis_runtimes($1,$2)`, tid, req.AnalysisOptions.GeometryEnabled).Scan(&fitRuntimeLock, &fitBackend, &aggregateRuntimeLock)
			if errors.Is(e, pgx.ErrNoRows) {
				return idempotency.Response{}, domain.NewError("STYLE_FIT_CAPABILITY_UNAVAILABLE", 409)
			}
			if e != nil {
				return idempotency.Response{}, e
			}
			if fitRuntimeLock == "" || fitBackend == "" {
				return idempotency.Response{}, domain.NewError("STYLE_FIT_CAPABILITY_UNAVAILABLE", 409)
			}
			if aggregateRuntimeLock == "" {
				return idempotency.Response{}, domain.NewError("STYLE_AGGREGATE_CAPABILITY_UNAVAILABLE", 409)
			}
			rows, e := tx.Query(ctx, `SELECT p.id,p.session_id,
			 bv.master_artifact_id,ba.sha256,ba.bucket,ba.object_key,ba.object_version,ba.bytes,ba.media_type,
			 av.master_artifact_id,aa.sha256,aa.bucket,aa.object_key,aa.object_version,aa.bytes,aa.media_type,
			 p.ignore_mask_artifact_id,im.id,im.sha256,im.bucket,im.object_key,im.object_version,im.bytes,im.media_type
			 FROM photo.training_pairs p
			 JOIN photo.asset_versions bv ON bv.tenant_id=p.tenant_id AND bv.id=p.before_asset_version_id
			 JOIN photo.assets bs ON bs.tenant_id=bv.tenant_id AND bs.id=bv.asset_id AND bs.project_id=(SELECT project_id FROM photo.styles WHERE tenant_id=$1 AND id=$2) AND bs.upload_state='verified' AND bs.deleted_at IS NULL
			 JOIN photo.artifacts ba ON ba.tenant_id=bv.tenant_id AND ba.id=bv.master_artifact_id AND ba.sha256=bv.content_hash AND ba.state='ready' AND ba.object_version IS NOT NULL AND (ba.expires_at IS NULL OR ba.expires_at>clock_timestamp())
			 JOIN photo.asset_versions av ON av.tenant_id=p.tenant_id AND av.id=p.after_asset_version_id
			 JOIN photo.assets ats ON ats.tenant_id=av.tenant_id AND ats.id=av.asset_id AND ats.project_id=bs.project_id AND ats.upload_state='verified' AND ats.deleted_at IS NULL
			 JOIN photo.artifacts aa ON aa.tenant_id=av.tenant_id AND aa.id=av.master_artifact_id AND aa.sha256=av.content_hash AND aa.state='ready' AND aa.object_version IS NOT NULL AND (aa.expires_at IS NULL OR aa.expires_at>clock_timestamp())
			 LEFT JOIN photo.artifacts im ON im.tenant_id=p.tenant_id AND im.id=p.ignore_mask_artifact_id AND im.state='ready' AND im.object_version IS NOT NULL AND (im.expires_at IS NULL OR im.expires_at>clock_timestamp())
			 WHERE p.tenant_id=$1 AND p.training_set_id=$3 AND p.included AND p.confirmed ORDER BY p.created_at,p.id`, tid, styleID, req.TrainingSetID)
			if e != nil {
				return idempotency.Response{}, e
			}
			defer rows.Close()
			pairs := make([]analysisPairSnapshot, 0, 32)
			for rows.Next() {
				var p analysisPairSnapshot
				var declaredMaskID, maskID *uuid.UUID
				var maskSHA, maskBucket, maskKey, maskVersion, maskMedia *string
				var maskBytes *int64
				e = rows.Scan(&p.PairID, &p.SessionID, &p.Before.ArtifactID, &p.Before.SHA256, &p.Before.Bucket, &p.Before.Key, &p.Before.VersionID, &p.Before.Bytes, &p.Before.MediaType, &p.After.ArtifactID, &p.After.SHA256, &p.After.Bucket, &p.After.Key, &p.After.VersionID, &p.After.Bytes, &p.After.MediaType, &declaredMaskID, &maskID, &maskSHA, &maskBucket, &maskKey, &maskVersion, &maskBytes, &maskMedia)
				if e != nil {
					return idempotency.Response{}, e
				}
				if declaredMaskID != nil && (maskID == nil || *declaredMaskID != *maskID) {
					return idempotency.Response{}, domain.NewError("TRAINING_MASK_NOT_READY", 409)
				}
				if maskID != nil {
					if maskSHA == nil || maskBucket == nil || maskKey == nil || maskVersion == nil || maskBytes == nil || maskMedia == nil {
						return idempotency.Response{}, errors.New("invalid ready ignore mask")
					}
					p.IgnoreMask = &artifactRef{ArtifactID: *maskID, SHA256: *maskSHA, Bucket: *maskBucket, Key: *maskKey, VersionID: *maskVersion, Bytes: *maskBytes, MediaType: *maskMedia}
				}
				pairs = append(pairs, p)
			}
			if e = rows.Err(); e != nil {
				return idempotency.Response{}, e
			}
			if len(pairs) < 1 || len(pairs) > 32 {
				return idempotency.Response{}, domain.NewError("TRAINING_PAIR_COUNT_INVALID", 422)
			}
			if req.HoldoutSnapshotID != nil {
				sessions := map[string]bool{}
				for _, p := range pairs {
					sessions[p.SessionID] = true
				}
				holdoutDigest, validationErr := validateHoldoutSnapshot(ctx, tx, tid, styleRecord.ProjectID, *req.HoldoutSnapshotID, sessions)
				if validationErr != nil {
					return idempotency.Response{}, validationErr
				}
				holdoutSnapshotDigest = &holdoutDigest
			}
			digest, e := canonical.Hash(pairs)
			if e != nil {
				return idempotency.Response{}, e
			}
			snapshotID, runID := uuid.New(), uuid.New()
			if _, e = tx.Exec(ctx, `INSERT INTO photo.snapshots(id,tenant_id,kind,training_set_id,source_revision,principal_type,principal_id,digest,item_count) VALUES($1,$2,'training',$3,$4,'user',$5,$6,$7)`, snapshotID, tid, req.TrainingSetID, revision, who.UserID, digest, len(pairs)); e != nil {
				return idempotency.Response{}, e
			}
			for i, p := range pairs {
				payload, e := canonical.Marshal(p)
				if e != nil {
					return idempotency.Response{}, e
				}
				sum, e := canonical.Hash(p)
				if e != nil {
					return idempotency.Response{}, e
				}
				if _, e = tx.Exec(ctx, `INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,item_id,artifact_id,sha256,payload) VALUES($1,$2,$3,$4,$5,$6,$7)`, tid, snapshotID, i, p.PairID, p.Before.ArtifactID, sum, payload); e != nil {
					return idempotency.Response{}, e
				}
			}
			config := struct {
				Schema                 string          `json:"schema"`
				StyleID                uuid.UUID       `json:"style_id"`
				TrainingSetID          uuid.UUID       `json:"training_set_id"`
				TrainingSetRevision    int64           `json:"training_set_revision"`
				SnapshotDigest         string          `json:"snapshot_digest"`
				Options                AnalysisOptions `json:"analysis_options"`
				HoldoutSnapshotID      *uuid.UUID      `json:"holdout_snapshot_id,omitempty"`
				HoldoutSnapshotDigest  *string         `json:"holdout_snapshot_digest,omitempty"`
				FitBackend             string          `json:"fit_backend"`
				FitRuntimeLockID       string          `json:"fit_runtime_lock_id"`
				AggregateRuntimeLockID string          `json:"aggregate_runtime_lock_id"`
			}{"style-analysis/v1", styleID, req.TrainingSetID, revision, digest, req.AnalysisOptions, req.HoldoutSnapshotID, holdoutSnapshotDigest, fitBackend, fitRuntimeLock, aggregateRuntimeLock}
			configBytes, e := canonical.Marshal(config)
			if e != nil {
				return idempotency.Response{}, e
			}
			configHash, e := canonical.Hash(config)
			if e != nil {
				return idempotency.Response{}, e
			}
			if _, e = tx.Exec(ctx, `INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed) VALUES($1,$2,'analyze','queued',$3,$4,$5,$6,'fail_fast',false)`, runID, tid, snapshotID, configBytes, configHash, who.UserID); e != nil {
				return idempotency.Response{}, e
			}
			var buildCreated bool
			if e = tx.QueryRow(ctx, `SELECT photo.create_style_analysis_build($1,$2,$3,$4)`, tid, runID, styleID, len(pairs)).Scan(&buildCreated); e != nil {
				return idempotency.Response{}, e
			}
			if !buildCreated {
				return idempotency.Response{}, errors.New("style analysis build rejected")
			}
			if e = audit(ctx, tx, who, tid, styleID, "style.analysis.create"); e != nil {
				return idempotency.Response{}, e
			}
			body, e := canonical.Marshal(RunAccepted{RunID: runID, Status: "queued"})
			return idempotency.Response{Status: 202, Body: body, ResourceID: &runID}, e
		})
		return e
	})
	return out, err
}

// CreateHoldoutSnapshot freezes a separate confirmed pair set for independent
// validation. Analyze later revalidates every artifact and rejects overlapping
// training sessions, so this snapshot cannot silently become training data.
func (s Service) CreateHoldoutSnapshot(ctx context.Context, who auth.Identity, tid, setID uuid.UUID, key string, req HoldoutSnapshotRequest) (idempotency.Response, error) {
	var out idempotency.Response
	if setID == uuid.Nil || req.ExpectedRevision < 1 {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		var e error
		out, e = idempotency.Use(ctx, tx, idempotency.Scope{Tenant: tid, Principal: who.UserID, PrincipalType: "user", Route: "Style.CreateHoldoutSnapshot", Key: key}, req, time.Now().Add(30*24*time.Hour), func() (idempotency.Response, error) {
			var styleID, projectID uuid.UUID
			var revision int64
			var state string
			e := tx.QueryRow(ctx, `SELECT t.style_id,t.revision,t.state,s.project_id FROM photo.training_sets t JOIN photo.styles s ON s.tenant_id=t.tenant_id AND s.id=t.style_id WHERE t.tenant_id=$1 AND t.id=$2 FOR UPDATE OF t`, tid, setID).Scan(&styleID, &revision, &state, &projectID)
			if errors.Is(e, pgx.ErrNoRows) {
				return idempotency.Response{}, domain.NewError("NOT_FOUND", 404)
			}
			if e != nil {
				return idempotency.Response{}, e
			}
			if revision != req.ExpectedRevision || state != "draft" {
				return idempotency.Response{}, domain.NewError("REVISION_CONFLICT", 409)
			}
			if _, e = authorizeStyle(ctx, tx, who, tid, styleID, role, domain.Editor, true); e != nil {
				return idempotency.Response{}, e
			}
			rows, e := tx.Query(ctx, `SELECT p.id,p.session_id,bv.master_artifact_id,ba.sha256,ba.bucket,ba.object_key,ba.object_version,ba.bytes,ba.media_type,av.master_artifact_id,aa.sha256,aa.bucket,aa.object_key,aa.object_version,aa.bytes,aa.media_type,p.ignore_mask_artifact_id,im.id,im.sha256,im.bucket,im.object_key,im.object_version,im.bytes,im.media_type FROM photo.training_pairs p JOIN photo.asset_versions bv ON bv.tenant_id=p.tenant_id AND bv.id=p.before_asset_version_id JOIN photo.assets bs ON bs.tenant_id=bv.tenant_id AND bs.id=bv.asset_id AND bs.project_id=$3 AND bs.upload_state='verified' AND bs.deleted_at IS NULL JOIN photo.artifacts ba ON ba.tenant_id=bv.tenant_id AND ba.id=bv.master_artifact_id AND ba.sha256=bv.content_hash AND ba.state='ready' AND ba.object_version IS NOT NULL AND (ba.expires_at IS NULL OR ba.expires_at>clock_timestamp()) JOIN photo.asset_versions av ON av.tenant_id=p.tenant_id AND av.id=p.after_asset_version_id JOIN photo.assets ats ON ats.tenant_id=av.tenant_id AND ats.id=av.asset_id AND ats.project_id=$3 AND ats.upload_state='verified' AND ats.deleted_at IS NULL JOIN photo.artifacts aa ON aa.tenant_id=av.tenant_id AND aa.id=av.master_artifact_id AND aa.sha256=av.content_hash AND aa.state='ready' AND aa.object_version IS NOT NULL AND (aa.expires_at IS NULL OR aa.expires_at>clock_timestamp()) LEFT JOIN photo.artifacts im ON im.tenant_id=p.tenant_id AND im.id=p.ignore_mask_artifact_id AND im.state='ready' AND im.object_version IS NOT NULL AND (im.expires_at IS NULL OR im.expires_at>clock_timestamp()) WHERE p.tenant_id=$1 AND p.training_set_id=$2 AND p.included AND p.confirmed ORDER BY p.created_at,p.id`, tid, setID, projectID)
			if e != nil {
				return idempotency.Response{}, e
			}
			defer rows.Close()
			pairs := make([]analysisPairSnapshot, 0, 64)
			for rows.Next() {
				var p analysisPairSnapshot
				var declaredMaskID, maskID *uuid.UUID
				var maskSHA, maskBucket, maskKey, maskVersion, maskMedia *string
				var maskBytes *int64
				e = rows.Scan(&p.PairID, &p.SessionID, &p.Before.ArtifactID, &p.Before.SHA256, &p.Before.Bucket, &p.Before.Key, &p.Before.VersionID, &p.Before.Bytes, &p.Before.MediaType, &p.After.ArtifactID, &p.After.SHA256, &p.After.Bucket, &p.After.Key, &p.After.VersionID, &p.After.Bytes, &p.After.MediaType, &declaredMaskID, &maskID, &maskSHA, &maskBucket, &maskKey, &maskVersion, &maskBytes, &maskMedia)
				if e != nil {
					return idempotency.Response{}, e
				}
				if declaredMaskID != nil && (maskID == nil || *declaredMaskID != *maskID) {
					return idempotency.Response{}, domain.NewError("TRAINING_MASK_NOT_READY", 409)
				}
				if maskID != nil {
					if maskSHA == nil || maskBucket == nil || maskKey == nil || maskVersion == nil || maskBytes == nil || maskMedia == nil {
						return idempotency.Response{}, errors.New("invalid ready ignore mask")
					}
					p.IgnoreMask = &artifactRef{*maskID, *maskSHA, *maskBucket, *maskKey, *maskVersion, *maskBytes, *maskMedia}
				}
				pairs = append(pairs, p)
			}
			if e = rows.Err(); e != nil {
				return idempotency.Response{}, e
			}
			if len(pairs) < 1 || len(pairs) > 64 {
				return idempotency.Response{}, domain.NewError("HOLDOUT_PAIR_COUNT_INVALID", 422)
			}
			digest, e := canonical.Hash(pairs)
			if e != nil {
				return idempotency.Response{}, e
			}
			snapshotID := uuid.New()
			if _, e = tx.Exec(ctx, `INSERT INTO photo.snapshots(id,tenant_id,kind,training_set_id,source_revision,principal_type,principal_id,digest,item_count) VALUES($1,$2,'holdout',$3,$4,'user',$5,$6,$7)`, snapshotID, tid, setID, revision, who.UserID, digest, len(pairs)); e != nil {
				return idempotency.Response{}, e
			}
			for i, p := range pairs {
				payload, e := canonical.Marshal(p)
				if e != nil {
					return idempotency.Response{}, e
				}
				sum, e := canonical.Hash(p)
				if e != nil {
					return idempotency.Response{}, e
				}
				if _, e = tx.Exec(ctx, `INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,item_id,artifact_id,sha256,payload) VALUES($1,$2,$3,$4,$5,$6,$7)`, tid, snapshotID, i, p.PairID, p.Before.ArtifactID, sum, payload); e != nil {
					return idempotency.Response{}, e
				}
			}
			body, e := canonical.Marshal(HoldoutSnapshot{snapshotID, digest, len(pairs), revision})
			return idempotency.Response{Status: 201, Body: body, ResourceID: &snapshotID}, e
		})
		return e
	})
	return out, err
}

// Import records exact immutable source objects and returns before any
// untrusted package parsing. The scheduler/worker path performs that parsing.
func (s Service) Import(ctx context.Context, who auth.Identity, tid uuid.UUID, key string, req ImportRequest) (idempotency.Response, error) {
	var out idempotency.Response
	if req.ProjectID == uuid.Nil || req.UploadArtifactID == uuid.Nil || strings.TrimSpace(req.Name) == "" || len([]rune(req.Name)) > 120 || (req.AssetsManifestArtifactID != nil && *req.AssetsManifestArtifactID == uuid.Nil) {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		if _, e := project.Authorize(ctx, tx, tid, req.ProjectID, who.UserID, role, domain.Editor, false); e != nil {
			return e
		}
		var e error
		out, e = idempotency.Use(ctx, tx, idempotency.Scope{Tenant: tid, Principal: who.UserID, PrincipalType: "user", Route: "Style.Import", Key: key}, req, time.Now().Add(30*24*time.Hour), func() (idempotency.Response, error) {
			type source struct {
				ID    uuid.UUID
				SHA   string
				Bytes int64
			}
			load := func(id uuid.UUID) (source, error) {
				var v source
				err := tx.QueryRow(ctx, `SELECT id,sha256,bytes FROM photo.artifacts WHERE tenant_id=$1 AND id=$2 AND state='ready' AND object_version IS NOT NULL AND (expires_at IS NULL OR expires_at>clock_timestamp()) AND bytes BETWEEN 2 AND 104857600`, tid, id).Scan(&v.ID, &v.SHA, &v.Bytes)
				if errors.Is(err, pgx.ErrNoRows) {
					err = domain.NewError("IMPORT_ARTIFACT_NOT_READY", 409)
				}
				return v, err
			}
			pkg, e := load(req.UploadArtifactID)
			if e != nil {
				return idempotency.Response{}, e
			}
			var manifest *source
			if req.AssetsManifestArtifactID != nil {
				v, e := load(*req.AssetsManifestArtifactID)
				if e != nil {
					return idempotency.Response{}, e
				}
				if v.Bytes > 1048576 {
					return idempotency.Response{}, domain.NewError("VALIDATION_FAILED", 422)
				}
				manifest = &v
			}
			styleID, snapshotID, runID, taskID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
			if _, e = tx.Exec(ctx, `INSERT INTO photo.styles(id,tenant_id,project_id,name,description,created_by) VALUES($1,$2,$3,$4,'Imported style awaiting review',$5)`, styleID, tid, req.ProjectID, strings.TrimSpace(req.Name), who.UserID); e != nil {
				return idempotency.Response{}, e
			}
			digest, e := canonical.Hash(struct {
				Package  source  `json:"package"`
				Manifest *source `json:"manifest,omitempty"`
			}{pkg, manifest})
			if e != nil {
				return idempotency.Response{}, e
			}
			if _, e = tx.Exec(ctx, `INSERT INTO photo.snapshots(id,tenant_id,kind,principal_type,principal_id,digest,item_count,artifact_id) VALUES($1,$2,'style_import','user',$3,$4,1,$5)`, snapshotID, tid, who.UserID, digest, pkg.ID); e != nil {
				return idempotency.Response{}, e
			}
			cfg := struct {
				Schema   string       `json:"schema"`
				StyleID  uuid.UUID    `json:"style_id"`
				Package  artifactRef  `json:"package"`
				Manifest *artifactRef `json:"assets_manifest,omitempty"`
			}{Schema: "style-import/v1", StyleID: styleID, Package: artifactRef{ArtifactID: pkg.ID, SHA256: pkg.SHA}}
			if manifest != nil {
				cfg.Manifest = &artifactRef{ArtifactID: manifest.ID, SHA256: manifest.SHA}
			}
			cfgRaw, e := canonical.Marshal(cfg)
			if e != nil {
				return idempotency.Response{}, e
			}
			cfgHash, e := canonical.Hash(cfg)
			if e != nil {
				return idempotency.Response{}, e
			}
			if _, e = tx.Exec(ctx, `INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed) VALUES($1,$2,'style_import','queued',$3,$4,$5,$6,'fail_fast',false)`, runID, tid, snapshotID, cfgRaw, cfgHash, who.UserID); e != nil {
				return idempotency.Response{}, e
			}
			params := struct {
				Package  artifactRef  `json:"package"`
				Target   uuid.UUID    `json:"target_style_id"`
				Manifest *artifactRef `json:"assets_manifest,omitempty"`
				Version  string       `json:"package_version"`
			}{cfg.Package, styleID, cfg.Manifest, "style_import_v1"}
			paramsRaw, e := canonical.Marshal(params)
			if e != nil {
				return idempotency.Response{}, e
			}
			paramsHash, e := canonical.Hash(params)
			if e != nil {
				return idempotency.Response{}, e
			}
			if _, e = tx.Exec(ctx, `INSERT INTO photo.style_import_builds(tenant_id,run_id,style_id,task_id,package_artifact_id,manifest_artifact_id,params,params_sha256) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, tid, runID, styleID, taskID, pkg.ID, req.AssetsManifestArtifactID, paramsRaw, paramsHash); e != nil {
				return idempotency.Response{}, e
			}
			if e = audit(ctx, tx, who, tid, styleID, "style.import.create"); e != nil {
				return idempotency.Response{}, e
			}
			body, e := canonical.Marshal(RunAccepted{RunID: runID, Status: "queued"})
			return idempotency.Response{Status: 202, Body: body, ResourceID: &runID}, e
		})
		return e
	})
	return out, err
}
func (s Service) List(ctx context.Context, who auth.Identity, tid uuid.UUID, limit int, raw, projectRaw, search string) (pagination.Page[Style], error) {
	out := pagination.Page[Style]{Items: []Style{}}
	if limit < 1 || limit > 100 || len([]rune(search)) > 200 {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	var projectID *uuid.UUID
	if projectRaw != "" {
		id, e := uuid.Parse(projectRaw)
		if e != nil {
			return out, domain.NewError("VALIDATION_FAILED", 422)
		}
		projectID = &id
	}
	filter, e := canonical.Hash(map[string]string{"project_id": projectRaw, "search": search})
	if e != nil {
		return out, e
	}
	scope := pagination.Cursor{Tenant: tid.String(), Principal: who.UserID.String(), FilterHash: filter, Sort: "style:created_at,id"}
	var afterID *uuid.UUID
	var afterTime *time.Time
	if raw != "" {
		c, e := s.Cursors.Decode(raw, scope)
		if e != nil {
			return out, domain.NewError("INVALID_CURSOR", 400)
		}
		id, e := uuid.Parse(c.LastID)
		if e != nil {
			return out, domain.NewError("INVALID_CURSOR", 400)
		}
		at, e := time.Parse(time.RFC3339Nano, c.LastPosition)
		if e != nil {
			return out, domain.NewError("INVALID_CURSOR", 400)
		}
		afterID = &id
		afterTime = &at
	}
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		rows, e := tx.Query(ctx, `SELECT `+styleFields+` FROM photo.styles s JOIN photo.projects p ON p.tenant_id=s.tenant_id AND p.id=s.project_id AND p.archived_at IS NULL WHERE s.tenant_id=$1 AND s.archived_at IS NULL AND ($2::boolean OR p.visibility='workspace' OR EXISTS(SELECT FROM photo.project_acl a WHERE a.tenant_id=p.tenant_id AND a.project_id=p.id AND a.user_id=$3)) AND ($4::uuid IS NULL OR s.project_id=$4) AND strpos(lower(s.name),lower($5))>0 AND ($6::uuid IS NULL OR (s.created_at,s.id)>($7::timestamptz,$6::uuid)) ORDER BY s.created_at,s.id LIMIT $8`, tid, role.Allows(domain.Admin), who.UserID, projectID, search, afterID, afterTime, limit+1)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			item, e := scanStyle(rows)
			if e != nil {
				return e
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
		scope.LastID = last.ID.String()
		scope.LastPosition = last.CreatedAt.Format(time.RFC3339Nano)
		next, e := s.Cursors.Encode(scope)
		if e != nil {
			return out, e
		}
		out.NextCursor = &next
	}
	return out, nil
}
func (s Service) Create(ctx context.Context, who auth.Identity, tid uuid.UUID, key string, req Create) (idempotency.Response, error) {
	var out idempotency.Response
	if req.ProjectID == uuid.Nil || !validText(req.Name, req.Description) {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		if _, e := project.Authorize(ctx, tx, tid, req.ProjectID, who.UserID, role, domain.Editor, false); e != nil {
			return e
		}
		var e error
		out, e = idempotency.Use(ctx, tx, idempotency.Scope{Tenant: tid, Principal: who.UserID, PrincipalType: "user", Route: "Style.Create", Key: key}, req, time.Now().Add(24*time.Hour), func() (idempotency.Response, error) {
			item, e := scanStyle(tx.QueryRow(ctx, `INSERT INTO photo.styles AS s(tenant_id,project_id,name,description,created_by) VALUES($1,$2,$3,$4,$5) RETURNING `+styleFields, tid, req.ProjectID, strings.TrimSpace(req.Name), req.Description, who.UserID))
			if e != nil {
				return idempotency.Response{}, e
			}
			if e = audit(ctx, tx, who, tid, item.ID, "style.create"); e != nil {
				return idempotency.Response{}, e
			}
			body, e := canonical.Marshal(item)
			return idempotency.Response{Status: 201, Body: body, ResourceID: &item.ID}, e
		})
		return e
	})
	return out, err
}
func (s Service) Update(ctx context.Context, who auth.Identity, tid, id uuid.UUID, expected int64, patch Patch) (Style, error) {
	var out Style
	if expected < 1 || patch.ExpectedRevision != expected {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		current, e := authorizeStyle(ctx, tx, who, tid, id, role, domain.Editor, true)
		if e != nil {
			return e
		}
		if current.Revision != strconv.FormatInt(expected, 10) {
			return domain.NewError("REVISION_CONFLICT", 409)
		}
		if patch.Name != nil {
			current.Name = *patch.Name
		}
		if patch.Description != nil {
			current.Description = *patch.Description
		}
		if !validText(current.Name, current.Description) {
			return domain.NewError("VALIDATION_FAILED", 422)
		}
		archived := current.Archived
		if patch.Archived != nil {
			archived = *patch.Archived
		}
		out, e = scanStyle(tx.QueryRow(ctx, `UPDATE photo.styles AS s SET name=$3,description=$4,archived_at=CASE WHEN $5 THEN coalesce(archived_at,clock_timestamp()) ELSE NULL END,revision=revision+1 WHERE tenant_id=$1 AND id=$2 RETURNING `+styleFields, tid, id, strings.TrimSpace(current.Name), current.Description, archived))
		if e != nil {
			return e
		}
		return audit(ctx, tx, who, tid, id, "style.update")
	})
	return out, err
}

func (s Service) CreateTrainingSet(ctx context.Context, who auth.Identity, tid, styleID uuid.UUID, key string) (idempotency.Response, error) {
	var out idempotency.Response
	request := struct {
		StyleID uuid.UUID `json:"style_id"`
	}{styleID}
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		if _, e := authorizeStyle(ctx, tx, who, tid, styleID, role, domain.Editor, false); e != nil {
			return e
		}
		var e error
		out, e = idempotency.Use(ctx, tx, idempotency.Scope{Tenant: tid, Principal: who.UserID, PrincipalType: "user", Route: "Training.CreateSet", Key: key}, request, time.Now().Add(24*time.Hour), func() (idempotency.Response, error) {
			var item TrainingSet
			var rev int64
			e := tx.QueryRow(ctx, `INSERT INTO photo.training_sets(tenant_id,style_id) VALUES($1,$2) RETURNING id,style_id,revision,state,created_at`, tid, styleID).Scan(&item.ID, &item.StyleID, &rev, &item.State, &item.CreatedAt)
			if e != nil {
				return idempotency.Response{}, e
			}
			item.Revision = strconv.FormatInt(rev, 10)
			item.Pairs = pagination.Page[TrainingPair]{Items: []TrainingPair{}}
			body, e := canonical.Marshal(item)
			return idempotency.Response{Status: 201, Body: body, ResourceID: &item.ID}, e
		})
		return e
	})
	return out, err
}
func (s Service) GetTrainingSet(ctx context.Context, who auth.Identity, tid, id uuid.UUID, limit int, raw string) (TrainingSet, error) {
	var out TrainingSet
	if limit < 1 || limit > 100 {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		var rev int64
		var projectID uuid.UUID
		e := tx.QueryRow(ctx, `SELECT t.id,t.style_id,t.revision,t.state,t.created_at,s.project_id FROM photo.training_sets t JOIN photo.styles s ON s.tenant_id=t.tenant_id AND s.id=t.style_id WHERE t.tenant_id=$1 AND t.id=$2`, tid, id).Scan(&out.ID, &out.StyleID, &rev, &out.State, &out.CreatedAt, &projectID)
		if errors.Is(e, pgx.ErrNoRows) {
			return domain.NewError("NOT_FOUND", 404)
		}
		if e != nil {
			return e
		}
		if _, e = project.Authorize(ctx, tx, tid, projectID, who.UserID, role, domain.Editor, false); e != nil {
			return e
		}
		out.Revision = strconv.FormatInt(rev, 10)
		out.Pairs = pagination.Page[TrainingPair]{Items: []TrainingPair{}}
		scope := pagination.Cursor{Tenant: tid.String(), Principal: who.UserID.String(), FilterHash: id.String(), Sort: "training-pair:created_at,id", Revision: out.Revision}
		var afterID *uuid.UUID
		var afterTime *time.Time
		if raw != "" {
			c, e := s.Cursors.Decode(raw, scope)
			if e != nil {
				return domain.NewError("INVALID_CURSOR", 400)
			}
			pid, e := uuid.Parse(c.LastID)
			if e != nil {
				return domain.NewError("INVALID_CURSOR", 400)
			}
			at, e := time.Parse(time.RFC3339Nano, c.LastPosition)
			if e != nil {
				return domain.NewError("INVALID_CURSOR", 400)
			}
			afterID = &pid
			afterTime = &at
		}
		rows, e := tx.Query(ctx, `SELECT `+pairFields+` FROM photo.training_pairs p WHERE p.tenant_id=$1 AND p.training_set_id=$2 AND ($3::uuid IS NULL OR (p.created_at,p.id)>($4::timestamptz,$3::uuid)) ORDER BY p.created_at,p.id LIMIT $5`, tid, id, afterID, afterTime, limit+1)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			pair, e := scanPair(rows)
			if e != nil {
				return e
			}
			out.Pairs.Items = append(out.Pairs.Items, pair)
		}
		if e = rows.Err(); e != nil {
			return e
		}
		out.Pairs.Revision = out.Revision
		if len(out.Pairs.Items) > limit {
			out.Pairs.Items = out.Pairs.Items[:limit]
			last := out.Pairs.Items[limit-1]
			scope.LastID = last.ID.String()
			scope.LastPosition = last.CreatedAt.Format(time.RFC3339Nano)
			next, e := s.Cursors.Encode(scope)
			if e != nil {
				return e
			}
			out.Pairs.NextCursor = &next
		}
		return nil
	})
	return out, err
}
func validatePair(ctx context.Context, tx pgx.Tx, tid uuid.UUID, styleID uuid.UUID, req PairCreate) error {
	if req.BeforeAssetVersionID == uuid.Nil || req.AfterAssetVersionID == uuid.Nil || req.BeforeAssetVersionID == req.AfterAssetVersionID || strings.TrimSpace(req.SessionID) == "" || len([]rune(req.SessionID)) > 120 {
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM photo.asset_versions v JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id JOIN photo.styles s ON s.tenant_id=a.tenant_id AND s.project_id=a.project_id WHERE v.tenant_id=$1 AND s.id=$2 AND v.id=ANY($3::uuid[]) AND a.deleted_at IS NULL AND a.upload_state='verified'`, tid, styleID, []uuid.UUID{req.BeforeAssetVersionID, req.AfterAssetVersionID}).Scan(&count); err != nil {
		return err
	}
	if count != 2 {
		return domain.NewError("NOT_FOUND", 404)
	}
	if req.IgnoreMaskArtifactID != nil {
		var valid bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT FROM photo.artifacts WHERE tenant_id=$1 AND id=$2 AND state='ready' AND role IN ('mask','manual_mask','rasterized_mask'))`, tid, *req.IgnoreMaskArtifactID).Scan(&valid); err != nil {
			return err
		}
		if !valid {
			return domain.NewError("NOT_FOUND", 404)
		}
	}
	return nil
}
func (s Service) AddPair(ctx context.Context, who auth.Identity, tid, setID uuid.UUID, key string, req PairCreate) (idempotency.Response, error) {
	var out idempotency.Response
	req.SessionID = strings.TrimSpace(req.SessionID)
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		var styleID, projectID uuid.UUID
		var state string
		e := tx.QueryRow(ctx, `SELECT t.style_id,t.state,s.project_id FROM photo.training_sets t JOIN photo.styles s ON s.tenant_id=t.tenant_id AND s.id=t.style_id WHERE t.tenant_id=$1 AND t.id=$2 FOR UPDATE OF t`, tid, setID).Scan(&styleID, &state, &projectID)
		if errors.Is(e, pgx.ErrNoRows) {
			return domain.NewError("NOT_FOUND", 404)
		}
		if e != nil {
			return e
		}
		if state != "draft" {
			return domain.NewError("REVISION_CONFLICT", 409)
		}
		if _, e = project.Authorize(ctx, tx, tid, projectID, who.UserID, role, domain.Editor, false); e != nil {
			return e
		}
		if e = validatePair(ctx, tx, tid, styleID, req); e != nil {
			return e
		}
		out, e = idempotency.Use(ctx, tx, idempotency.Scope{Tenant: tid, Principal: who.UserID, PrincipalType: "user", Route: "Training.AddPair", Key: key}, req, time.Now().Add(24*time.Hour), func() (idempotency.Response, error) {
			pair, e := scanPair(tx.QueryRow(ctx, `INSERT INTO photo.training_pairs AS p(tenant_id,training_set_id,before_asset_version_id,after_asset_version_id,session_id,confirmed,included,ignore_mask_artifact_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING `+pairFields, tid, setID, req.BeforeAssetVersionID, req.AfterAssetVersionID, req.SessionID, req.Confirmed, req.Included, req.IgnoreMaskArtifactID))
			if e != nil {
				return idempotency.Response{}, e
			}
			if _, e = tx.Exec(ctx, `UPDATE photo.training_sets SET revision=revision+1 WHERE tenant_id=$1 AND id=$2`, tid, setID); e != nil {
				return idempotency.Response{}, e
			}
			body, e := canonical.Marshal(pair)
			return idempotency.Response{Status: 201, Body: body, ResourceID: &pair.ID}, e
		})
		return e
	})
	return out, err
}

func (s Service) AddFeedback(ctx context.Context, who auth.Identity, tid, collectionID uuid.UUID, key string, req FeedbackRequest) (idempotency.Response, error) {
	var out idempotency.Response
	req.SessionID = strings.TrimSpace(req.SessionID)
	if collectionID == uuid.Nil || req.TrainingSetID == uuid.Nil || req.ExpectedRevision < 1 || req.SourceAssetVersionID == uuid.Nil || req.ResultAssetVersionID == uuid.Nil || req.SourceAssetVersionID == req.ResultAssetVersionID || !req.Confirmed || req.SessionID == "" || len([]rune(req.SessionID)) > 120 {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	if !idempotency.ValidKey(key) {
		return out, domain.NewError("IDEMPOTENCY_KEY_REQUIRED", 400)
	}
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		var e error
		out, e = idempotency.Use(ctx, tx, idempotency.Scope{Tenant: tid, Principal: who.UserID, PrincipalType: "user", Route: "Training.AddFeedback", Key: key}, req, time.Now().Add(24*time.Hour), func() (idempotency.Response, error) {
			var collectionProject uuid.UUID
			if queryErr := tx.QueryRow(ctx, `SELECT project_id FROM photo.collections WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL FOR SHARE`, tid, collectionID).Scan(&collectionProject); errors.Is(queryErr, pgx.ErrNoRows) {
				return idempotency.Response{}, domain.NewError("NOT_FOUND", 404)
			} else if queryErr != nil {
				return idempotency.Response{}, queryErr
			}
			if _, queryErr := project.Authorize(ctx, tx, tid, collectionProject, who.UserID, role, domain.Editor, false); queryErr != nil {
				return idempotency.Response{}, queryErr
			}
			var styleID, styleProject uuid.UUID
			var setRevision int64
			var setState string
			if queryErr := tx.QueryRow(ctx, `SELECT t.style_id,t.revision,t.state,s.project_id FROM photo.training_sets t JOIN photo.styles s ON s.tenant_id=t.tenant_id AND s.id=t.style_id WHERE t.tenant_id=$1 AND t.id=$2 FOR UPDATE OF t`, tid, req.TrainingSetID).Scan(&styleID, &setRevision, &setState, &styleProject); errors.Is(queryErr, pgx.ErrNoRows) {
				return idempotency.Response{}, domain.NewError("NOT_FOUND", 404)
			} else if queryErr != nil {
				return idempotency.Response{}, queryErr
			}
			if styleProject != collectionProject || setState != "draft" || setRevision != req.ExpectedRevision {
				return idempotency.Response{}, domain.NewError("REVISION_CONFLICT", 409)
			}
			var sourceKind string
			queryErr := tx.QueryRow(ctx, `SELECT CASE WHEN r.config_snapshot->>'explicit_generation'='true' THEN 'user_added_generated' ELSE 'feedback_edit' END
			 FROM photo.collection_items i JOIN photo.asset_versions result ON result.tenant_id=i.tenant_id AND result.id=i.asset_version_id AND result.asset_id=i.asset_id
			 LEFT JOIN photo.runs r ON r.tenant_id=result.tenant_id AND r.id=result.created_by_run_id
			 WHERE i.tenant_id=$1 AND i.collection_id=$2 AND i.asset_version_id=$3
			 AND (result.parent_asset_version_id=$4 OR EXISTS(SELECT FROM photo.asset_version_sources src WHERE src.tenant_id=result.tenant_id AND src.version_id=result.id AND src.source_version_id=$4))`, tid, collectionID, req.ResultAssetVersionID, req.SourceAssetVersionID).Scan(&sourceKind)
			if errors.Is(queryErr, pgx.ErrNoRows) {
				return idempotency.Response{}, domain.NewError("SOURCE_VERSION_MISMATCH", 409)
			}
			if queryErr != nil {
				return idempotency.Response{}, queryErr
			}
			pairReq := PairCreate{BeforeAssetVersionID: req.SourceAssetVersionID, AfterAssetVersionID: req.ResultAssetVersionID, SessionID: req.SessionID, Confirmed: true, Included: true}
			if queryErr = validatePair(ctx, tx, tid, styleID, pairReq); queryErr != nil {
				return idempotency.Response{}, queryErr
			}
			pair, insertErr := scanPair(tx.QueryRow(ctx, `INSERT INTO photo.training_pairs AS p(tenant_id,training_set_id,before_asset_version_id,after_asset_version_id,session_id,confirmed,included,source_kind) VALUES($1,$2,$3,$4,$5,true,true,$6) RETURNING `+pairFields, tid, req.TrainingSetID, req.SourceAssetVersionID, req.ResultAssetVersionID, req.SessionID, sourceKind))
			if insertErr != nil {
				return idempotency.Response{}, insertErr
			}
			if _, insertErr = tx.Exec(ctx, `UPDATE photo.training_sets SET revision=revision+1 WHERE tenant_id=$1 AND id=$2`, tid, req.TrainingSetID); insertErr != nil {
				return idempotency.Response{}, insertErr
			}
			body, insertErr := canonical.Marshal(pair)
			return idempotency.Response{Status: 201, Body: body, ResourceID: &pair.ID}, insertErr
		})
		return e
	})
	return out, err
}
func (s Service) UpdatePair(ctx context.Context, who auth.Identity, tid, setID, pairID uuid.UUID, expected int64, patch PairPatch) (TrainingPair, error) {
	var out TrainingPair
	if expected < 1 || patch.ExpectedRevision != expected {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		var styleID, projectID uuid.UUID
		var state string
		e := tx.QueryRow(ctx, `SELECT t.style_id,t.state,s.project_id FROM photo.training_sets t JOIN photo.styles s ON s.tenant_id=t.tenant_id AND s.id=t.style_id WHERE t.tenant_id=$1 AND t.id=$2 FOR UPDATE OF t`, tid, setID).Scan(&styleID, &state, &projectID)
		if errors.Is(e, pgx.ErrNoRows) {
			return domain.NewError("NOT_FOUND", 404)
		}
		if e != nil {
			return e
		}
		if state != "draft" {
			return domain.NewError("REVISION_CONFLICT", 409)
		}
		if _, e = project.Authorize(ctx, tx, tid, projectID, who.UserID, role, domain.Editor, false); e != nil {
			return e
		}
		current, e := scanPair(tx.QueryRow(ctx, `SELECT `+pairFields+` FROM photo.training_pairs p WHERE p.tenant_id=$1 AND p.training_set_id=$2 AND p.id=$3 FOR UPDATE`, tid, setID, pairID))
		if errors.Is(e, pgx.ErrNoRows) {
			return domain.NewError("NOT_FOUND", 404)
		}
		if e != nil {
			return e
		}
		if current.Revision != strconv.FormatInt(expected, 10) {
			return domain.NewError("REVISION_CONFLICT", 409)
		}
		if patch.Confirmed != nil {
			current.Confirmed = *patch.Confirmed
		}
		if patch.Included != nil {
			current.Included = *patch.Included
		}
		if patch.IgnoreMaskArtifactID.Set {
			current.IgnoreMaskArtifactID = patch.IgnoreMaskArtifactID.Value
		}
		if current.IgnoreMaskArtifactID != nil {
			var valid bool
			if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT FROM photo.artifacts WHERE tenant_id=$1 AND id=$2 AND state='ready' AND role IN ('mask','manual_mask','rasterized_mask'))`, tid, *current.IgnoreMaskArtifactID).Scan(&valid); e != nil {
				return e
			}
			if !valid {
				return domain.NewError("NOT_FOUND", 404)
			}
		}
		out, e = scanPair(tx.QueryRow(ctx, `UPDATE photo.training_pairs AS p SET confirmed=$4,included=$5,ignore_mask_artifact_id=$6,validation_status='pending',revision=revision+1 WHERE tenant_id=$1 AND training_set_id=$2 AND id=$3 RETURNING `+pairFields, tid, setID, pairID, current.Confirmed, current.Included, current.IgnoreMaskArtifactID))
		if e != nil {
			return e
		}
		_, e = tx.Exec(ctx, `UPDATE photo.training_sets SET revision=revision+1 WHERE tenant_id=$1 AND id=$2`, tid, setID)
		return e
	})
	return out, err
}
