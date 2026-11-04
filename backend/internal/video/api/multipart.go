package videoapi

import (
	"context"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/video/worker"
)

const partURLTTL = time.Hour

// Multipart is the object store side of resumable uploads (worker.S3Store implements it).
type Multipart interface {
	BeginMultipart(ctx context.Context, bucket, key, contentType string) (string, error)
	SignPart(ctx context.Context, bucket, key, uploadID string, part int, ttl time.Duration) (string, error)
	ListParts(ctx context.Context, bucket, key, uploadID string) ([]worker.ListedPart, error)
	CompleteMultipart(ctx context.Context, bucket, key, uploadID string, parts []worker.ListedPart) (string, error)
	AbortMultipart(ctx context.Context, bucket, key, uploadID string) error
	HashObject(ctx context.Context, bucket, key string) (string, int64, error)
	Remove(ctx context.Context, bucket, key string) error
}

func (s *Service) loadUpload(ctx context.Context, who auth.Identity, tid, uid uuid.UUID) (upload, error) {
	var up upload
	err := s.with(ctx, who, tid, domain.Editor, func(tx pgx.Tx) error {
		var raw []byte
		if err := tx.QueryRow(ctx, `SELECT photo.get_video_upload($1,$2)::text`, tid, uid).Scan(&raw); err != nil || raw == nil {
			return notFound()
		}
		return jsonUnmarshal(raw, &up)
	})
	if err == nil && up.Principal != who.UserID {
		err = notFound()
	}
	return up, err
}

func (s *Service) mpReady() error {
	if s.Multipart == nil || s.Scheduler == nil {
		return domain.NewError("MULTIPART_UNAVAILABLE", 501)
	}
	return nil
}

// StartMultipart opens the multipart upload in the object store and binds it to the session.
func (s *Service) StartMultipart(ctx context.Context, who auth.Identity, tid, uid uuid.UUID) (map[string]any, error) {
	if err := s.mpReady(); err != nil {
		return nil, err
	}
	up, err := s.loadUpload(ctx, who, tid, uid)
	if err != nil {
		return nil, err
	}
	if up.State != "initializing" && up.State != "uploading" {
		return nil, domain.NewError("VIDEO_UPLOAD_STATE", 409)
	}
	id := ""
	if up.Multipart != nil {
		id = *up.Multipart
	} else {
		mt := mime.TypeByExtension(strings.ToLower(filepath.Ext(up.Filename)))
		if mt == "" {
			mt = "application/octet-stream"
		}
		if id, err = s.Multipart.BeginMultipart(ctx, s.Bucket, up.ObjectKey, mt); err != nil {
			return nil, err
		}
		var ok bool
		err = s.with(ctx, who, tid, domain.Editor, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT photo.bind_video_upload_multipart($1,$2,$3)`, tid, uid, id).Scan(&ok)
		})
		if err != nil || !ok {
			_ = s.Multipart.AbortMultipart(ctx, s.Bucket, up.ObjectKey, id)
			if err == nil {
				err = domain.NewError("VIDEO_UPLOAD_STATE", 409)
			}
			return nil, err
		}
	}
	return map[string]any{"upload_id": uid, "part_bytes": partBytes, "parts_total": up.Parts}, nil
}

// SignParts returns presigned PUT URLs for the requested part numbers.
func (s *Service) SignParts(ctx context.Context, who auth.Identity, tid, uid uuid.UUID, nums []int) (map[string]any, error) {
	if err := s.mpReady(); err != nil {
		return nil, err
	}
	up, err := s.loadUpload(ctx, who, tid, uid)
	if err != nil {
		return nil, err
	}
	if up.State != "uploading" || up.Multipart == nil {
		return nil, domain.NewError("VIDEO_UPLOAD_STATE", 409)
	}
	if len(nums) == 0 || len(nums) > 100 {
		return nil, bad("VIDEO_UPLOAD_INVALID")
	}
	urls := make([]map[string]any, 0, len(nums))
	for _, n := range nums {
		if n < 1 || n > up.Parts {
			return nil, bad("VIDEO_UPLOAD_INVALID")
		}
		u, err := s.Multipart.SignPart(ctx, s.Bucket, up.ObjectKey, *up.Multipart, n, partURLTTL)
		if err != nil {
			return nil, err
		}
		urls = append(urls, map[string]any{"part_no": n, "url": u})
	}
	return map[string]any{"urls": urls, "expires_in": int(partURLTTL.Seconds())}, nil
}

// CompleteMultipart assembles the object from the parts the store holds, checks size and hash against the declaration
// and hands the upload on to the scheduler like PutContent does.
func (s *Service) CompleteMultipart(ctx context.Context, who auth.Identity, tid, uid uuid.UUID) (Media, error) {
	if err := s.mpReady(); err != nil {
		return Media{}, err
	}
	up, err := s.loadUpload(ctx, who, tid, uid)
	if err != nil {
		return Media{}, err
	}
	if up.State != "uploading" || up.Multipart == nil {
		return Media{}, domain.NewError("VIDEO_UPLOAD_STATE", 409)
	}
	held, err := s.Multipart.ListParts(ctx, s.Bucket, up.ObjectKey, *up.Multipart)
	if err != nil {
		return Media{}, err
	}
	if len(held) != up.Parts {
		return Media{}, bad("VIDEO_UPLOAD_PARTS")
	}
	etags := make([]string, up.Parts)
	for i, p := range held {
		want := int64(partBytes)
		if i == up.Parts-1 {
			want = up.Bytes - int64(up.Parts-1)*partBytes
		}
		if p.No != i+1 || p.Size != want {
			return Media{}, bad("VIDEO_UPLOAD_PARTS")
		}
		etags[i] = strings.Trim(p.ETag, `"`)
	}
	version, err := s.Multipart.CompleteMultipart(ctx, s.Bucket, up.ObjectKey, *up.Multipart, held)
	if err != nil {
		return Media{}, err
	}
	sha, n, err := s.Multipart.HashObject(ctx, s.Bucket, up.ObjectKey)
	if err != nil {
		return Media{}, err
	}
	if n != up.Bytes || sha != up.SHA256 {
		_ = s.Multipart.Remove(ctx, s.Bucket, up.ObjectKey)
		_ = s.with(ctx, who, tid, domain.Editor, func(tx pgx.Tx) error {
			var ok bool
			return tx.QueryRow(ctx, `SELECT photo.abort_video_upload($1,$2)`, tid, uid).Scan(&ok)
		})
		return Media{}, bad("VIDEO_UPLOAD_MISMATCH")
	}
	return s.finalize(ctx, who, tid, up, *up.Multipart, version, etags)
}

func (h handler) startMultipart(w http.ResponseWriter, r *http.Request) {
	who, tid, ids, ok := h.begin(w, r, true, "u")
	if !ok {
		return
	}
	out, err := h.svc.StartMultipart(r.Context(), who, tid, ids[0])
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 200, out)
}

func (h handler) signParts(w http.ResponseWriter, r *http.Request) {
	who, tid, ids, ok := h.begin(w, r, true, "u")
	if !ok {
		return
	}
	var req struct {
		Parts []int `json:"part_numbers"`
	}
	if err := decode(r, &req, 4096); err != nil {
		writeErr(w, r, err)
		return
	}
	out, err := h.svc.SignParts(r.Context(), who, tid, ids[0], req.Parts)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 200, out)
}

func (h handler) completeMultipart(w http.ResponseWriter, r *http.Request) {
	who, tid, ids, ok := h.begin(w, r, true, "u")
	if !ok {
		return
	}
	m, err := h.svc.CompleteMultipart(r.Context(), who, tid, ids[0])
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 202, m)
}
