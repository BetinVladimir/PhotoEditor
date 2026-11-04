// Package videoapi is the tenant-facing video API: uploads, media library,
// sequences (immutable revisions), renders and deliveries. Every call runs in a
// tenant-scoped transaction after the caller's role is checked; data is read and
// written only through the SQL functions of migrations 02699-02704.
package videoapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/video/timeline"
	"photoeditor/backend/internal/video/worker"
)

const (
	partBytes      = 33554432
	tps            = 705600000
	derivFractionP = 15
	derivBase      = 134217728
	derivCap       = 4294967296
	maxDocBytes    = timeline.MaxDocBytes
)

// Authorizer checks live authority inside the tenant transaction (tenant.AuthorizeInTransaction in production).
type Authorizer func(ctx context.Context, tx pgx.Tx, who auth.Identity, tid uuid.UUID, required domain.Role) (domain.Role, error)

type Service struct {
	Pool      *pgxpool.Pool // photo_api
	Scheduler *pgxpool.Pool // photo_scheduler; DEMO completes uploads inline instead of a sweeper
	Portal    *pgxpool.Pool // photo_portal: viewer reads and egress accounting
	Pepper    []byte        // MAC key of share secrets (same as the portal's)
	Store     worker.Store
	Bucket    string
	Authz     Authorizer
	TempDir   string

	streams streamLimiter
}

func (s *Service) with(ctx context.Context, who auth.Identity, tid uuid.UUID, required domain.Role, fn func(pgx.Tx) error) error {
	return mapErr(database.TenantTransaction(ctx, s.Pool, tid, func(tx pgx.Tx) error {
		if _, err := s.Authz(ctx, tx, who, tid, required); err != nil {
			return err
		}
		return fn(tx)
	}))
}

var codeRE = regexp.MustCompile(`^[A-Z][A-Z0-9_]{2,63}`)

// mapErr turns SQL function errors (stable codes in the message) into API errors.
func mapErr(err error) error {
	var de *domain.Error
	if err == nil || errors.As(err, &de) {
		return err
	}
	var pe *pgconn.PgError
	if !errors.As(err, &pe) {
		return err
	}
	code := codeRE.FindString(pe.Message)
	status := 0
	switch pe.Code {
	case "22023":
		status = 422
	case "P0002":
		status = 404
	case "55000", "23505", "40001":
		status = 409
	case "53400":
		status = 429
	case "53300":
		status = 503
	case "42501":
		status = 403
	}
	if status == 0 || code == "" {
		return err
	}
	return domain.NewError(code, status)
}

func bad(code string) error { return domain.NewError(code, 422) }
func notFound() error       { return domain.NewError("NOT_FOUND", 404) }

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// ---- uploads

type BeginUpload struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

var shaRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (s *Service) BeginUpload(ctx context.Context, who auth.Identity, tid, pid uuid.UUID, req BeginUpload, key string) (uuid.UUID, error) {
	if len(key) < 8 || len(key) > 128 {
		return uuid.Nil, bad("IDEMPOTENCY_KEY_INVALID")
	}
	if (req.Kind != "video" && req.Kind != "audio") || !shaRE.MatchString(req.SHA256) || req.Bytes < 1 || req.Name == "" {
		return uuid.Nil, bad("VIDEO_UPLOAD_INVALID")
	}
	parts := (req.Bytes + partBytes - 1) / partBytes
	est := req.Bytes*derivFractionP/100 + derivBase
	if est > derivCap {
		est = derivCap
	}
	var id uuid.UUID
	err := s.with(ctx, who, tid, domain.Editor, func(tx pgx.Tx) error {
		// Principal ids are scoped by the session user, so the same key from another user is a different upload.
		objKey := fmt.Sprintf("tenants/%s/video/uploads/%s/source", tid, uuid.New())
		return tx.QueryRow(ctx, `SELECT photo.begin_video_upload($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
			tid, pid, who.UserID, req.Kind, req.Name, req.Bytes, req.SHA256, sha256Hex("upload:"+key), int64(partBytes), int32(parts), objKey, req.Bytes+est).Scan(&id)
	})
	return id, err
}

type upload struct {
	ID        uuid.UUID `json:"id"`
	ProjectID uuid.UUID `json:"project_id"`
	Principal uuid.UUID `json:"principal_id"`
	State     string    `json:"state"`
	Kind      string    `json:"media_kind"`
	Filename  string    `json:"filename"`
	Bytes     int64     `json:"bytes"`
	SHA256    string    `json:"sha256"`
	ObjectKey string    `json:"object_key"`
	Parts     int       `json:"parts_total"`
}

type Media struct {
	MediaID uuid.UUID `json:"media_id"`
	State   string    `json:"state"`
}

// PutContent stores the body as the upload's object, verifies size and hash against the declaration, then
// completes the upload and queues the ingest job. DEMO: one request carries the whole file.
func (s *Service) PutContent(ctx context.Context, who auth.Identity, tid, uid uuid.UUID, body io.Reader) (Media, error) {
	if s.Scheduler == nil {
		return Media{}, domain.NewError("SCHEDULER_UNAVAILABLE", 503)
	}
	var up upload
	err := s.with(ctx, who, tid, domain.Editor, func(tx pgx.Tx) error {
		var raw []byte
		if err := tx.QueryRow(ctx, `SELECT photo.get_video_upload($1,$2)::text`, tid, uid).Scan(&raw); err != nil || raw == nil {
			return notFound()
		}
		return json.Unmarshal(raw, &up)
	})
	if err != nil {
		return Media{}, err
	}
	if up.Principal != who.UserID {
		return Media{}, notFound()
	}
	if up.State != "initializing" && up.State != "uploading" {
		return Media{}, domain.NewError("VIDEO_UPLOAD_STATE", 409)
	}
	tmp, err := os.CreateTemp(s.TempDir, "upload-")
	if err != nil {
		return Media{}, err
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(body, up.Bytes+1))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return Media{}, err
	}
	if n != up.Bytes || hex.EncodeToString(h.Sum(nil)) != up.SHA256 {
		return Media{}, bad("VIDEO_UPLOAD_MISMATCH")
	}
	if _, _, err := s.Store.Put(ctx, s.Bucket, up.ObjectKey, tmp.Name()); err != nil {
		return Media{}, err
	}
	var token uuid.UUID
	err = s.with(ctx, who, tid, domain.Editor, func(tx pgx.Tx) error {
		var ok bool
		if err := tx.QueryRow(ctx, `SELECT photo.bind_video_upload_multipart($1,$2,'local')`, tid, uid).Scan(&ok); err != nil {
			return err
		}
		for i := 1; i <= up.Parts; i++ {
			b := int64(partBytes)
			if i == up.Parts {
				b = up.Bytes - int64(up.Parts-1)*partBytes
			}
			if err := tx.QueryRow(ctx, `SELECT photo.record_video_upload_part($1,$2,$3,$4,$5)`, tid, uid, i, fmt.Sprintf("p%d", i), b).Scan(&ok); err != nil {
				return err
			}
		}
		parts := make([]map[string]any, up.Parts)
		for i := range parts {
			parts[i] = map[string]any{"part_no": i + 1, "etag": fmt.Sprintf("p%d", i+1)}
		}
		pj, _ := json.Marshal(parts)
		return tx.QueryRow(ctx, `SELECT photo.complete_video_upload($1,$2,$3::jsonb)`, tid, uid, string(pj)).Scan(&token)
	})
	if err != nil {
		return Media{}, err
	}
	mt := mime.TypeByExtension(strings.ToLower(filepath.Ext(up.Filename)))
	if mt == "" {
		mt = "application/octet-stream"
	}
	var mid uuid.UUID
	if err := mapErr(s.Scheduler.QueryRow(ctx, `SELECT photo.finish_video_upload_completion($1,$2,$3,$4,'local-v1',$5,$6)`, tid, uid, token, s.Bucket, up.SHA256, mt).Scan(&mid)); err != nil {
		return Media{}, err
	}
	err = s.with(ctx, who, tid, domain.Editor, func(tx pgx.Tx) error {
		var job uuid.UUID
		req := sha256Hex("ingest:" + mid.String())
		return tx.QueryRow(ctx, `SELECT photo.create_video_job($1,$2,$3,'ingest',$4,NULL,NULL,$5,$6,'auto',$7,$8,0,1)`,
			tid, up.ProjectID, who.UserID, mid, uuid.New(), req, int64(60*tps), req).Scan(&job)
	})
	return Media{MediaID: mid, State: "probing"}, err
}

// ---- media library

func (s *Service) ListMedia(ctx context.Context, who auth.Identity, tid, pid uuid.UUID) (json.RawMessage, error) {
	var raw []byte
	err := s.with(ctx, who, tid, domain.Viewer, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT photo.list_video_media($1,$2,100)::text`, tid, pid).Scan(&raw)
	})
	return raw, err
}

// Object is a stored file the handler may serve.
type Object struct {
	Bucket    string `json:"bucket"`
	ObjectKey string `json:"object_key"`
	MediaType string `json:"media_type"`
	Bytes     int64  `json:"bytes"`
}

func (s *Service) MediaObject(ctx context.Context, who auth.Identity, tid, mid uuid.UUID, which string) (Object, error) {
	var o Object
	err := s.with(ctx, who, tid, domain.Viewer, func(tx pgx.Tx) error {
		var raw []byte
		if err := tx.QueryRow(ctx, `SELECT photo.get_video_media_object($1,$2,$3)::text`, tid, mid, which).Scan(&raw); err != nil || raw == nil {
			return notFound()
		}
		return json.Unmarshal(raw, &o)
	})
	return o, err
}

// WithdrawMedia starts the two-phase removal of a library item; the maintenance sweeper finishes it.
func (s *Service) WithdrawMedia(ctx context.Context, who auth.Identity, tid, mid uuid.UUID) error {
	return s.with(ctx, who, tid, domain.Editor, func(tx pgx.Tx) error {
		var ok bool
		return tx.QueryRow(ctx, `SELECT photo.withdraw_video_media($1,$2)`, tid, mid).Scan(&ok)
	})
}

// ---- sequences

type NewSequence struct {
	Name   string `json:"name"`
	FPSNum int    `json:"fps_num"`
	FPSDen int    `json:"fps_den"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

func (s *Service) CreateSequence(ctx context.Context, who auth.Identity, tid, pid uuid.UUID, r NewSequence) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.with(ctx, who, tid, domain.Editor, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT photo.create_video_sequence($1,$2,$3,$4,$5,$6,$7,$8)`, tid, pid, who.UserID, r.Name, r.FPSNum, r.FPSDen, r.Width, r.Height).Scan(&id)
	})
	return id, err
}

type Sequence struct {
	ID           uuid.UUID       `json:"id"`
	ProjectID    uuid.UUID       `json:"project_id"`
	Name         string          `json:"name"`
	FPSNum       int             `json:"fps_num"`
	FPSDen       int             `json:"fps_den"`
	Width        int             `json:"width"`
	Height       int             `json:"height"`
	HeadRevision int64           `json:"head_revision"`
	Document     *Object2        `json:"document,omitempty"`
	Doc          json.RawMessage `json:"doc,omitempty"`
}

// Object2 is the head revision pointer inside get_video_sequence.
type Object2 struct {
	Revision  int64  `json:"revision"`
	SHA256    string `json:"sha256"`
	Bucket    string `json:"bucket"`
	ObjectKey string `json:"object_key"`
	Bytes     int64  `json:"bytes"`
}

func (s *Service) GetSequence(ctx context.Context, who auth.Identity, tid, sid uuid.UUID) (Sequence, error) {
	var q Sequence
	err := s.with(ctx, who, tid, domain.Viewer, func(tx pgx.Tx) error {
		var raw []byte
		if err := tx.QueryRow(ctx, `SELECT photo.get_video_sequence($1,$2)::text`, tid, sid).Scan(&raw); err != nil || raw == nil {
			return notFound()
		}
		return json.Unmarshal(raw, &q)
	})
	if err != nil || q.Document == nil {
		return q, err
	}
	tmp, err := os.CreateTemp(s.TempDir, "doc-")
	if err != nil {
		return q, err
	}
	defer os.Remove(tmp.Name())
	tmp.Close()
	if err := s.Store.Fetch(ctx, q.Document.Bucket, q.Document.ObjectKey, tmp.Name()); err != nil {
		return q, err
	}
	q.Doc, err = os.ReadFile(tmp.Name())
	q.Document.Bucket, q.Document.ObjectKey = "", ""
	return q, err
}

type SaveResult struct {
	Revision int64 `json:"revision"`
}

// SaveDocument validates the document against the library, stores it and appends revision expected+1.
func (s *Service) SaveDocument(ctx context.Context, who auth.Identity, tid, sid uuid.UUID, expected int64, docBytes []byte, label string) (SaveResult, error) {
	if len(docBytes) == 0 || len(docBytes) > maxDocBytes {
		return SaveResult{}, bad("VIDEO_DOCUMENT_SIZE")
	}
	doc, err := timeline.Parse(docBytes)
	if err != nil {
		return SaveResult{}, bad("VIDEO_DOCUMENT_INVALID")
	}
	if label != "manual" && label != "autosave" {
		label = "manual"
	}
	var res SaveResult
	err = s.with(ctx, who, tid, domain.Editor, func(tx pgx.Tx) error {
		var seq []byte
		if err := tx.QueryRow(ctx, `SELECT photo.get_video_sequence($1,$2)::text`, tid, sid).Scan(&seq); err != nil || seq == nil {
			return notFound()
		}
		var head Sequence
		if err := json.Unmarshal(seq, &head); err != nil {
			return err
		}
		var lib []byte
		if err := tx.QueryRow(ctx, `SELECT photo.list_video_media($1,$2,100)::text`, tid, head.ProjectID).Scan(&lib); err != nil {
			return err
		}
		var media []struct {
			ID    string          `json:"id"`
			State string          `json:"state"`
			SHA   string          `json:"source_sha256"`
			Kind  string          `json:"kind"`
			Probe json.RawMessage `json:"probe"`
		}
		if err := json.Unmarshal(lib, &media); err != nil {
			return err
		}
		vc := timeline.Context{SourceDurations: map[string]int64{}, SourceStates: map[string]string{}, SourceHashes: map[string]string{}}
		var sources []map[string]string
		for _, m := range media {
			vc.SourceStates[m.ID], vc.SourceHashes[m.ID] = m.State, m.SHA
			var f struct {
				D int64 `json:"duration_ticks"`
			}
			if json.Unmarshal(m.Probe, &f) == nil && f.D > 0 {
				vc.SourceDurations[m.ID] = f.D
			}
		}
		if issues := timeline.Validate(doc, vc); len(issues) > 0 {
			return &ValidationError{Issues: issues}
		}
		seen := map[string]bool{}
		for _, t := range asArr(doc["tracks"]) {
			for _, c := range asArr(asObj(t)["clips"]) {
				src := asObj(asObj(c)["source"])
				id, _ := src["media_id"].(string)
				if id != "" && !seen[id] {
					seen[id] = true
					k, _ := src["kind"].(string)
					sh, _ := src["sha256"].(string)
					sources = append(sources, map[string]string{"kind": k, "id": id, "sha256": sh})
				}
			}
		}
		sum := sha256.Sum256(docBytes)
		docSha := hex.EncodeToString(sum[:])
		objKey := fmt.Sprintf("tenants/%s/video/sequences/%s/%s.json", tid, sid, docSha)
		tmp, err := os.CreateTemp(s.TempDir, "docsave-")
		if err != nil {
			return err
		}
		defer os.Remove(tmp.Name())
		if _, err := tmp.Write(docBytes); err != nil {
			tmp.Close()
			return err
		}
		tmp.Close()
		if _, _, err := s.Store.Put(ctx, s.Bucket, objKey, tmp.Name()); err != nil {
			return err
		}
		var art uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,output_role)
			VALUES($1,$2,$3,'v1',$4,$5,'application/json','ready',now(),'video_sequence_document')
			ON CONFLICT(tenant_id,bucket,object_key) DO UPDATE SET sha256=EXCLUDED.sha256 RETURNING id`, tid, s.Bucket, objKey, docSha, len(docBytes)).Scan(&art); err != nil {
			return err
		}
		sj, _ := json.Marshal(sources)
		summary, _ := json.Marshal(map[string]any{"clips": countClips(doc)})
		return tx.QueryRow(ctx, `SELECT photo.save_video_sequence_revision($1,$2,$3,$4,$5,$6::jsonb,$7::jsonb,$8,$9)`,
			tid, sid, expected, art, docSha, string(summary), string(sj), label, who.UserID).Scan(&res.Revision)
	})
	return res, err
}

// ValidationError carries the validator's issues for a 422 body.
type ValidationError struct{ Issues []timeline.Issue }

func (e *ValidationError) Error() string { return "VIDEO_DOCUMENT_INVALID" }

func asObj(v any) map[string]any { m, _ := v.(map[string]any); return m }
func asArr(v any) []any          { a, _ := v.([]any); return a }
func countClips(d timeline.Doc) int {
	n := 0
	for _, t := range asArr(d["tracks"]) {
		n += len(asArr(asObj(t)["clips"]))
	}
	return n
}

// ---- renders

type NewRender struct {
	Revision int64           `json:"revision"`
	Preset   json.RawMessage `json:"preset"`
}

type RenderTicket struct {
	RenderID uuid.UUID `json:"render_id"`
}

func (s *Service) CreateRender(ctx context.Context, who auth.Identity, tid, sid uuid.UUID, r NewRender, key string) (RenderTicket, error) {
	if len(key) < 8 || len(key) > 128 {
		return RenderTicket{}, bad("IDEMPOTENCY_KEY_INVALID")
	}
	preset := string(r.Preset)
	var p struct {
		Encoder string `json:"encoder"`
	}
	if preset == "" || json.Unmarshal(r.Preset, &p) != nil || (p.Encoder != "" && p.Encoder != "libx264" && p.Encoder != "h264_nvenc") || r.Revision < 1 {
		return RenderTicket{}, bad("VIDEO_RENDER_INVALID")
	}
	var out RenderTicket
	err := s.with(ctx, who, tid, domain.Editor, func(tx pgx.Tx) error {
		var raw []byte
		if err := tx.QueryRow(ctx, `SELECT photo.get_video_sequence($1,$2)::text`, tid, sid).Scan(&raw); err != nil || raw == nil {
			return notFound()
		}
		var q Sequence
		if err := json.Unmarshal(raw, &q); err != nil {
			return err
		}
		// Duration drives cost and the output reservation; read it from the stored document.
		ticks, _, err := s.docEnd(ctx, tx, tid, sid, r.Revision)
		if err != nil {
			return err
		}
		reserve := estimateRenderBytes(ticks)
		return tx.QueryRow(ctx, `SELECT photo.create_video_render($1,$2,$3,$4,$5,$6::jsonb,$7,$8,$9,'auto',$10,$11,$12,1)`,
			tid, q.ProjectID, who.UserID, sid, r.Revision, preset, sha256Hex(preset), uuid.New(), sha256Hex("render-params:"+preset), ticks,
			sha256Hex("render:"+key+sid.String()), reserve).Scan(&out.RenderID)
	})
	return out, err
}

// docEnd returns the end of the last clip of a stored revision (ticks).
func (s *Service) docEnd(ctx context.Context, tx pgx.Tx, tid, sid uuid.UUID, rev int64) (int64, int64, error) {
	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT photo.get_video_sequence($1,$2)::text`, tid, sid).Scan(&raw); err != nil || raw == nil {
		return 0, 0, notFound()
	}
	var q Sequence
	if err := json.Unmarshal(raw, &q); err != nil {
		return 0, 0, err
	}
	if q.Document == nil || q.Document.Revision != rev {
		return 0, 0, domain.NewError("VIDEO_REVISION_NOT_HEAD", 409)
	}
	tmp, err := os.CreateTemp(s.TempDir, "docend-")
	if err != nil {
		return 0, 0, err
	}
	defer os.Remove(tmp.Name())
	tmp.Close()
	if err := s.Store.Fetch(ctx, q.Document.Bucket, q.Document.ObjectKey, tmp.Name()); err != nil {
		return 0, 0, err
	}
	b, err := os.ReadFile(tmp.Name())
	if err != nil {
		return 0, 0, err
	}
	doc, err := timeline.Parse(b)
	if err != nil {
		return 0, 0, err
	}
	var end int64
	for _, t := range asArr(doc["tracks"]) {
		for _, c := range asArr(asObj(t)["clips"]) {
			cm := asObj(c)
			if e := int64(num(cm["at"])) + timeline.ClipDuration(cm); e > end {
				end = e
			}
		}
	}
	if end <= 0 {
		return 0, 0, bad("VIDEO_DOCUMENT_EMPTY")
	}
	return end, q.Document.Bytes, nil
}

func num(v any) float64 { f, _ := v.(float64); return f }

// RENDER_EST_MARGIN_PCT 105 and RENDER_EST_BASE_BYTES 64 MiB over a 25 Mbit/s ceiling.
func estimateRenderBytes(ticks int64) int64 {
	secs := (ticks + tps - 1) / tps
	return secs*25_000_000/8*105/100 + 67108864
}

func (s *Service) GetRender(ctx context.Context, who auth.Identity, tid, rid uuid.UUID) (json.RawMessage, error) {
	var raw []byte
	err := s.with(ctx, who, tid, domain.Viewer, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT photo.get_video_render($1,$2)::text`, tid, rid).Scan(&raw); err != nil || raw == nil {
			return notFound()
		}
		return nil
	})
	return raw, err
}

// RenderOutput returns the finished file of a render.
func (s *Service) RenderOutput(ctx context.Context, who auth.Identity, tid, rid uuid.UUID) (Object, error) {
	raw, err := s.GetRender(ctx, who, tid, rid)
	if err != nil {
		return Object{}, err
	}
	var r struct {
		State  string  `json:"state"`
		Output *Object `json:"output"`
	}
	if json.Unmarshal(raw, &r) != nil || r.State != "ready" || r.Output == nil {
		return Object{}, domain.NewError("VIDEO_RENDER_NOT_READY", 409)
	}
	return *r.Output, nil
}

// ---- deliveries

func (s *Service) CreateDelivery(ctx context.Context, who auth.Identity, tid, pid uuid.UUID, name string) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.with(ctx, who, tid, domain.Editor, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT photo.create_video_delivery($1,$2,$3)`, tid, pid, name).Scan(&id)
	})
	return id, err
}

// ListDeliveries returns the project's video deliveries with their items and share links (never the link secrets).
func (s *Service) ListDeliveries(ctx context.Context, who auth.Identity, tid, pid uuid.UUID) (json.RawMessage, error) {
	var raw []byte
	err := s.with(ctx, who, tid, domain.Viewer, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT photo.list_video_deliveries($1,$2)::text`, tid, pid).Scan(&raw)
	})
	return raw, err
}

func (s *Service) AddRender(ctx context.Context, who auth.Identity, tid, cid, rid uuid.UUID, title string) (int, error) {
	var ord int
	err := s.with(ctx, who, tid, domain.Editor, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT photo.add_video_render_to_delivery($1,$2,$3,$4)`, tid, cid, rid, title).Scan(&ord)
	})
	return ord, err
}

func (s *Service) Publish(ctx context.Context, who auth.Identity, tid, cid uuid.UUID, publish bool) error {
	return s.with(ctx, who, tid, domain.Editor, func(tx pgx.Tx) error {
		var ok bool
		return tx.QueryRow(ctx, `SELECT photo.publish_video_delivery($1,$2,$3)`, tid, cid, publish).Scan(&ok)
	})
}

// ---- shares (owner side)

type NewShare struct {
	Label        string `json:"label"`
	Download     bool   `json:"download"`
	ExpiresHours int    `json:"expires_in_hours"`
	EgressBytes  int64  `json:"egress_bytes,omitempty"` // optional cap on the traffic of this link
}

type Share struct {
	ShareID   uuid.UUID `json:"share_id"`
	Secret    string    `json:"secret"`
	ExpiresAt time.Time `json:"expires_at"`
}

// CreateShare issues a viewer link for a published delivery. The secret is shown once; only its MAC is stored.
func (s *Service) CreateShare(ctx context.Context, who auth.Identity, tid, cid uuid.UUID, r NewShare) (Share, error) {
	if len(s.Pepper) < 32 {
		return Share{}, domain.NewError("SHARES_UNAVAILABLE", 503)
	}
	if r.ExpiresHours == 0 {
		r.ExpiresHours = 24 * 7
	}
	if r.ExpiresHours < 1 || r.ExpiresHours > 24*90 {
		return Share{}, bad("VIDEO_SHARE_INVALID")
	}
	secret, err := auth.NewSecret(rand.Reader)
	if err != nil {
		return Share{}, err
	}
	out := Share{ShareID: uuid.New(), Secret: secret, ExpiresAt: time.Now().Add(time.Duration(r.ExpiresHours) * time.Hour)}
	err = s.with(ctx, who, tid, domain.Editor, func(tx pgx.Tx) error {
		limits := map[string]any{}
		if r.EgressBytes > 0 {
			limits["egress_bytes"] = r.EgressBytes
		}
		return tx.QueryRow(ctx, `SELECT photo.create_video_share($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9)`, tid, cid, out.ShareID,
			auth.MAC(s.Pepper, "share-secret", out.ShareID.String(), secret), r.Label, r.Download, out.ExpiresAt, limits, who.UserID).Scan(&out.ShareID)
	})
	return out, err
}

func (s *Service) RevokeShare(ctx context.Context, who auth.Identity, tid, share uuid.UUID) error {
	return s.with(ctx, who, tid, domain.Editor, func(tx pgx.Tx) error {
		var ok bool
		if err := tx.QueryRow(ctx, `SELECT photo.revoke_video_share($1,$2)`, tid, share).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return notFound()
		}
		return nil
	})
}

// ---- viewer side (portal share session)

// Viewer is the authorized portal session.
type Viewer struct{ Tenant, Share, Collection, Session uuid.UUID }

func (s *Service) Delivery(ctx context.Context, v Viewer) (json.RawMessage, error) {
	var raw []byte
	if err := mapErr(s.Portal.QueryRow(ctx, `SELECT photo.get_video_delivery_view($1,$2)::text`, v.Tenant, v.Collection).Scan(&raw)); err != nil || raw == nil {
		if err == nil {
			err = notFound()
		}
		return nil, err
	}
	return raw, nil
}

// ViewerObject resolves a render file of the viewer's delivery. With download set the egress is accounted first;
// a share without the download permission is refused.
func (s *Service) ViewerObject(ctx context.Context, v Viewer, render uuid.UUID, download bool) (Object, error) {
	var o Object
	var raw []byte
	if err := mapErr(s.Portal.QueryRow(ctx, `SELECT photo.get_video_delivery_object($1,$2,$3)::text`, v.Tenant, v.Collection, render).Scan(&raw)); err != nil || raw == nil {
		if err == nil {
			err = notFound()
		}
		return o, err
	}
	if err := json.Unmarshal(raw, &o); err != nil {
		return o, err
	}
	if download {
		var ok bool
		if err := mapErr(s.Portal.QueryRow(ctx, `SELECT photo.record_video_egress($1,$2,$3,$4)`, v.Tenant, v.Share, render, o.Bytes).Scan(&ok)); err != nil {
			return o, err
		}
		if !ok {
			return o, domain.NewError("FORBIDDEN", 403)
		}
	}
	return o, nil
}

// egressLease is a slice of the traffic budget held while one response is written (FN-D-19).
type egressLease struct {
	ID      uuid.UUID `json:"lease_id"`
	Granted int64     `json:"granted"`
}

func (s *Service) leaseEgress(ctx context.Context, v Viewer, want int64) (egressLease, error) {
	var l egressLease
	var raw []byte
	// DEMO: one lease per response, sized to it (the spec renews 8-256 MiB leases while a stream runs). A grant smaller
	// than the response ends it short, which is how a limit shows up mid-stream.
	if want < 1 {
		want = 1
	}
	if err := mapErr(s.Portal.QueryRow(ctx, `SELECT photo.lease_video_egress($1,$2,$3)::text`, v.Tenant, v.Share, want).Scan(&raw)); err != nil {
		return l, err
	}
	return l, json.Unmarshal(raw, &l)
}

// settleEgress returns what a response did not use; it runs even when the client went away.
func (s *Service) settleEgress(ctx context.Context, v Viewer, l egressLease, used int64, render uuid.UUID) error {
	var ok bool
	return s.Portal.QueryRow(context.WithoutCancel(ctx), `SELECT photo.settle_video_egress($1,$2,$3,$4,'stream')`, v.Tenant, l.ID, used, render).Scan(&ok)
}
