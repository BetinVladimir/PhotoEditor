package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2region"
	"testing"
)

type authoredMediaDenyReader struct{ reads int }

func (s *authoredMediaDenyReader) Read(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID) (v2region.AuthoredRegionMedia, error) {
	s.reads++
	return v2region.AuthoredRegionMedia{}, v2region.ErrRegionInvalid
}
func (s *authoredMediaDenyReader) Recheck(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, v2region.AuthoredRegionMediaProof) error {
	return nil
}

func TestAuthoredMediaAuthenticatesBeforeUnavailableReader(t *testing.T) {
	for _, suffix := range []string{"descriptor", "derivative"} {
		t.Run(suffix, func(t *testing.T) {
			router := chi.NewRouter()
			called := false
			RegisterAuthoredRegionMediaRoutes(router, func(*http.Request, bool) (auth.Identity, uuid.UUID, error) {
				called = true
				return auth.Identity{}, uuid.Nil, domain.NewError("UNAUTHORIZED", 401)
			}, nil)
			req := httptest.NewRequest("GET", "/api/v1/tenants/"+uuid.NewString()+"/assets/"+uuid.NewString()+"/authored-region-previews/"+uuid.NewString()+"/"+suffix, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if !called || w.Code != 401 {
				t.Fatal("authentication skipped", called, w.Code)
			}
			if w.Header().Get("Cache-Control") != "private, no-store" {
				t.Fatal("missing private cache policy")
			}
		})
	}
}
func TestAuthoredMediaRefusesUnverifiedResultWithoutBodyLeak(t *testing.T) {
	reader := &authoredMediaDenyReader{}
	router := chi.NewRouter()
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tenant := uuid.New()
	RegisterAuthoredRegionMediaRoutes(router, func(*http.Request, bool) (auth.Identity, uuid.UUID, error) { return who, tenant, nil }, reader)
	for _, suffix := range []string{"descriptor", "derivative"} {
		req := httptest.NewRequest("GET", "/api/v1/tenants/"+tenant.String()+"/assets/"+uuid.NewString()+"/authored-region-previews/"+uuid.NewString()+"/"+suffix, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != 404 || w.Header().Get("Content-Type") == "image/jpeg" {
			t.Fatal("unverified result exposed", w.Code)
		}
	}
	if reader.reads != 2 {
		t.Fatal("routes not exercised", reader.reads)
	}
}

func authoredHTTPMediaFixture() v2region.AuthoredRegionMedia {
	digest := func(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
	body := []byte{0xff, 0xd8, 0xff, 0xe0}
	id := uuid.New()
	root := uuid.New()
	intent := digest([]byte("intent"))
	derivative := v2input.Ref{ArtifactID: uuid.New(), SHA256: digest(body)}
	manifest, _ := json.Marshal(map[string]any{"schema_version": 1, "request_id": id, "intent_sha256": intent, "derivative": derivative, "bytes": len(body), "media_type": "image/jpeg", "review_required": true})
	return v2region.AuthoredRegionMedia{
		Proof:      v2region.AuthoredRegionMediaProof{Tenant: uuid.New(), Asset: uuid.New(), Actor: uuid.New(), Session: uuid.New(), Revision: 1, Canonical: v2region.AuthoredCropVerification{Request: id, Run: uuid.New(), Task: uuid.New(), Result: uuid.New(), Root: root, IntentSHA: intent, RootSHA: digest(manifest), Attempt: 1, Fence: 1}},
		Descriptor: v2region.AuthoredRegionMediaDescriptor{SchemaVersion: 1, Request: id, Revision: 1, IntentSHA: intent, Manifest: v2input.Ref{ArtifactID: root, SHA256: digest(manifest)}, ManifestBytes: manifest, Derivative: derivative, Bytes: int64(len(body)), MediaType: "image/jpeg", ReviewRequired: true}, Body: body,
	}
}

type authoredMediaReadyReader struct {
	media    v2region.AuthoredRegionMedia
	checkErr error
	checks   int
	mutate   bool
}

func (s *authoredMediaReadyReader) Read(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID) (v2region.AuthoredRegionMedia, error) {
	return s.media, nil
}
func (s *authoredMediaReadyReader) Recheck(_ context.Context, _ auth.Identity, _ uuid.UUID, _ uuid.UUID, _ uuid.UUID, p v2region.AuthoredRegionMediaProof) error {
	s.checks++
	if p != s.media.Proof {
		return v2region.ErrRegionInvalid
	}
	if s.mutate {
		s.media.Body[0] = 0
		s.media.Descriptor.ManifestBytes[0] = '!'
	}
	return s.checkErr
}
func TestAuthoredMediaSuccessAndLastMomentAuthorityWithdrawal(t *testing.T) {
	for _, mode := range []string{"descriptor", "derivative", "withdrawn", "foreign", "buffer mutation"} {
		t.Run(mode, func(t *testing.T) {
			m := authoredHTTPMediaFixture()
			reader := &authoredMediaReadyReader{media: m}
			expected := append([]byte(nil), m.Body...)
			who := auth.Identity{UserID: m.Proof.Actor, SessionID: m.Proof.Session}
			if mode == "withdrawn" {
				reader.checkErr = v2input.ErrConflict
			}
			if mode == "foreign" {
				reader.media.Proof.Actor = uuid.New()
			}
			reader.mutate = mode == "buffer mutation"
			suffix := "derivative"
			if mode == "descriptor" {
				suffix = "descriptor"
			}
			router := chi.NewRouter()
			RegisterAuthoredRegionMediaRoutes(router, func(*http.Request, bool) (auth.Identity, uuid.UUID, error) { return who, m.Proof.Tenant, nil }, reader)
			req := httptest.NewRequest("GET", "/api/v1/tenants/"+m.Proof.Tenant.String()+"/assets/"+m.Proof.Asset.String()+"/authored-region-previews/"+m.Descriptor.Request.String()+"/"+suffix, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if mode == "foreign" {
				if w.Code != 404 || reader.checks != 0 {
					t.Fatal("foreign proof accepted", w.Code, reader.checks)
				}
				return
			}
			if reader.checks != 1 {
				t.Fatal("missing final authority check")
			}
			if mode == "withdrawn" {
				if w.Code != 409 || bytes.Equal(w.Body.Bytes(), expected) {
					t.Fatal("withdrawn bytes exposed", w.Code)
				}
				return
			}
			if w.Code != 200 {
				t.Fatal("valid media rejected", w.Code, w.Body.String())
			}
			if mode == "descriptor" {
				var d v2region.AuthoredRegionMediaDescriptor
				if json.Unmarshal(w.Body.Bytes(), &d) != nil || !bytes.Equal(d.ManifestBytes, m.Descriptor.ManifestBytes) || !d.ReviewRequired {
					t.Fatal("manifest or review changed")
				}
			} else if !bytes.Equal(w.Body.Bytes(), expected) || w.Header().Get("Content-Type") != "image/jpeg" {
				t.Fatal("body changed or wire type lost")
			}
		})
	}
}
