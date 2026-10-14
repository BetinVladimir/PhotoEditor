package upload

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"path/filepath"
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
	"photoeditor/backend/internal/project"
 "photoeditor/backend/internal/rawdevelopment"
 "photoeditor/backend/internal/v2qualification"
	"photoeditor/backend/internal/tenant"
)

type MultipartStore interface {
	BeginMultipart(context.Context, string, string) (string, error)
	AbortMultipart(context.Context, string, string) error
	SignUploadParts(context.Context, string, string, []int, time.Duration) ([]objectstore.PartURL, error)
}

// RAWAdmission must resolve current physical qualification using this transaction.
// rawdevelopment.Source implements this interface; HTTP input never supplies it.
type RAWAdmission interface {
 Admit(context.Context,rawdevelopment.ReportRows,uuid.UUID,uuid.UUID,uuid.UUID,uuid.UUID,*rawdevelopment.Policy)(*rawdevelopment.Policy,error)
}

type Service struct {
 RAWAdmission RAWAdmission
	DB      *pgxpool.Pool
	Objects MultipartStore
	Now     func() time.Time
}

type BeginRequest struct {
 RAWDevelopment *rawdevelopment.Policy `json:"raw_development,omitempty"`
	ProjectID     uuid.UUID  `json:"project_id"`
	CollectionID  *uuid.UUID `json:"collection_id,omitempty"`
	Filename      string     `json:"filename"`
	Bytes         int64      `json:"bytes"`
	Format        string     `json:"format"`
	AssumeProfile *string    `json:"assume_profile,omitempty"`
	SHA256        *string    `json:"sha256,omitempty"`
}

type Session struct {
	ID           uuid.UUID  `json:"id"`
	ProjectID    uuid.UUID  `json:"project_id"`
	CollectionID *uuid.UUID `json:"collection_id,omitempty"`
	State        string     `json:"state"`
	Revision     string     `json:"revision"`
	Filename     string     `json:"filename"`
	ExpectedSize int64      `json:"expected_size"`
	ActualSize   *int64     `json:"actual_size,omitempty"`
	PartBytes    int64      `json:"part_bytes"`
	PartsTotal   int        `json:"parts_total"`
	ExpiresAt    time.Time  `json:"expires_at"`
	objectKey    string
	multipartID  *string
	mediaType    string
}

type PartRequest struct {
	PartNumbers []int `json:"part_numbers"`
}

type PartURLs struct {
	UploadID uuid.UUID             `json:"upload_id"`
	Items    []objectstore.PartURL `json:"items"`
}

type CompleteRequest struct {
	Parts        []objectstore.UploadedPart `json:"parts"`
	ExpectedSize int64                      `json:"expected_size"`
}

type Accepted struct {
	JobID  uuid.UUID `json:"job_id"`
	Status string    `json:"status"`
}

type ModerationRequest struct {
	Decision         string `json:"decision"`
	ExpectedRevision int64  `json:"expected_revision"`
}

type ModeratedItem struct {
	ID             uuid.UUID `json:"id"`
	CollectionID   uuid.UUID `json:"collection_id"`
	AssetID        uuid.UUID `json:"asset_id"`
	AssetVersionID uuid.UUID `json:"asset_version_id"`
	Visibility     string    `json:"visibility"`
	Moderation     string    `json:"moderation_state"`
	Position       int64     `json:"position"`
}

func normalizeModeration(request ModerationRequest) (ModerationRequest, error) {
	request.Decision = strings.ToLower(strings.TrimSpace(request.Decision))
	if (request.Decision != "accept" && request.Decision != "reject") || request.ExpectedRevision < 1 || request.ExpectedRevision > 9007199254740991 {
		return request, domain.NewError("VALIDATION_FAILED", 422)
	}
	return request, nil
}

var formats = map[string]string{
	"jpeg": "image/jpeg", "jpg": "image/jpeg", "png": "image/png",
	"tiff": "image/tiff", "tif": "image/tiff",
	"dng": "image/x-adobe-dng", "cr2": "image/x-canon-cr2", "nef": "image/x-nikon-nef",
	"arw": "image/x-sony-arw", "raf": "image/x-fuji-raf", "orf": "image/x-olympus-orf", "rw2": "image/x-panasonic-rw2",
}

func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func normalize(request BeginRequest) (BeginRequest, string, error) {
	request.Filename = strings.TrimSpace(request.Filename)
	request.Format = strings.ToLower(strings.TrimSpace(request.Format))
	media, ok := formats[request.Format]
	if request.ProjectID == uuid.Nil || request.Bytes < 1 || request.Bytes > objectstore.MaxUploadBytes || !ok || request.Filename == "" || len([]rune(request.Filename)) > 255 || strings.ContainsAny(request.Filename, "\x00/\\") {
		return request, "", domain.NewError("VALIDATION_FAILED", 422)
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(request.Filename)), ".")
	if ext != request.Format && !(request.Format == "jpeg" && ext == "jpg") && !(request.Format == "jpg" && ext == "jpeg") && !(request.Format == "tiff" && ext == "tif") && !(request.Format == "tif" && ext == "tiff") {
		return request, "", domain.NewError("VALIDATION_FAILED", 422)
	}
	if request.CollectionID != nil && *request.CollectionID == uuid.Nil {
		return request, "", domain.NewError("VALIDATION_FAILED", 422)
	}
    if !rawdevelopment.Valid(request.RAWDevelopment) { return request,"",domain.NewError("VALIDATION_FAILED",422) }
    if request.RAWDevelopment != nil {
        if request.Format=="jpeg" || request.Format=="jpg" || request.Format=="png" || request.Format=="tiff" || request.Format=="tif" { return request,"",domain.NewError("VALIDATION_FAILED",422) }
        copy:=*request.RAWDevelopment
        request.RAWDevelopment=&copy
    }
	if request.AssumeProfile != nil {
		profile := strings.ToLower(strings.TrimSpace(*request.AssumeProfile))
		switch profile {
		case "srgb", "srgb_v1":
			profile = "srgb_v1"
		case "linear_srgb", "linear_srgb_v1":
			profile = "linear_srgb_v1"
		case "work_rec2020_gamma24", "work_rec2020_gamma24_v1":
			profile = "work_rec2020_gamma24_v1"
		default:
			return request, "", domain.NewError("VALIDATION_FAILED", 422)
		}
		request.AssumeProfile = &profile
	}
	if request.SHA256 != nil {
		value := strings.ToLower(*request.SHA256)
		if len(value) != 64 {
			return request, "", domain.NewError("VALIDATION_FAILED", 422)
		}
		for _, c := range value {
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
				return request, "", domain.NewError("VALIDATION_FAILED", 422)
			}
		}
		request.SHA256 = &value
	}
	return request, media, nil
}

func scan(row pgx.Row) (Session, error) {
	var out Session
	var revision int64
	err := row.Scan(&out.ID, &out.ProjectID, &out.CollectionID, &out.State, &revision, &out.Filename,
		&out.ExpectedSize, &out.ActualSize, &out.ExpiresAt, &out.objectKey, &out.multipartID, &out.mediaType)
	if err != nil {
		return out, err
	}
	out.Revision = strconv.FormatInt(revision, 10)
	out.PartBytes = objectstore.PartBytes
	out.PartsTotal = int((out.ExpectedSize + objectstore.PartBytes - 1) / objectstore.PartBytes)
	return out, nil
}

const sessionFields = "id,project_id,collection_id,state,revision,filename,expected_size,actual_size,expires_at,object_key,multipart_id,media_type"

func load(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, lock bool) (Session, error) {
	suffix := ""
	if lock {
		suffix = " FOR UPDATE"
	}
	out, err := scan(tx.QueryRow(ctx, "SELECT "+sessionFields+" FROM photo.upload_sessions WHERE tenant_id=$1 AND id=$2"+suffix, tenantID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return out, domain.NewError("NOT_FOUND", 404)
	}
	return out, err
}

func authorizeSession(ctx context.Context, tx pgx.Tx, who auth.Identity, tenantID uuid.UUID, role domain.Role, session Session, lockProject bool) error {
	_, err := project.Authorize(ctx, tx, tenantID, session.ProjectID, who.UserID, role, domain.Editor, lockProject)
	return err
}

func (s Service) Begin(ctx context.Context, who auth.Identity, tenantID uuid.UUID, key string, raw BeginRequest) (Session, error) {
	request, mediaType, err := normalize(raw)
	if err != nil {
		return Session{}, err
	}
	if s.DB == nil || s.Objects == nil {
		return Session{}, errors.New("upload dependencies unavailable")
	}
	var uploadID uuid.UUID
	err = tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
        // Qualification is deliberately outside the idempotency factory:
        // a stored receipt cannot bypass a revoked qualification on replay.
        if request.RAWDevelopment!=nil {
            if _,err:=project.Authorize(ctx,tx,tenantID,request.ProjectID,who.UserID,role,domain.Editor,true);err!=nil{return err}
            admitted,err:=s.admitRAW(ctx,tx,who,tenantID,request.ProjectID,request.RAWDevelopment)
            if err!=nil{return err}
            request.RAWDevelopment=admitted
        }
		response, err := idempotency.Use(ctx, tx, idempotency.Scope{Tenant: tenantID, Principal: who.UserID, PrincipalType: "user", Route: "Upload.Begin", Key: key}, request, s.now().Add(30*24*time.Hour), func() (idempotency.Response, error) {
			if _, err := project.Authorize(ctx, tx, tenantID, request.ProjectID, who.UserID, role, domain.Editor, true); err != nil {
				return idempotency.Response{}, err
			}
			if request.CollectionID != nil {
				var valid bool
				if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT FROM photo.collections WHERE tenant_id=$1 AND id=$2 AND project_id=$3 AND deleted_at IS NULL)", tenantID, *request.CollectionID, request.ProjectID).Scan(&valid); err != nil {
					return idempotency.Response{}, err
				}
				if !valid {
					return idempotency.Response{}, domain.NewError("NOT_FOUND", 404)
				}
			}
			var storage, reserved, files, reservedFiles, byteLimit, fileLimit int64
			if err := tx.QueryRow(ctx, `SELECT u.storage_bytes,u.reserved_bytes,u.source_files,u.reserved_files,
			 (t.quotas->>'storage_bytes')::bigint,(t.quotas->>'source_files')::bigint
			 FROM photo.tenant_usage u JOIN photo.tenants t ON t.id=u.tenant_id WHERE u.tenant_id=$1 FOR UPDATE OF u`, tenantID).Scan(&storage, &reserved, &files, &reservedFiles, &byteLimit, &fileLimit); err != nil {
				return idempotency.Response{}, err
			}
			if byteLimit < 1 || fileLimit < 1 || storage > math.MaxInt64-reserved || storage+reserved > byteLimit-request.Bytes || files > math.MaxInt64-reservedFiles || files+reservedFiles >= fileLimit {
				return idempotency.Response{}, domain.NewError("QUOTA_EXCEEDED", 422)
			}
			uploadID, reservationID := uuid.New(), uuid.New()
			objectKey, err := objectstore.StagingKey(tenantID, uploadID)
			if err != nil {
				return idempotency.Response{}, err
			}
			expires := s.now().Add(24 * time.Hour)
			if _, err = tx.Exec(ctx, `INSERT INTO photo.upload_sessions(id,tenant_id,project_id,collection_id,principal_type,principal_id,
			 expected_size,filename,assume_profile,object_key,state,expires_at,reservation_id,media_type,expected_sha256,raw_development)
			 VALUES($1,$2,$3,$4,'user',$5,$6,$7,$8,$9,'initializing',$10,$11,$12,$13,$14)`, uploadID, tenantID, request.ProjectID, request.CollectionID, who.UserID, request.Bytes, request.Filename, request.AssumeProfile, objectKey, expires, reservationID, mediaType, request.SHA256, rawPolicyValue(request.RAWDevelopment)); err != nil {
				return idempotency.Response{}, err
			}
			if _, err = tx.Exec(ctx, `INSERT INTO photo.quota_reservations(id,tenant_id,principal_type,principal_id,holder_type,holder_id,bytes,files,state,expires_at)
			 VALUES($1,$2,'user',$3,'upload',$4,$5,1,'reserved',$6)`, reservationID, tenantID, who.UserID, uploadID, request.Bytes, expires); err != nil {
				return idempotency.Response{}, err
			}
			if _, err = tx.Exec(ctx, "UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+$2,reserved_files=reserved_files+1 WHERE tenant_id=$1", tenantID, request.Bytes); err != nil {
				return idempotency.Response{}, err
			}
			body, err := canonical.Marshal(map[string]string{"upload_id": uploadID.String()})
			return idempotency.Response{Status: 201, Body: body, ResourceID: &uploadID}, err
		})
		if err != nil {
			return err
		}
		if response.ResourceID == nil {
			return errors.New("upload idempotency record missing resource")
		}
		uploadID = *response.ResourceID
		current, err := load(ctx, tx, tenantID, uploadID, false)
		if err != nil {
			return err
		}
		return authorizeSession(ctx, tx, who, tenantID, role, current, false)
	})
	if err != nil {
		return Session{}, err
	}
	return s.ensureMultipart(ctx, who, tenantID, uploadID)
}

func (s Service) ensureMultipart(ctx context.Context, who auth.Identity, tenantID, id uuid.UUID) (Session, error) {
	current, err := s.Get(ctx, who, tenantID, id)
	if err != nil || current.State != "initializing" {
		return current, err
	}
	multipartID, err := s.Objects.BeginMultipart(ctx, current.objectKey, current.mediaType)
	if err != nil {
		return Session{}, err
	}
	attached := false
	err = tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		locked, err := load(ctx, tx, tenantID, id, true)
		if err != nil {
			return err
		}
		if err = authorizeSession(ctx, tx, who, tenantID, role, locked, false); err != nil {
			return err
		}
		if locked.State == "initializing" && locked.multipartID == nil && locked.ExpiresAt.After(s.now()) {
			if _, err = tx.Exec(ctx, "UPDATE photo.upload_sessions SET multipart_id=$3,state='uploading',revision=revision+1 WHERE tenant_id=$1 AND id=$2", tenantID, id, multipartID); err != nil {
				return err
			}
			attached = true
		}
		return nil
	})
	if err != nil || !attached {
		abortCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		_ = s.Objects.AbortMultipart(abortCtx, current.objectKey, multipartID)
		cancel()
		if err != nil {
			return Session{}, err
		}
	}
	return s.Get(ctx, who, tenantID, id)
}

func (s Service) Get(ctx context.Context, who auth.Identity, tenantID, id uuid.UUID) (Session, error) {
	var out Session
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		var err error
		out, err = load(ctx, tx, tenantID, id, false)
		if err != nil {
			return err
		}
		return authorizeSession(ctx, tx, who, tenantID, role, out, false)
	})
	return out, err
}

func (s Service) SignParts(ctx context.Context, who auth.Identity, tenantID, id uuid.UUID, request PartRequest) (PartURLs, error) {
	if len(request.PartNumbers) < 1 || len(request.PartNumbers) > 100 {
		return PartURLs{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	session, err := s.Get(ctx, who, tenantID, id)
	if err != nil {
		return PartURLs{}, err
	}
	if session.State != "uploading" || session.multipartID == nil || !session.ExpiresAt.After(s.now()) {
		return PartURLs{}, domain.NewError("UPLOAD_STATE_INVALID", 409)
	}
	if !validPartNumbers(request.PartNumbers, session) {
		return PartURLs{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	items, err := s.Objects.SignUploadParts(ctx, session.objectKey, *session.multipartID, request.PartNumbers, 10*time.Minute)
	if errors.Is(err, objectstore.ErrInvalid) {
		return PartURLs{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	return PartURLs{UploadID: id, Items: items}, err
}

func validPartNumbers(numbers []int, session Session) bool {
	if len(numbers) < 1 || len(numbers) > 100 {
		return false
	}
	seen := make(map[int]bool, len(numbers))
	for _, number := range numbers {
		if number < 1 || number > session.PartsTotal || seen[number] {
			return false
		}
		seen[number] = true
	}
	return true
}

func validCompletion(request CompleteRequest, session Session) bool {
	if request.ExpectedSize != session.ExpectedSize || len(request.Parts) != session.PartsTotal || len(request.Parts) < 1 || len(request.Parts) > objectstore.MaxUploadParts {
		return false
	}
	for i, part := range request.Parts {
		if part.PartNumber != i+1 || len(part.ETag) < 1 || len(part.ETag) > 256 || strings.ContainsAny(part.ETag, "\r\n") {
			return false
		}
	}
	return true
}

func (s Service) Complete(ctx context.Context, who auth.Identity, tenantID, id uuid.UUID, key string, request CompleteRequest) (Accepted, error) {
	var out Accepted
	if s.DB == nil {
		return out, errors.New("upload database unavailable")
	}
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		response, err := idempotency.Use(ctx, tx, idempotency.Scope{Tenant: tenantID, Principal: who.UserID, PrincipalType: "user", Route: "Upload.Complete/" + id.String(), Key: key}, request, s.now().Add(30*24*time.Hour), func() (idempotency.Response, error) {
			session, err := load(ctx, tx, tenantID, id, true)
			if err != nil {
				return idempotency.Response{}, err
			}
			if err = authorizeSession(ctx, tx, who, tenantID, role, session, false); err != nil {
				return idempotency.Response{}, err
			}
			if session.State != "uploading" || session.multipartID == nil || !session.ExpiresAt.After(s.now()) {
				return idempotency.Response{}, domain.NewError("UPLOAD_STATE_INVALID", 409)
			}
			if !validCompletion(request, session) {
				return idempotency.Response{}, domain.NewError("VALIDATION_FAILED", 422)
			}
			for _, part := range request.Parts {
				if _, err = tx.Exec(ctx, `INSERT INTO photo.upload_parts(tenant_id,upload_session_id,part_no,etag,state)
				 VALUES($1,$2,$3,$4,'completed')`, tenantID, id, part.PartNumber, part.ETag); err != nil {
					return idempotency.Response{}, err
				}
			}
			artifactID, snapshotID, runID := uuid.New(), uuid.New(), uuid.New()
			if _, err = tx.Exec(ctx, `UPDATE photo.upload_sessions SET state='completing',next_completion_at=clock_timestamp(),
			 staging_candidate_id=$3,ingest_snapshot_id=$4,ingest_run_id=$5,revision=revision+1 WHERE tenant_id=$1 AND id=$2`, tenantID, id, artifactID, snapshotID, runID); err != nil {
				return idempotency.Response{}, err
			}
			body, err := canonical.Marshal(Accepted{JobID: id, Status: "queued"})
			return idempotency.Response{Status: 202, Body: body, ResourceID: &id}, err
		})
		if err != nil {
			return err
		}
		if response.ResourceID == nil || *response.ResourceID != id {
			return errors.New("upload completion idempotency mismatch")
		}
		// Authorization is required even for a cached idempotency response.
		session, err := load(ctx, tx, tenantID, id, false)
		if err != nil {
			return err
		}
		if err = authorizeSession(ctx, tx, who, tenantID, role, session, false); err != nil {
			return err
		}
		return json.Unmarshal(response.Body, &out)
	})
	return out, err
}

func (s Service) Abort(ctx context.Context, who auth.Identity, tenantID, id uuid.UUID) (Session, error) {
	var out Session
	if s.DB == nil || s.Objects == nil {
		return out, errors.New("upload dependencies unavailable")
	}
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		hint, err := load(ctx, tx, tenantID, id, false)
		if err != nil {
			return err
		}
		if hint.State == "aborted" {
			out = hint
			return authorizeSession(ctx, tx, who, tenantID, role, hint, false)
		}
		if hint.State != "initializing" && hint.State != "uploading" {
			return domain.NewError("UPLOAD_STATE_INVALID", 409)
		}
		if _, err = tx.Exec(ctx, "SELECT id FROM photo.tenant_usage WHERE tenant_id=$1 FOR UPDATE", tenantID); err != nil {
			return err
		}
		if err = authorizeSession(ctx, tx, who, tenantID, role, hint, true); err != nil {
			return err
		}
		locked, err := load(ctx, tx, tenantID, id, true)
		if err != nil {
			return err
		}
		if locked.State == "aborted" {
			out = locked
			return nil
		}
		if locked.State != "initializing" && locked.State != "uploading" {
			return domain.NewError("UPLOAD_STATE_INVALID", 409)
		}
		var reservationBytes int64
		var reservationState string
		if err = tx.QueryRow(ctx, `SELECT bytes,state FROM photo.quota_reservations
		 WHERE tenant_id=$1 AND id=(SELECT reservation_id FROM photo.upload_sessions WHERE tenant_id=$1 AND id=$2) FOR UPDATE`, tenantID, id).Scan(&reservationBytes, &reservationState); err != nil {
			return err
		}
		if reservationState != "reserved" || reservationBytes != locked.ExpectedSize {
			return errors.New("upload reservation invariant failed")
		}
		tag, err := tx.Exec(ctx, `UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-$2
		 WHERE tenant_id=$1 AND reserved_bytes>=$2 AND reserved_files>=1`, tenantID, reservationBytes)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return errors.New("upload usage invariant failed")
		}
		tag, err = tx.Exec(ctx, `UPDATE photo.quota_reservations SET state='released'
		 WHERE tenant_id=$1 AND holder_type='upload' AND holder_id=$2 AND state='reserved'`, tenantID, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return errors.New("upload reservation release failed")
		}
		if _, err = tx.Exec(ctx, "UPDATE photo.upload_sessions SET state='aborted',revision=revision+1 WHERE tenant_id=$1 AND id=$2", tenantID, id); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO photo.audit_log(tenant_id,actor_type,actor_id,action,resource_type,resource_id,request_id,details)
		 VALUES($1,'user',$2,'upload.abort','upload',$3,$4,'{}')`, tenantID, who.UserID, id, uuid.New()); err != nil {
			return err
		}
		out, err = load(ctx, tx, tenantID, id, false)
		return err
	})
	if err != nil {
		return out, err
	}
	if out.multipartID != nil {
		abortCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		err = s.Objects.AbortMultipart(abortCtx, out.objectKey, *out.multipartID)
		cancel()
		if err != nil && !errors.Is(err, objectstore.ErrMissing) {
			return out, err
		}
	}
	return out, nil
}

// Moderate decides a verified guest upload membership. Publication remains a
// separate explicit collection action; acceptance alone never exposes bytes.
func (s Service) Moderate(ctx context.Context, who auth.Identity, tenantID, id uuid.UUID, request ModerationRequest) (ModeratedItem, error) {
	var out ModeratedItem
	request, err := normalizeModeration(request)
	if err != nil {
		return out, err
	}
	if s.DB == nil {
		return out, errors.New("upload database unavailable")
	}
	err = tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		hint, err := load(ctx, tx, tenantID, id, false)
		if err != nil {
			return err
		}
		if err = authorizeSession(ctx, tx, who, tenantID, role, hint, true); err != nil {
			return err
		}
		if hint.CollectionID == nil {
			return domain.NewError("UPLOAD_STATE_INVALID", 409)
		}
		var collectionRevision int64
		if err = tx.QueryRow(ctx, `SELECT revision FROM photo.collections
			WHERE tenant_id=$1 AND id=$2 AND project_id=$3 AND deleted_at IS NULL FOR UPDATE`, tenantID, *hint.CollectionID, hint.ProjectID).Scan(&collectionRevision); errors.Is(err, pgx.ErrNoRows) {
			return domain.NewError("NOT_FOUND", 404)
		} else if err != nil {
			return err
		}
		if collectionRevision >= 9007199254740991 {
			return errors.New("collection revision exhausted")
		}
		locked, err := load(ctx, tx, tenantID, id, true)
		if err != nil {
			return err
		}
		if locked.State != "verified" || locked.CollectionID == nil || locked.multipartID == nil || locked.Revision != strconv.FormatInt(request.ExpectedRevision, 10) {
			return domain.NewError("REVISION_CONFLICT", 409)
		}
		var principalType, current string
		var versionID uuid.UUID
		err = tx.QueryRow(ctx, `SELECT u.principal_type,i.id,i.collection_id,i.asset_id,i.asset_version_id,i.visibility,i.moderation_state,i.position
			FROM photo.upload_sessions u
			JOIN photo.ingest_upload_bindings b ON b.tenant_id=u.tenant_id AND b.upload_id=u.id AND b.run_id=u.ingest_run_id
			JOIN photo.collection_items i ON i.tenant_id=b.tenant_id AND i.id=b.item_id AND i.collection_id=u.collection_id
			WHERE u.tenant_id=$1 AND u.id=$2 AND u.share_id IS NOT NULL`, tenantID, id).Scan(&principalType, &out.ID, &out.CollectionID, &out.AssetID, &out.AssetVersionID, &out.Visibility, &current, &out.Position)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.NewError("UPLOAD_STATE_INVALID", 409)
		}
		if err != nil {
			return err
		}
		if principalType != "guest" || current != "pending_review" {
			return domain.NewError("UPLOAD_STATE_INVALID", 409)
		}
		decision := map[string]string{"accept": "accepted", "reject": "rejected"}[request.Decision]
		out.Moderation = decision
		out.Visibility = "hidden"
		versionID = out.AssetVersionID
		tag, err := tx.Exec(ctx, `UPDATE photo.collection_items SET moderation_state=$3,visibility='hidden'
			WHERE tenant_id=$1 AND id=$2 AND moderation_state='pending_review'`, tenantID, out.ID, decision)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return domain.NewError("REVISION_CONFLICT", 409)
		}
		if _, err = tx.Exec(ctx, "UPDATE photo.asset_versions SET review_state=$3 WHERE tenant_id=$1 AND id=$2", tenantID, versionID, decision); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "UPDATE photo.collections SET revision=revision+1 WHERE tenant_id=$1 AND id=$2", tenantID, out.CollectionID); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "UPDATE photo.upload_sessions SET revision=revision+1 WHERE tenant_id=$1 AND id=$2", tenantID, id); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO photo.audit_log(tenant_id,actor_type,actor_id,action,resource_type,resource_id,request_id,details)
			VALUES($1,'user',$2,$3,'upload',$4,$5,jsonb_build_object('item_id',$6,'decision',$7))`, tenantID, who.UserID, "upload.moderate."+request.Decision, id, uuid.New(), out.ID, decision)
		return err
	})
	return out, err
}

// MarshalJSON prevents internal object and multipart identifiers from being
// exposed if Session is ever encoded through an interface value.
func (s Session) MarshalJSON() ([]byte, error) {
	type public Session
	return json.Marshal(struct{ public }{public(s)})
}

// Preserve SQL NULL for legacy uploads rather than the JSON null literal.
func rawPolicyValue(p *rawdevelopment.Policy) any {
 if p==nil {return nil}
 return p
}

func (s Service) admitRAW(ctx context.Context,tx rawdevelopment.ReportRows,who auth.Identity,tenantID,projectID uuid.UUID,policy *rawdevelopment.Policy)(*rawdevelopment.Policy,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if policy==nil{return nil,nil}
 if s.RAWAdmission==nil{return nil,domain.NewError("QUALIFICATION_UNAVAILABLE",503)}
 expected:=*policy
 input:=expected
 admitted,err:=s.RAWAdmission.Admit(ctx,tx,tenantID,projectID,who.UserID,who.SessionID,&input)
 if errors.Is(err,v2qualification.ErrUnqualified){failure:=domain.NewError("QUALIFICATION_UNAVAILABLE",503);failure.Cause=err;return nil,failure}
 if err!=nil{return nil,err}
 if admitted==nil || !rawdevelopment.Valid(admitted) || *admitted!=expected{return nil,domain.NewError("QUALIFICATION_UNAVAILABLE",503)}
 if err=ctx.Err();err!=nil{return nil,err}
 owned:=*admitted
 return &owned,nil
}
