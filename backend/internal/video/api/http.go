package videoapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
)

// Requester authenticates the request (CSRF/origin for mutations) and returns the caller and the {t} tenant.
type Requester func(r *http.Request, mutate bool) (auth.Identity, uuid.UUID, error)

type handler struct {
	svc *Service
	req Requester
}

// Routes registers the video API under /api/v1/tenants/{t}.
func Routes(r chi.Router, svc *Service, req Requester) {
	h := handler{svc, req}
	const b = "/api/v1/tenants/{t}"
	r.Post(b+"/projects/{p}/video/uploads", h.beginUpload)
	r.Put(b+"/video/uploads/{u}/content", h.putContent)
	r.Get(b+"/projects/{p}/video/media", h.listMedia)
	r.Get(b+"/video/media/{m}/{which}", h.mediaFile)
	r.Post(b+"/projects/{p}/video/sequences", h.createSequence)
	r.Get(b+"/video/sequences/{s}", h.getSequence)
	r.Put(b+"/video/sequences/{s}/document", h.saveDocument)
	r.Post(b+"/video/sequences/{s}/renders", h.createRender)
	r.Get(b+"/video/renders/{r}", h.getRender)
	r.Get(b+"/video/renders/{r}/content", h.renderFile)
	r.Get(b+"/projects/{p}/video/deliveries", h.listDeliveries)
	r.Post(b+"/projects/{p}/video/deliveries", h.createDelivery)
	r.Post(b+"/video/deliveries/{c}/renders", h.addRender)
	r.Post(b+"/video/deliveries/{c}/publish", h.publish)
	r.Post(b+"/video/deliveries/{c}/shares", h.createShare)
	r.Delete(b+"/video/shares/{sh}", h.revokeShare)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, r *http.Request, err error) {
	var ve *ValidationError
	if errors.As(err, &ve) {
		writeJSON(w, 422, map[string]any{"code": "VIDEO_DOCUMENT_INVALID", "message_key": "errors.VIDEO_DOCUMENT_INVALID", "issues": ve.Issues, "request_id": middleware.GetReqID(r.Context())})
		return
	}
	e := domain.AsError(err)
	writeJSON(w, e.HTTPStatus, map[string]any{"code": e.Code, "message_key": e.MessageKey, "args": e.Args, "request_id": middleware.GetReqID(r.Context())})
}

func decode(r *http.Request, dst any, limit int64) error {
	if m, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || m != "application/json" {
		return domain.NewError("UNSUPPORTED_MEDIA_TYPE", 415)
	}
	b, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, limit))
	if err != nil {
		return domain.NewError("BODY_TOO_LARGE", 413)
	}
	if !utf8.Valid(b) {
		return domain.NewError("INVALID_JSON", 400)
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil || d.More() {
		return domain.NewError("INVALID_JSON", 400)
	}
	return nil
}

func id(r *http.Request, name string) (uuid.UUID, error) {
	v, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		return uuid.Nil, domain.NewError("NOT_FOUND", 404)
	}
	return v, nil
}

// begin authenticates, parses the path ids and hands over.
func (h handler) begin(w http.ResponseWriter, r *http.Request, mutate bool, ids ...string) (auth.Identity, uuid.UUID, []uuid.UUID, bool) {
	who, tid, err := h.req(r, mutate)
	if err != nil {
		writeErr(w, r, err)
		return who, tid, nil, false
	}
	out := make([]uuid.UUID, len(ids))
	for i, n := range ids {
		if out[i], err = id(r, n); err != nil {
			writeErr(w, r, err)
			return who, tid, nil, false
		}
	}
	return who, tid, out, true
}

func (h handler) beginUpload(w http.ResponseWriter, r *http.Request) {
	who, tid, ids, ok := h.begin(w, r, true, "p")
	if !ok {
		return
	}
	var req BeginUpload
	if err := decode(r, &req, 4096); err != nil {
		writeErr(w, r, err)
		return
	}
	up, err := h.svc.BeginUpload(r.Context(), who, tid, ids[0], req, r.Header.Get("Idempotency-Key"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 201, map[string]any{"upload_id": up})
}

func (h handler) putContent(w http.ResponseWriter, r *http.Request) {
	who, tid, ids, ok := h.begin(w, r, true, "u")
	if !ok {
		return
	}
	m, err := h.svc.PutContent(r.Context(), who, tid, ids[0], r.Body)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 202, m)
}

func (h handler) listMedia(w http.ResponseWriter, r *http.Request) {
	who, tid, ids, ok := h.begin(w, r, false, "p")
	if !ok {
		return
	}
	raw, err := h.svc.ListMedia(r.Context(), who, tid, ids[0])
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": json.RawMessage(raw)})
}

func (h handler) serve(w http.ResponseWriter, r *http.Request, o Object, download bool) {
	p, ok := h.svc.Store.(interface {
		Path(bucket, key string) (string, error)
	})
	if !ok {
		writeErr(w, r, domain.NewError("NOT_IMPLEMENTED", 501))
		return
	}
	path, err := p.Path(o.Bucket, o.ObjectKey)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	w.Header().Set("Content-Type", o.MediaType)
	if download {
		w.Header().Set("Content-Disposition", `attachment; filename="video.mp4"`)
	}
	w.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")
	http.ServeFile(w, r, path)
}

func (h handler) mediaFile(w http.ResponseWriter, r *http.Request) {
	which := chi.URLParam(r, "which")
	if which != "proxy" && which != "filmstrip" && which != "waveform" {
		writeErr(w, r, domain.NewError("NOT_FOUND", 404))
		return
	}
	who, tid, ids, ok := h.begin(w, r, false, "m")
	if !ok {
		return
	}
	o, err := h.svc.MediaObject(r.Context(), who, tid, ids[0], which)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	h.serve(w, r, o, false)
}

func (h handler) createSequence(w http.ResponseWriter, r *http.Request) {
	who, tid, ids, ok := h.begin(w, r, true, "p")
	if !ok {
		return
	}
	var req NewSequence
	if err := decode(r, &req, 4096); err != nil {
		writeErr(w, r, err)
		return
	}
	sid, err := h.svc.CreateSequence(r.Context(), who, tid, ids[0], req)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 201, map[string]any{"sequence_id": sid})
}

func (h handler) getSequence(w http.ResponseWriter, r *http.Request) {
	who, tid, ids, ok := h.begin(w, r, false, "s")
	if !ok {
		return
	}
	q, err := h.svc.GetSequence(r.Context(), who, tid, ids[0])
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 200, q)
}

func (h handler) saveDocument(w http.ResponseWriter, r *http.Request) {
	who, tid, ids, ok := h.begin(w, r, true, "s")
	if !ok {
		return
	}
	var req struct {
		Expected int64           `json:"expected_revision"`
		Label    string          `json:"label"`
		Document json.RawMessage `json:"document"`
	}
	if err := decode(r, &req, maxDocBytes+4096); err != nil {
		writeErr(w, r, err)
		return
	}
	res, err := h.svc.SaveDocument(r.Context(), who, tid, ids[0], req.Expected, req.Document, req.Label)
	if err != nil {
		var de *domain.Error
		if errors.As(err, &de) && de.Code == "VIDEO_SEQUENCE_REVISION_CONFLICT" {
			if q, qerr := h.svc.GetSequence(r.Context(), who, tid, ids[0]); qerr == nil {
				writeJSON(w, 409, map[string]any{"code": de.Code, "message_key": de.MessageKey, "head_revision": q.HeadRevision})
				return
			}
		}
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 200, res)
}

func (h handler) createRender(w http.ResponseWriter, r *http.Request) {
	who, tid, ids, ok := h.begin(w, r, true, "s")
	if !ok {
		return
	}
	var req NewRender
	if err := decode(r, &req, 8192); err != nil {
		writeErr(w, r, err)
		return
	}
	t, err := h.svc.CreateRender(r.Context(), who, tid, ids[0], req, r.Header.Get("Idempotency-Key"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 202, t)
}

func (h handler) getRender(w http.ResponseWriter, r *http.Request) {
	who, tid, ids, ok := h.begin(w, r, false, "r")
	if !ok {
		return
	}
	raw, err := h.svc.GetRender(r.Context(), who, tid, ids[0])
	if err != nil {
		writeErr(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(raw)
}

func (h handler) renderFile(w http.ResponseWriter, r *http.Request) {
	who, tid, ids, ok := h.begin(w, r, false, "r")
	if !ok {
		return
	}
	o, err := h.svc.RenderOutput(r.Context(), who, tid, ids[0])
	if err != nil {
		writeErr(w, r, err)
		return
	}
	h.serve(w, r, o, r.URL.Query().Get("download") == "1")
}

func (h handler) listDeliveries(w http.ResponseWriter, r *http.Request) {
	who, tid, ids, ok := h.begin(w, r, false, "p")
	if !ok {
		return
	}
	out, err := h.svc.ListDeliveries(r.Context(), who, tid, ids[0])
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": json.RawMessage(out)})
}

func (h handler) createDelivery(w http.ResponseWriter, r *http.Request) {
	who, tid, ids, ok := h.begin(w, r, true, "p")
	if !ok {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := decode(r, &req, 2048); err != nil {
		writeErr(w, r, err)
		return
	}
	cid, err := h.svc.CreateDelivery(r.Context(), who, tid, ids[0], req.Name)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 201, map[string]any{"delivery_id": cid})
}

func (h handler) addRender(w http.ResponseWriter, r *http.Request) {
	who, tid, ids, ok := h.begin(w, r, true, "c")
	if !ok {
		return
	}
	var req struct {
		RenderID uuid.UUID `json:"render_id"`
		Title    string    `json:"title"`
	}
	if err := decode(r, &req, 2048); err != nil {
		writeErr(w, r, err)
		return
	}
	ord, err := h.svc.AddRender(r.Context(), who, tid, ids[0], req.RenderID, req.Title)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ordinal": ord})
}

func (h handler) publish(w http.ResponseWriter, r *http.Request) {
	who, tid, ids, ok := h.begin(w, r, true, "c")
	if !ok {
		return
	}
	var req struct {
		Published bool `json:"published"`
	}
	if err := decode(r, &req, 256); err != nil {
		writeErr(w, r, err)
		return
	}
	if err := h.svc.Publish(r.Context(), who, tid, ids[0], req.Published); err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"published": req.Published})
}

func (h handler) createShare(w http.ResponseWriter, r *http.Request) {
	who, tid, ids, ok := h.begin(w, r, true, "c")
	if !ok {
		return
	}
	var req NewShare
	if err := decode(r, &req, 2048); err != nil {
		writeErr(w, r, err)
		return
	}
	sh, err := h.svc.CreateShare(r.Context(), who, tid, ids[0], req)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, 201, sh)
}

func (h handler) revokeShare(w http.ResponseWriter, r *http.Request) {
	who, tid, ids, ok := h.begin(w, r, true, "sh")
	if !ok {
		return
	}
	if err := h.svc.RevokeShare(r.Context(), who, tid, ids[0]); err != nil {
		writeErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ViewerAuth authorizes a portal share session for one permission ("view" or "download").
type ViewerAuth func(r *http.Request, permission string) (Viewer, error)

// PortalRoutes registers the viewer endpoints of a published delivery.
func PortalRoutes(r chi.Router, svc *Service, authz ViewerAuth) {
	h := handler{svc: svc}
	r.Get("/api/v1/portal/video/delivery", func(w http.ResponseWriter, r *http.Request) {
		v, err := authz(r, "view")
		if err != nil {
			writeErr(w, r, err)
			return
		}
		raw, err := svc.Delivery(r.Context(), v)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	})
	r.Get("/api/v1/portal/video/renders/{r}/content", func(w http.ResponseWriter, r *http.Request) {
		rid, err := id(r, "r")
		if err != nil {
			writeErr(w, r, err)
			return
		}
		download := r.URL.Query().Get("download") == "1"
		perm := "view"
		if download {
			perm = "download"
		}
		v, err := authz(r, perm)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		o, err := svc.ViewerObject(r.Context(), v, rid, download)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		h.serve(w, r, o, download)
	})
}
