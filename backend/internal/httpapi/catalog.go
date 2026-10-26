package httpapi

import (
 "photoeditor/backend/internal/idempotency"
	"context"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"io"
	"net/http"
	adminservice "photoeditor/backend/internal/admin"
	assetservice "photoeditor/backend/internal/asset"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/capability"
	collectionservice "photoeditor/backend/internal/collection"
	contactsheetservice "photoeditor/backend/internal/contactsheet"
	"photoeditor/backend/internal/domain"
	downloadservice "photoeditor/backend/internal/download"
	"photoeditor/backend/internal/editor"
	"photoeditor/backend/internal/eventstream"
	"photoeditor/backend/internal/exportjob"
	"photoeditor/backend/internal/exportpreset"
	interactiveservice "photoeditor/backend/internal/interactive"
	"photoeditor/backend/internal/legacyimport"
	"photoeditor/backend/internal/media"
	"photoeditor/backend/internal/pagination"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/review"
	runservice "photoeditor/backend/internal/run"
	styleservice "photoeditor/backend/internal/style"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/upload"
	"photoeditor/backend/internal/v2capture"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2learning"
	"photoeditor/backend/internal/v2observation"
	"photoeditor/backend/internal/v2people"
	"photoeditor/backend/internal/v2sessions"
	"photoeditor/backend/internal/v2workflow"
	"strconv"
	"time"
)

func revision(r *http.Request) (int64, error) {
	h := r.Header.Get("If-Match")
	if h == "" {
		return 0, domain.NewError("PRECONDITION_REQUIRED", 428)
	}
	if len(h) < 3 || h[0] != '"' || h[len(h)-1] != '"' {
		return 0, domain.NewError("VALIDATION_FAILED", 422)
	}
	n, err := strconv.ParseInt(h[1:len(h)-1], 10, 64)
	if err != nil || n < 1 || strconv.FormatInt(n, 10) != h[1:len(h)-1] {
		return 0, domain.NewError("VALIDATION_FAILED", 422)
	}
	return n, nil
}
func revisionAllowZero(r *http.Request) (int64, error) {
	h := r.Header.Get("If-Match")
	if h == "" {
		return 0, domain.NewError("PRECONDITION_REQUIRED", 428)
	}
	if len(h) < 3 || h[0] != '"' || h[len(h)-1] != '"' {
		return 0, domain.NewError("VALIDATION_FAILED", 422)
	}
	n, err := strconv.ParseInt(h[1:len(h)-1], 10, 64)
	if err != nil || n < 0 || strconv.FormatInt(n, 10) != h[1:len(h)-1] {
		return 0, domain.NewError("VALIDATION_FAILED", 422)
	}
	return n, nil
}
func limit(r *http.Request) (int, error) {
	s := r.URL.Query().Get("limit")
	if s == "" {
		return 50, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 100 {
		return 0, domain.NewError("VALIDATION_FAILED", 422)
	}
	return n, nil
}
func routeID(r *http.Request, key string) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, key))
	if err != nil {
		return uuid.Nil, domain.NewError("NOT_FOUND", 404)
	}
	return id, nil
}
func (s *Server) mutation(r *http.Request) (auth.Identity, error) {
	who, err := s.authenticate(r)
	if err != nil {
		return who, err
	}
	if err = s.checkOrigin(r); err != nil {
		return who, err
	}
	if !s.native(r) {
		err = s.Auth.ValidateSessionCSRF(r.Context(), who.SessionID, r.Header.Get("X-CSRF-Token"))
	}
	return who, err
}
func (s *Server) catalogRoutes(r chi.Router) {
 s.stylePackageUploadRoutes(r)
	tenants := tenant.Service{DB: s.DB, Cursors: pagination.Codec{Key: auth.MAC(s.Auth.Pepper, "cursor-key")}, MailKeys: s.Auth.MailKeys}
	projects := project.Service{DB: s.DB, Cursors: tenants.Cursors}
	collections := collectionservice.Service{DB: s.DB, Cursors: tenants.Cursors, Pepper: s.Auth.Pepper}
	var runObjects runservice.OutputSigner
	var runReadObjects runservice.OutputReader
	var runBucket string
	if s.Media != nil {
		runObjects, _ = s.Media.Objects.(runservice.OutputSigner)
		runReadObjects, _ = s.Media.Objects.(runservice.OutputReader)
		runBucket = s.Media.Bucket
	}
	runs := runservice.Service{DB: s.DB, Cursors: tenants.Cursors, Objects: runObjects,
		ReadObjects: runReadObjects, OutputKey: auth.MAC(s.Auth.Pepper, "v26-run-output-capability"),
		PublicOrigin: s.Config.APIPublicOrigin, Bucket: runBucket}
 s.collectionApplyCapabilityRoutes(r,runs)
	downloads := downloadservice.Service{DB: s.DB, Pepper: s.Auth.Pepper}
 if s.Portal!=nil{downloads.DownloadPolicy=s.Portal.DownloadPolicy}
	assets := assetservice.Service{DB: s.DB, Cursors: tenants.Cursors}
 s.versionNavigationRoutes(r, assets)
	capabilities := capability.Service{DB: s.DB, SemanticInstalled: s.semanticOperationInstalled}
	events := eventstream.Service{DB: s.DB}
	var styleObjects styleservice.DownloadSigner
	var styleBucket string
	if s.Media != nil {
		styleObjects, _ = s.Media.Objects.(styleservice.DownloadSigner)
		styleBucket = s.Media.Bucket
	}
	styles := styleservice.Service{DB: s.DB, Cursors: tenants.Cursors, Objects: styleObjects, Bucket: styleBucket}
	s.styleAnalysisCapabilityRoutes(r,styles)
	edits := editor.Service{DB: s.DB}
	interactive := interactiveservice.Service{DB: s.DB}
	presets := exportpreset.Service{DB: s.DB, Cursors: tenants.Cursors}
	exports := exportjob.Service{DB: s.DB, DeliveryCreator:s.DeliveryCreator}
 if s.Portal!=nil{exports.DownloadPolicy=s.Portal.DownloadPolicy}
	legacyImports := legacyimport.Service{DB: s.DB}
	adminAPI := adminservice.Service{DB: s.DB, Cursors: tenants.Cursors}
	var observationObjects v2observation.ObjectReader
	var observationBucket string
	if s.Media != nil {
		observationObjects, _ = s.Media.Objects.(v2observation.ObjectReader)
		observationBucket = s.Media.Bucket
	}
	observations := v2observation.Service{DB: s.DB, Objects: observationObjects, Bucket: observationBucket}
	people := v2people.Service{DB: s.DB, Cursors: pagination.Codec{
		Key: auth.MAC(s.Auth.Pepper, "cursor-key"), TTL: 15 * time.Minute}}
	var sessionObjects v2sessions.ObjectReader
	var sessionBucket string
	if s.Media != nil {
		sessionObjects, _ = s.Media.Objects.(v2sessions.ObjectReader)
		sessionBucket = s.Media.Bucket
	}
	sessionsV2 := v2sessions.Service{DB: s.DB, Cursors: people.Cursors,
		Objects: sessionObjects, Bucket: sessionBucket,
		Admission: s.AnalysisAdmission}
	s.captureCapabilityRoutes(r,sessionsV2)
	s.sessionAnalysisCapabilityRoutes(r,sessionsV2)
	draftsV2 := v2draft.Service{DB: s.DB, Cursors: people.Cursors}
	captureV2 := v2capture.Service{DB: s.DB, Cursors: tenants.Cursors}
	r.Get("/api/v1/tenants/{t}/shoot-sessions/{id}/alerts", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		pageLimit, err := limit(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := captureV2.ListAlerts(r.Context(), who, tid, id, r.URL.Query().Get("cursor"), pageLimit)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
	r.Post("/api/v1/tenants/{t}/capture-receipts/{id}/confirm", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		var req v2capture.ConfirmFrameRequest
		if err = decodeJSON(r, &req); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := captureV2.ConfirmFrame(r.Context(), who, tid, id, r.Header.Get("Idempotency-Key"), req)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if out.Replayed {
			w.Header().Set("Idempotency-Replayed", "true")
		}
		writeJSON(w, out.Status, out.Body)
	})
	r.Post("/api/v1/tenants/{t}/capture-alerts/{id}/ack", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request v2capture.RevokeRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := captureV2.Acknowledge(r.Context(), who, tid, id, r.Header.Get("Idempotency-Key"), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if out.Replayed {
			w.Header().Set("Idempotency-Replayed", "true")
		}
		writeJSON(w, out.Status, out.Body)
	})
	capturePublic := v2capture.PublicService{DB: s.DB, Credentials: v2capture.Credentials{
		Signer: s.Auth.Signer, Pepper: s.Auth.Pepper, Origin: s.Config.CaptureOrigin}}
	deviceRequest := func(r *http.Request) (v2capture.DevicePrincipal, error) {
		tid, err := routeID(r, "t")
		if err != nil {
			return v2capture.DevicePrincipal{}, err
		}
		return capturePublic.AuthenticateDevice(r.Context(), r.Header.Get("Authorization"), tid)
	}
	r.Post("/api/v1/tenants/{t}/shoot-sessions/{id}/capture-receipts", func(w http.ResponseWriter, r *http.Request) {
		principal, err := deviceRequest(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request v2capture.ReceiptRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := capturePublic.AcceptReceipt(r.Context(), principal, id, r.Header.Get("Idempotency-Key"), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if out.Replayed {
			w.Header().Set("Idempotency-Replayed", "true")
		}
		writeJSON(w, out.Status, out.Body)
	})
	r.Post("/api/v1/tenants/{t}/capture-devices/{id}/challenges", func(w http.ResponseWriter, r *http.Request) {
		tid, err := routeID(r, "t")
		if err != nil {
			writeError(w, r, err)
			return
		}
		deviceID, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request v2capture.ChallengeRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		ip := s.clientIP(r)
		out, err := capturePublic.CreateChallenge(r.Context(), tid, deviceID, ip, request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
	r.Post("/api/v1/tenants/{t}/capture-devices/{id}/token", func(w http.ResponseWriter, r *http.Request) {
		tid, err := routeID(r, "t")
		if err != nil {
			writeError(w, r, err)
			return
		}
		deviceID, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request v2capture.TokenRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		ip := s.clientIP(r)
		out, err := capturePublic.ExchangeChallenge(r.Context(), tid, deviceID, ip, request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
	r.Post("/api/v1/tenants/{t}/capture-devices", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request v2capture.RegisterRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := captureV2.Register(r.Context(), who, tid,
			r.Header.Get("Idempotency-Key"), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if out.Replayed {
			w.Header().Set("Idempotency-Replayed", "true")
		}
		writeJSON(w, out.Status, out.Body)
	})
	r.Post("/api/v1/tenants/{t}/capture-devices/{id}/revoke", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		deviceID, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request v2capture.RevokeRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := captureV2.Revoke(r.Context(), who, tid, deviceID,
			r.Header.Get("Idempotency-Key"), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if out.Replayed {
			w.Header().Set("Idempotency-Replayed", "true")
		}
		writeJSON(w, out.Status, out.Body)
	})
	r.Post("/api/v1/tenants/{t}/capture-uploads", func(w http.ResponseWriter, r *http.Request) {
		principal, err := deviceRequest(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if s.Uploads == nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		var request upload.DeviceBeginRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := s.Uploads.BeginDevice(r.Context(), principal, r.Header.Get("Idempotency-Key"), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("ETag", `"`+out.Revision+`"`)
		writeJSON(w, 201, out)
	})
	r.Get("/api/v1/tenants/{t}/capture-uploads/{id}", func(w http.ResponseWriter, r *http.Request) {
		principal, err := deviceRequest(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if s.Uploads == nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := s.Uploads.GetDevice(r.Context(), principal, id)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("ETag", `"`+out.Revision+`"`)
		writeJSON(w, 200, out)
	})
	r.Post("/api/v1/tenants/{t}/capture-uploads/{id}/parts", func(w http.ResponseWriter, r *http.Request) {
		principal, err := deviceRequest(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if s.Uploads == nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request upload.PartRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := s.Uploads.SignDeviceParts(r.Context(), principal, id, request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, 200, out)
	})
	r.Post("/api/v1/tenants/{t}/capture-uploads/{id}/complete", func(w http.ResponseWriter, r *http.Request) {
		principal, err := deviceRequest(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if s.Uploads == nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request upload.CompleteRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := s.Uploads.CompleteDevice(r.Context(), principal, id,
			r.Header.Get("Idempotency-Key"), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, 202, out)
	})
	correctionsV2 := v2learning.CorrectionService{DB: s.DB}
	profilesV2 := v2learning.ProfileService{DB: s.DB, Cursors: people.Cursors,
		Training: s.TrainingAdmission}
	workflowsV2 := v2workflow.Service{DB: s.DB}
	r.Get("/api/v1/tenants/{t}/v2-workflows/{id}", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		workflowID, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := workflowsV2.Get(r.Context(), who, tid, workflowID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
	r.Post("/api/v1/tenants/{t}/v2-workflows/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		workflowID, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request v2workflow.CancelRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := workflowsV2.Cancel(r.Context(), who, tid, workflowID,
			r.Header.Get("Idempotency-Key"), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if out.Replayed {
			w.Header().Set("Idempotency-Replayed", "true")
		}
		writeJSON(w, out.Status, out.Body)
	})
	if s.Media != nil {
		draftsV2.Objects, _ = s.Media.Objects.(v2draft.DraftStore)
		draftsV2.Bucket = s.Media.Bucket
	}

 s.editorCapabilityRoutes(r,runs,draftsV2)
 r.Get("/api/v1/tenants/{t}/projects/{id}/editor-effects/{build}",func(w http.ResponseWriter,r *http.Request){
  who,tid,err:=s.tenantRequest(r,false);if err!=nil{writeError(w,r,err);return}
  pid,err:=routeID(r,"id");if err!=nil{writeError(w,r,err);return};build,err:=routeID(r,"build");if err!=nil{writeError(w,r,err);return}
  out,err:=(runservice.EditorEffectsService{Runs:runs,Drafts:draftsV2}).Status(r.Context(),who,tid,pid,build);if err!=nil{writeError(w,r,err);return};writeJSON(w,http.StatusOK,out)
 })
 r.Post("/api/v1/tenants/{t}/projects/{id}/editor-effects", func(w http.ResponseWriter,r *http.Request){
  who,tid,err:=s.tenantRequest(r,true);if err!=nil{writeError(w,r,err);return}
  pid,err:=routeID(r,"id");if err!=nil{writeError(w,r,err);return}
  var req runservice.EditorEffectRequest;if err=decodeJSON(r,&req);err!=nil{writeError(w,r,err);return}
  out,err:=(runservice.EditorEffectsService{Runs:runs,Drafts:draftsV2,FreshRenderInstalled:s.RenderCalibrations!=nil,Calibrations:s.RenderCalibrations}).Begin(r.Context(),who,tid,pid,r.Header.Get("Idempotency-Key"),req)
  if err!=nil{writeError(w,r,err);return};if out.Replayed{w.Header().Set("Idempotency-Replayed","true")}
  w.Header().Set("Content-Type","application/json");w.WriteHeader(out.Status);_,_=w.Write(out.Body)
 })
	correctionsV2.Drafts = draftsV2
	r.Post("/api/v1/tenants/{t}/corrections", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request v2learning.CorrectionCreate
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := correctionsV2.RecordCorrection(r.Context(), who, tid,
			r.Header.Get("Idempotency-Key"), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if out.Replayed {
			w.Header().Set("Idempotency-Replayed", "true")
		}
		writeJSON(w, out.Status, out.Body)
	})
	r.Post("/api/v1/tenants/{t}/learning-profiles", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request v2learning.ProfileCreate
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := profilesV2.CreateProfile(r.Context(), who, tid,
			r.Header.Get("Idempotency-Key"), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if out.Replayed {
			w.Header().Set("Idempotency-Replayed", "true")
		}
		writeJSON(w, out.Status, out.Body)
	})
	r.Post("/api/v1/tenants/{t}/learning-profiles/{id}/train", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		profileID, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request v2learning.TrainCreate
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		ticket, replayed, err := profilesV2.TrainProfile(r.Context(), who,
			tid, profileID, r.Header.Get("Idempotency-Key"), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if replayed {
			w.Header().Set("Idempotency-Replayed", "true")
		}
		writeJSON(w, http.StatusAccepted, ticket)
	})
	r.Get("/api/v1/tenants/{t}/learning-profiles/{id}", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		profileID, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := profilesV2.GetProfile(r.Context(), who, tid, profileID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
	r.Get("/api/v1/tenants/{t}/learning-profiles/{id}/versions", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		profileID, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		pageLimit, err := limit(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := profilesV2.ListVersions(r.Context(), who, tid, profileID,
			pageLimit, r.URL.Query().Get("cursor"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
	r.Post("/api/v1/tenants/{t}/learning-profiles/{id}/activate", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		profileID, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request v2learning.ActivateProfileRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := profilesV2.ActivateProfile(r.Context(), who, tid, profileID,
			r.Header.Get("Idempotency-Key"), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if out.Replayed {
			w.Header().Set("Idempotency-Replayed", "true")
		}
		writeJSON(w, out.Status, out.Body)
	})
	r.Post("/api/v1/tenants/{t}/learning-versions/{id}/safety-review", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		versionID, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request v2learning.LearningSafetyReviewRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := profilesV2.ReviewLearningSafety(r.Context(), who, tid, versionID,
			r.Header.Get("Idempotency-Key"), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if out.Replayed {
			w.Header().Set("Idempotency-Replayed", "true")
		}
		writeJSON(w, out.Status, out.Body)
	})
	r.Get("/api/v1/tenants/{t}/assets/{id}/v2-layers/{artifact}", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		asset, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		artifact, err := routeID(r, "artifact")
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := draftsV2.GetLayerStack(r.Context(), who, tid, asset, v2input.Ref{ArtifactID: artifact, SHA256: r.URL.Query().Get("sha256")})
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, out)
	})
	r.Get("/api/v1/tenants/{t}/v2-documents/{id}", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		documentID, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := draftsV2.GetDocument(r.Context(), who, tid, documentID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
	r.Put("/api/v1/tenants/{t}/assets/{id}/v2-draft", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		assetID, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request v2draft.ReplaceRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := draftsV2.Replace(r.Context(), who, tid, assetID,
			r.Header.Get("Idempotency-Key"), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if out.Replayed {
			w.Header().Set("Idempotency-Replayed", "true")
		}
		writeJSON(w, out.Status, out.Body)
	})
	r.Put("/api/v1/tenants/{t}/shoot-sessions/{id}/shot-list", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		var req v2draft.ShotListReplaceRequest
		if err = decodeJSON(r, &req); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := draftsV2.ReplaceShotList(r.Context(), who, tid, id, r.Header.Get("Idempotency-Key"), req)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if out.Replayed {
			w.Header().Set("Idempotency-Replayed", "true")
		}
		writeJSON(w, out.Status, out.Body)
	})
	r.Get("/api/v1/tenants/{t}/shoot-sessions/{id}/shot-list", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := draftsV2.GetShotList(r.Context(), who, tid, id)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
	r.Get("/api/v1/tenants/{t}/assets/{id}/v2-draft", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		assetID, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := draftsV2.Get(r.Context(), who, tid, assetID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
	r.Get("/api/v1/tenants/{t}/assets/{id}/v2-binding", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		assetID, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := draftsV2.Binding(r.Context(), who, tid, assetID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
	r.Post("/api/v1/tenants/{t}/assets/{id}/v2-draft/approve", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		assetID, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request v2draft.ApprovalRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := draftsV2.Approve(r.Context(), who, tid, assetID,
			r.Header.Get("Idempotency-Key"), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if out.Replayed {
			w.Header().Set("Idempotency-Replayed", "true")
		}
		writeJSON(w, out.Status, out.Body)
	})
	r.Get("/api/v1/tenants/{t}/assets/{id}/v2-draft/history", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		assetID, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		pageLimit, err := limit(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := draftsV2.History(r.Context(), who, tid, assetID,
			pageLimit, r.URL.Query().Get("cursor"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
	r.Post("/api/v1/tenants/{t}/shoot-sessions/{id}/analyses", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		sessionID, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request v2sessions.AnalysisRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		ticket, replayed, err := sessionsV2.Analyze(r.Context(), who, tid,
			sessionID, r.Header.Get("Idempotency-Key"), request)
		if r.Context().Err() != nil { return }
		if err != nil {
			writeError(w, r, err)
			return
		}
		if replayed {
			w.Header().Set("Idempotency-Replayed", "true")
		}
		writeJSON(w, http.StatusAccepted, ticket)
	})
	r.Get("/api/v1/tenants/{t}/shoot-sessions/{id}/analyses", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		size, err := limit(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := sessionsV2.ListAnalyses(r.Context(), who, tid, id, size, r.URL.Query().Get("cursor"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, out)
	})
	r.Get("/api/v1/tenants/{t}/session-analyses/{id}", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		analysisID, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := sessionsV2.GetAnalysis(r.Context(), who, tid, analysisID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
	r.Get("/api/v1/tenants/{t}/session-analyses/{id}/scene-board", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		analysisID, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := sessionsV2.GetSceneBoard(r.Context(), who, tid, analysisID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, out)
	})
	r.Post("/api/v1/tenants/{t}/session-analyses/{id}/apply", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		analysisID, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request v2sessions.ApplyDecision
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		ticket, replayed, err := sessionsV2.ApplyAnalysis(r.Context(), who, tid,
			analysisID, r.Header.Get("Idempotency-Key"), request)
		if r.Context().Err() != nil {
			return
		}
		if err != nil {
			writeError(w, r, err)
			return
		}
		if replayed {
			w.Header().Set("Idempotency-Replayed", "true")
		}
		writeJSON(w, http.StatusAccepted, ticket)
	})
	r.Get("/api/v1/tenants/{t}/shoot-sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		sessionID, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := sessionsV2.Get(r.Context(), who, tid, sessionID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
	r.Get("/api/v1/tenants/{t}/shoot-sessions", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		projectID, err := uuid.Parse(r.URL.Query().Get("project_id"))
		if err != nil {
			writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
			return
		}
		pageLimit, err := limit(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := sessionsV2.List(r.Context(), who, tid, projectID,
			pageLimit, r.URL.Query().Get("cursor"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
	r.Post("/api/v1/tenants/{t}/shoot-sessions", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request v2sessions.CreateRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := sessionsV2.Create(r.Context(), who, tid, r.Header.Get("Idempotency-Key"), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if out.Replayed {
			w.Header().Set("Idempotency-Replayed", "true")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(out.Status)
		_, _ = w.Write(out.Body)
	})
	r.Patch("/api/v1/tenants/{t}/shoot-sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		sessionID, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request v2sessions.UpdateRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := sessionsV2.Update(r.Context(), who, tid, sessionID,
			r.Header.Get("Idempotency-Key"), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if out.Replayed {
			w.Header().Set("Idempotency-Replayed", "true")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(out.Status)
		_, _ = w.Write(out.Body)
	})
	for _, listing := range []struct{ path, kind string }{
		{"/api/v1/tenants/{t}/projects/{id}/persons", "project_persons"},
		{"/api/v1/tenants/{t}/projects/{id}/consents", "consent_grants"},
	} {
		kind := listing.kind
		r.Get(listing.path, func(w http.ResponseWriter, r *http.Request) {
			who, tid, err := s.tenantRequest(r, false)
			if err != nil {
				writeError(w, r, err)
				return
			}
			projectID, err := routeID(r, "id")
			if err != nil {
				writeError(w, r, err)
				return
			}
			pageLimit, err := limit(r)
			if err != nil {
				writeError(w, r, err)
				return
			}
			out, err := people.List(r.Context(), who, tid, projectID, kind,
				pageLimit, r.URL.Query().Get("cursor"))
			if err != nil {
				writeError(w, r, err)
				return
			}
			writeJSON(w, http.StatusOK, out)
		})
	}
	r.Post("/api/v1/tenants/{t}/project-persons", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request v2people.CreateRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := people.Create(r.Context(), who, tid, r.Header.Get("Idempotency-Key"), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if out.Replayed {
			w.Header().Set("Idempotency-Replayed", "true")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(out.Status)
		_, _ = w.Write(out.Body)
	})
	r.Post("/api/v1/tenants/{t}/project-persons/{id}/revoke", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		personID, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request v2people.CASRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := people.Revoke(r.Context(), who, tid, personID, r.Header.Get("Idempotency-Key"), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if out.Replayed {
			w.Header().Set("Idempotency-Replayed", "true")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(out.Status)
		_, _ = w.Write(out.Body)
	})
	r.Post("/api/v1/tenants/{t}/projects/{id}/consents", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		projectID, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request v2people.ConsentRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := people.ChangeConsent(r.Context(), who, tid, projectID, r.Header.Get("Idempotency-Key"), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if out.Replayed {
			w.Header().Set("Idempotency-Replayed", "true")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(out.Status)
		_, _ = w.Write(out.Body)
	})
	r.Post("/api/v1/tenants/{t}/project-persons/{id}/observations", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		personID, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request v2observation.ConfirmRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := observations.Confirm(r.Context(), who, tid, personID, r.Header.Get("Idempotency-Key"), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if out.Replayed {
			w.Header().Set("Idempotency-Replayed", "true")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(out.Status)
		_, _ = w.Write(out.Body)
	})
	var contactSheets contactsheetservice.Service
	if s.ContactSheets != nil {
		contactSheets = *s.ContactSheets
	}
	r.Post("/api/v1/tenants/{t}/contact-sheets/abandon",func(w http.ResponseWriter,r *http.Request){
		who,tid,err:=s.tenantRequest(r,true);if err!=nil{writeError(w,r,err);return}
		if s.ContactSheets==nil{writeError(w,r,domain.NewError("DEPENDENCY_UNAVAILABLE",503));return}
		var request contactsheetservice.CreateRequest
		if err=decodeJSON(r,&request);err!=nil{writeError(w,r,err);return}
		out,err:=contactSheets.Abandon(r.Context(),who,tid,r.Header.Get("Idempotency-Key"),request)
		if r.Context().Err()!=nil{return};if err!=nil{writeError(w,r,err);return}
		if out.Replayed{w.Header().Set("Idempotency-Replayed","true")};w.Header().Set("Content-Type","application/json");w.WriteHeader(out.Status);_,_=w.Write(out.Body)
	})
	r.Post("/api/v1/tenants/{t}/contact-sheets", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if s.ContactSheets == nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		var request contactsheetservice.CreateRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := contactSheets.Create(r.Context(), who, tid, r.Header.Get("Idempotency-Key"), request)
		if r.Context().Err() != nil { return }
		if err != nil {
			writeError(w, r, err)
			return
		}
		if out.Replayed {
			w.Header().Set("Idempotency-Replayed", "true")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(out.Status)
		_, _ = w.Write(out.Body)
	})
	r.Post("/api/v1/tenants/{t}/masks", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request interactiveservice.MaskCreate
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := interactive.CreateMask(r.Context(), who, tid, r.Header.Get("Idempotency-Key"), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if out.Replayed {
			w.Header().Set("Idempotency-Replayed", "true")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(out.Status)
		_, _ = w.Write(out.Body)
	})
	r.Post("/api/v1/tenants/{t}/generations", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request interactiveservice.GenerateRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
        replayOnly,modeErr:=generationReplayOnly(r.Header)
        if modeErr!=nil {writeError(w,r,modeErr);return}
        submit:=interactive.Generate
        if replayOnly {submit=interactive.ReplayGeneration}
		out, err := submit(r.Context(), who, tid, r.Header.Get("Idempotency-Key"), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if out.Replayed {
			w.Header().Set("Idempotency-Replayed", "true")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(out.Status)
		_, _ = w.Write(out.Body)
	})
	r.Post("/api/v1/tenants/{t}/generations/abandon", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil { writeError(w, r, err); return }
		var request interactiveservice.GenerateRequest
		if err = decodeJSON(r, &request); err != nil { writeError(w, r, err); return }
		out, err := interactive.AbandonGeneration(r.Context(), who, tid, r.Header.Get("Idempotency-Key"), request)
		if err != nil { writeError(w, r, err); return }
		if out.Replayed { w.Header().Set("Idempotency-Replayed", "true") }
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(out.Status)
		_, _ = w.Write(out.Body)
	})

	r.Post("/api/v1/tenants/{t}/previews", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request interactiveservice.PreviewRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := interactive.Preview(r.Context(), who, tid, r.Header.Get("Idempotency-Key"), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if out.Replayed {
			w.Header().Set("Idempotency-Replayed", "true")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(out.Status)
		_, _ = w.Write(out.Body)
	})
	r.Post("/api/v1/tenants/{t}/collections/{id}/save-edits", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request runservice.SaveEditsRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
        resolve:=r.Header.Values("Save-Edits-Resolve-Only")
        if len(resolve)>1||len(resolve)==1&&resolve[0]!="abandon"{writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
        var out idempotency.Response
        if len(resolve)==1{out,err=runs.ResolveSaveEdits(r.Context(),who,tid,id,r.Header.Get("Idempotency-Key"),request)}else{out,err=runs.SaveEdits(r.Context(),who,tid,id,r.Header.Get("Idempotency-Key"),request)}
		if err != nil {
			writeError(w, r, err)
			return
		}
		if out.Replayed {
			w.Header().Set("Idempotency-Replayed", "true")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(out.Status)
		_, _ = w.Write(out.Body)
	})
 r.Get("/api/v1/tenants/{t}/exports/batches/{id}",func(w http.ResponseWriter,r *http.Request){
  who,tid,err:=s.tenantRequest(r,false);if err!=nil{writeError(w,r,err);return}
  id,err:=uuid.Parse(chi.URLParam(r,"id"));if err!=nil{writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
  out,err:=exports.GetDeliveryBatch(r.Context(),who,tid,id);if err!=nil{writeError(w,r,err);return};writeJSON(w,http.StatusOK,out)
 })
	r.Post("/api/v1/tenants/{t}/exports", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request exportjob.CreateRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := exports.Create(r.Context(), who, tid, r.Header.Get("Idempotency-Key"), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if out.Replayed {
			w.Header().Set("Idempotency-Replayed", "true")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(out.Status)
		_, _ = w.Write(out.Body)
	})
	r.Post("/api/v1/tenants/{t}/assets/search-similar", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request assetservice.SimilarRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := assets.SearchSimilar(r.Context(), who, tid, request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
	r.Post("/api/v1/tenants/{t}/styles/import", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request styleservice.ImportRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := styles.Import(r.Context(), who, tid, r.Header.Get("Idempotency-Key"), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if out.Replayed {
			w.Header().Set("Idempotency-Replayed", "true")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(out.Status)
		_, _ = w.Write(out.Body)
	})
	r.Get("/api/v1/admin/dead-letters", func(w http.ResponseWriter, r *http.Request) {
		who, err := s.authenticate(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		n, err := limit(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := adminAPI.ListDeadLetters(r.Context(), who, n, r.URL.Query().Get("cursor"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
	r.Post("/api/v1/admin/dead-letters/{id}/replay", func(w http.ResponseWriter, r *http.Request) {
		who, err := s.mutation(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request adminservice.ReplayRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := adminAPI.Replay(r.Context(), who, id, r.Header.Get("Idempotency-Key"), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusAccepted, out)
	})
	r.Post("/api/v1/tenants/{t}/legacy-imports/reconcile", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request legacyimport.ReconcileRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := legacyImports.Reconcile(r.Context(), who, tid, request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if r.Context().Err()!=nil { return }
		writeJSON(w, http.StatusOK, out)
	})
	r.Get("/api/v1/tenants/{t}/reviews", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if s.Reviews == nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		n, err := limit(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := s.Reviews.List(r.Context(), who, tid, n, r.URL.Query().Get("cursor"), r.URL.Query().Get("run_id"), r.URL.Query().Get("state"), r.URL.Query().Get("kind"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, 200, out)
	})
	r.Get("/api/v1/tenants/{t}/reviews/{id}/previews/{side}/{variant}", s.serveReviewPreview)
	r.Get("/api/v1/media/review-pending", s.servePendingReviewMedia)
	r.Get("/api/v1/tenants/{t}/reviews/{id}/culling-items", s.serveCullingReviewItems)
	r.Post("/api/v1/tenants/{t}/reviews/{id}/culling-decision", s.serveCullingReviewDecision)
	r.Post("/api/v1/tenants/{t}/reviews/{id}/relight-viewed", s.serveRelightReviewViewed)
	r.Post("/api/v1/tenants/{t}/reviews/{id}/relight-apply", s.serveRelightReviewApply)
 r.Post("/api/v1/tenants/{t}/reviews/{id}/open-eyes-apply", s.serveOpenEyesReviewApply)
 r.Post("/api/v1/tenants/{t}/reviews/{id}/local-portrait-apply", s.serveLocalPortraitReviewApply)
 r.Get("/api/v1/tenants/{t}/runs/{id}/local-portrait-result", s.serveLocalPortraitDiscovery)
 r.Get("/api/v1/tenants/{t}/v2-workflows/{id}/local-portrait-result", s.serveLocalPortraitResult)
 r.Post("/api/v1/tenants/{t}/v2-workflows/{id}/local-portrait-apply", s.serveLocalPortraitResultApply)
 r.Post("/api/v1/tenants/{t}/reviews/{id}/glare-apply", s.serveGlareReviewApply)
 r.Post("/api/v1/tenants/{t}/reviews/{id}/reflection-apply", s.serveReflectionReviewApply)
 r.Post("/api/v1/tenants/{t}/reviews/{id}/negative-apply", s.serveNegativeReviewApply)
	r.Get("/api/v1/tenants/{t}/reviews/{id}/relight-collection-targets", s.serveRelightCollectionTargets)
	r.Get("/api/v1/tenants/{t}/reviews/{id}/selected-preview/{variant}", s.serveSelectedRenderPreview)
	r.Get("/api/v1/media/render-selected", s.serveSelectedRenderMedia)
	r.Get("/api/v1/media/restoration-selected", s.serveSelectedRestorationMedia)
	r.Get("/api/v1/tenants/{t}/reviews/{id}/restoration-selected-preview/{variant}", s.serveSelectedRestorationPreview)
	r.Get("/api/v1/tenants/{t}/reviews/{id}/restoration-result-selection", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		if s.Reviews == nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		out, err := s.Reviews.ReadRestorationResultSelection(r.Context(), who, tid, id)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if r.Context().Err() != nil {
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
	r.Get("/api/v1/tenants/{t}/reviews/{id}/render-result-selection", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		if s.Reviews == nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		out, err := s.Reviews.ReadRenderResultSelection(r.Context(), who, tid, id)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if r.Context().Err() != nil {
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
	r.Get("/api/v1/tenants/{t}/reviews/{id}/result-selection", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		if s.Reviews == nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		out, err := s.Reviews.ReadNegativeResultSelection(r.Context(), who, tid, id)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if err = r.Context().Err(); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
	r.Get("/api/v1/tenants/{t}/reviews/{id}", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
			return
		}
		if s.Reviews == nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		out, err := s.Reviews.Detail(r.Context(), who, tid, id)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
	r.Post("/api/v1/tenants/{t}/reviews/{id}/decision", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if s.Reviews == nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request review.Decision
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		requestID := uuid.New()
		out, err := s.Reviews.Decide(r.Context(), who, tid, id, requestID, request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("ETag", `"`+strconv.FormatInt(out.Revision, 10)+`"`)
		writeJSON(w, 200, out)
	})
	r.Get("/api/v1/tenants/{t}/collections", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		n, err := limit(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := collections.List(r.Context(), who, tid, n, r.URL.Query().Get("cursor"), r.URL.Query().Get("project_id"), r.URL.Query().Get("state"), r.URL.Query().Get("search"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, 200, out)
	})
	r.Post("/api/v1/tenants/{t}/collections", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request collectionservice.Create
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := collections.Create(r.Context(), who, tid, r.Header.Get("Idempotency-Key"), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(out.Status)
		_, _ = w.Write(out.Body)
	})
	r.Get("/api/v1/tenants/{t}/collections/{id}", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := collections.Get(r.Context(), who, tid, id)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("ETag", `"`+out.Revision+`"`)
		writeJSON(w, 200, out)
	})
	r.Patch("/api/v1/tenants/{t}/collections/{id}", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		expected, err := revision(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request collectionservice.Patch
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := collections.Update(r.Context(), who, tid, id, expected, request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("ETag", `"`+out.Revision+`"`)
		writeJSON(w, 200, out)
	})
	r.Delete("/api/v1/tenants/{t}/collections/{id}", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		expected, err := revision(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request collectionservice.DeleteRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := collections.Archive(r.Context(), who, tid, id, expected, request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, 202, out)
	})
	r.Get("/api/v1/tenants/{t}/collections/{id}/items", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		n, err := limit(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := collections.ListItems(r.Context(), who, tid, id, n, r.URL.Query().Get("cursor"), r.URL.Query().Get("section_id"), r.URL.Query().Get("flag"), r.URL.Query().Get("tag"), r.URL.Query().Get("rating"),collectionservice.ItemGalleryFilter{Search:r.URL.Query().Get("search"),Review:r.URL.Query().Get("review"),Visibility:r.URL.Query().Get("visibility")})
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("ETag", `"`+out.Revision+`"`)
		writeJSON(w, 200, out)
	})
	r.Post("/api/v1/tenants/{t}/collections/{id}/items", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request collectionservice.ItemAdd
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := collections.AddItem(r.Context(), who, tid, id, r.Header.Get("Idempotency-Key"), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(out.Status)
		_, _ = w.Write(out.Body)
	})
	r.Patch("/api/v1/tenants/{t}/collections/{id}/items/{item}", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		itemID, err := routeID(r, "item")
		if err != nil {
			writeError(w, r, err)
			return
		}
		expected, err := revision(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request collectionservice.ItemPatch
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := collections.UpdateItem(r.Context(), who, tid, id, itemID, expected, request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, 200, out)
	})
	r.Put("/api/v1/tenants/{t}/collections/{id}/items/{item}/version", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		itemID, err := routeID(r, "item")
		if err != nil {
			writeError(w, r, err)
			return
		}
		expected, err := revision(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request collectionservice.ItemVersionReplace
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		if request.ExpectedRevision != strconv.FormatInt(expected, 10) {
			writeError(w, r, domain.NewError("REVISION_CONFLICT", 409))
			return
		}
		out, err := collections.ReplaceItemVersion(r.Context(), who, tid, id, itemID, request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, 200, out)
	})
	r.Get("/api/v1/tenants/{t}/collections/{id}/items/{item}/version", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		itemID, err := routeID(r, "item")
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := collections.ItemVersionState(r.Context(), who, tid, id, itemID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, 200, out)
	})
	r.Post("/api/v1/tenants/{t}/collections/{id}/items/{item}/moderation", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		itemID, err := routeID(r, "item")
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request collectionservice.ItemModeration
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := collections.ModerateApplyItem(r.Context(), who, tid, id, itemID, request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
	r.Delete("/api/v1/tenants/{t}/collections/{id}/items/{item}", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		itemID, err := routeID(r, "item")
		if err != nil {
			writeError(w, r, err)
			return
		}
		expected, err := revision(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if err = collections.RemoveItem(r.Context(), who, tid, id, itemID, expected); err != nil {
			writeError(w, r, err)
			return
		}
		w.WriteHeader(204)
	})
	r.Get("/api/v1/tenants/{t}/collections/{id}/guest-uploads", func(w http.ResponseWriter, r *http.Request) {
        who,tid,err:=s.tenantRequest(r,false);if err!=nil{writeError(w,r,err);return}
        id,err:=routeID(r,"id");if err!=nil{writeError(w,r,err);return}
        n,err:=limit(r);if err!=nil{writeError(w,r,err);return}
        out,err:=collections.ListGuestUploads(r.Context(),who,tid,id,n,r.URL.Query().Get("cursor"));if err!=nil{writeError(w,r,err);return}
        if r.Context().Err()!=nil{return}
        writeJSON(w,200,out)
    })
	r.Get("/api/v1/tenants/{t}/collections/{id}/sections", func(w http.ResponseWriter, r *http.Request) {
        who,tid,err:=s.tenantRequest(r,false);if err!=nil{writeError(w,r,err);return}
        id,err:=routeID(r,"id");if err!=nil{writeError(w,r,err);return}
        n,err:=limit(r);if err!=nil{writeError(w,r,err);return}
        out,err:=collections.ListSections(r.Context(),who,tid,id,n,r.URL.Query().Get("cursor"));if err!=nil{writeError(w,r,err);return}
        w.Header().Set("ETag",`"`+out.Revision+`"`);writeJSON(w,200,out)
    })
    r.Post("/api/v1/tenants/{t}/collections/{id}/sections/abandon",func(w http.ResponseWriter,r *http.Request){
        who,tid,err:=s.tenantRequest(r,true);if err!=nil{writeError(w,r,err);return}
        id,err:=routeID(r,"id");if err!=nil{writeError(w,r,err);return}
        var request collectionservice.SectionCreate;if err=decodeJSON(r,&request);err!=nil{writeError(w,r,err);return}
        out,err:=collections.AbandonSectionCreation(r.Context(),who,tid,id,r.Header.Get("Idempotency-Key"),request);if err!=nil{writeError(w,r,err);return}
        w.Header().Set("Content-Type","application/json");w.WriteHeader(out.Status);_,_=w.Write(out.Body)
    })
	r.Post("/api/v1/tenants/{t}/collections/{id}/sections", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request collectionservice.SectionCreate
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := collections.CreateSection(r.Context(), who, tid, id, r.Header.Get("Idempotency-Key"), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(out.Status)
		_, _ = w.Write(out.Body)
	})
	r.Patch("/api/v1/tenants/{t}/collections/{id}/sections/{section}", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		sectionID, err := routeID(r, "section")
		if err != nil {
			writeError(w, r, err)
			return
		}
		expected, err := revision(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request collectionservice.SectionPatch
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := collections.UpdateSection(r.Context(), who, tid, id, sectionID, expected, request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, 200, out)
	})
	r.Delete("/api/v1/tenants/{t}/collections/{id}/sections/{section}", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		sectionID, err := routeID(r, "section")
		if err != nil {
			writeError(w, r, err)
			return
		}
		expected, err := revision(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if err = collections.DeleteSection(r.Context(), who, tid, id, sectionID, expected); err != nil {
			writeError(w, r, err)
			return
		}
		w.WriteHeader(204)
	})
	r.Post("/api/v1/tenants/{t}/collections/{id}/publication", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request collectionservice.PublicationRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := collections.Publish(r.Context(), who, tid, id, request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("ETag", `"`+out.Revision+`"`)
		writeJSON(w, 200, out)
	})
	s.shareUsageRoutes(r, collections)
	s.collectionCreationAbandonRoutes(r, collections)
 s.collectionCullingRoutes(r)
	r.Get("/api/v1/tenants/{t}/collections/{id}/shares", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		n, err := limit(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := collections.ListShares(r.Context(), who, tid, id, n, r.URL.Query().Get("cursor"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, 200, out)
	})
	r.Post("/api/v1/tenants/{t}/collections/{id}/shares", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request collectionservice.ShareCreate
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := collections.CreateShare(r.Context(), who, tid, id, r.Header.Get("Idempotency-Key"), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, 201, out)
	})
	r.Patch("/api/v1/tenants/{t}/collections/{id}/shares/{share}", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		shareID, err := routeID(r, "share")
		if err != nil {
			writeError(w, r, err)
			return
		}
		expected, err := revision(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request collectionservice.SharePatch
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := collections.UpdateShare(r.Context(), who, tid, id, shareID, expected, request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("ETag", `"`+out.PolicyVersion+`"`)
		writeJSON(w, 200, out)
	})
	r.Post("/api/v1/tenants/{t}/collections/{id}/shares/{share}/revoke", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		shareID, err := routeID(r, "share")
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := collections.RevokeShare(r.Context(), who, tid, id, shareID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, 200, out)
	})
	r.Post("/api/v1/tenants/{t}/collections/{id}/selection-snapshots", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request collectionservice.SelectionRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := collections.CreateSelection(r.Context(), who, tid, id, r.Header.Get("Idempotency-Key"), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(out.Status)
		_, _ = w.Write(out.Body)
	})
	r.Post("/api/v1/tenants/{t}/collections/{id}/bulk-operations", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request collectionservice.BulkRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := collections.StartBulk(r.Context(), who, tid, id, r.Header.Get("Idempotency-Key"), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(out.Status)
		_, _ = w.Write(out.Body)
	})
    r.Post("/api/v1/tenants/{t}/collections/{id}/bulk-operations/abandon", func(w http.ResponseWriter, r *http.Request) {
        who, tid, err := s.tenantRequest(r, true)
        if err != nil { writeError(w,r,err); return }
        id, err := routeID(r,"id")
        if err != nil { writeError(w,r,err); return }
        var request collectionservice.BulkAbandonRequest
        if err = decodeJSON(r,&request); err != nil { writeError(w,r,err); return }
        out, err := collections.AbandonBulk(r.Context(),who,tid,id,r.Header.Get("Idempotency-Key"),request)
        if err != nil { writeError(w,r,err); return }
        w.Header().Set("Content-Type","application/json"); w.WriteHeader(out.Status); _, _ = w.Write(out.Body)
    })
	r.Get("/api/v1/tenants/{t}/bulk-operations/{id}", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		n, err := limit(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := collections.GetBulk(r.Context(), who, tid, id, n, r.URL.Query().Get("cursor"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, 200, out)
	})
	r.Post("/api/v1/tenants/{t}/bulk-operations/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := collections.CancelBulk(r.Context(), who, tid, id)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, 200, out)
	})
	r.Get("/api/v1/tenants/{t}/assets", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		n, err := limit(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		state, err := assetUploadStateQuery(r.URL.Query())
		if err != nil { writeError(w, r, err); return }
		out, err := assets.List(r.Context(), who, tid, n, r.URL.Query().Get("cursor"), r.URL.Query().Get("project_id"), state, r.URL.Query().Get("search"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
	r.Get("/api/v1/tenants/{t}/assets/{id}", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := assets.Get(r.Context(), who, tid, id)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("ETag", `"`+out.Revision+`"`)
		writeJSON(w, http.StatusOK, out)
	})
	r.Delete("/api/v1/tenants/{t}/assets/{id}", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		rev, err := revision(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var req assetservice.DeleteRequest
		if err = decodeJSON(r, &req); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := assets.Delete(r.Context(), who, tid, id, rev, req)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusAccepted, out)
	})
	r.Post("/api/v1/tenants/{t}/assets/{id}/media-links", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if s.Media == nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		assetID, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request media.Request
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := s.Media.Sign(r.Context(), who, tid, assetID, request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, 200, out)
	})
	r.Get("/api/v1/me", func(w http.ResponseWriter, r *http.Request) {
		who, err := s.authenticate(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := s.Auth.GetUser(r.Context(), who.UserID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("ETag", `"`+out.Revision+`"`)
		writeJSON(w, 200, out)
	})
	r.Patch("/api/v1/me", func(w http.ResponseWriter, r *http.Request) {
		who, err := s.mutation(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		rev, err := revision(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var req auth.Preferences
		if err = decodeJSON(r, &req); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := s.Auth.UpdateUser(r.Context(), who.UserID, rev, req)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("ETag", `"`+out.Revision+`"`)
		writeJSON(w, 200, out)
	})
	r.Get("/api/v1/me/sessions", func(w http.ResponseWriter, r *http.Request) {
		who, err := s.authenticate(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		pageSize, err := limit(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		scope := pagination.Cursor{Principal: who.UserID.String(), Sort: "session:id"}
		var after *uuid.UUID
		if raw := r.URL.Query().Get("cursor"); raw != "" {
			cursor, decodeErr := tenants.Cursors.Decode(raw, scope)
			if decodeErr != nil {
				writeError(w, r, domain.NewError("INVALID_CURSOR", 400))
				return
			}
			id, parseErr := uuid.Parse(cursor.LastID)
			if parseErr != nil {
				writeError(w, r, domain.NewError("INVALID_CURSOR", 400))
				return
			}
			after = &id
		}
		items, more, err := s.Auth.ListSessions(r.Context(), who.UserID, who.SessionID, after, pageSize)
		if err != nil {
			writeError(w, r, err)
			return
		}
		out := pagination.Page[auth.SessionView]{Items: items}
		if more {
			scope.LastID = items[len(items)-1].ID.String()
			next, encodeErr := tenants.Cursors.Encode(scope)
			if encodeErr != nil {
				writeError(w, r, encodeErr)
				return
			}
			out.NextCursor = &next
		}
		writeJSON(w, 200, out)
	})
	r.Delete("/api/v1/me/sessions/{session}", func(w http.ResponseWriter, r *http.Request) {
		who, err := s.mutation(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "session")
		if err != nil {
			writeError(w, r, err)
			return
		}
		if err = s.Auth.RevokeSession(r.Context(), who.UserID, id); err != nil {
			writeError(w, r, err)
			return
		}
		w.WriteHeader(204)
	})
	r.Get("/api/v1/tenants", func(w http.ResponseWriter, r *http.Request) {
		who, err := s.authenticate(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		n, err := limit(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := tenants.List(r.Context(), who, n, r.URL.Query().Get("cursor"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, 200, out)
	})
	r.Post("/api/v1/tenants", func(w http.ResponseWriter, r *http.Request) {
		who, err := s.mutation(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request tenant.CreateRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := tenants.Create(r.Context(), who, r.Header.Get("Idempotency-Key"), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("ETag", `"`+out.Revision+`"`)
		writeJSON(w, http.StatusCreated, out)
	})
	r.Get("/api/v1/tenants/{t}", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := tenants.Get(r.Context(), who, tid)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("ETag", `"`+out.Revision+`"`)
		writeJSON(w, 200, out)
	})
	r.Patch("/api/v1/tenants/{t}", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		rev, err := revision(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request tenant.PatchRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := tenants.Update(r.Context(), who, tid, rev, request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("ETag", `"`+out.Revision+`"`)
		writeJSON(w, http.StatusOK, out)
	})
	r.Delete("/api/v1/tenants/{t}", func(w http.ResponseWriter, r *http.Request) {
		who, err := s.mutation(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		tid, err := routeID(r, "t")
		if err != nil {
			writeError(w, r, err)
			return
		}
		rev, err := revision(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := tenants.Delete(r.Context(), who, tid, rev)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusAccepted, out)
	})
	r.Post("/api/v1/tenants/{t}/invites", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request tenant.InviteRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := tenants.Invite(r.Context(), who, tid, r.Header.Get("Idempotency-Key"), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, out)
	})
	r.Post("/api/v1/invites/accept", func(w http.ResponseWriter, r *http.Request) {
		who, err := s.mutation(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request tenant.InviteAccept
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := tenants.AcceptInvite(r.Context(), who, request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
	r.Get("/api/v1/tenants/{t}/exports/{id}", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := exports.Get(r.Context(), who, tid, id)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
	r.Post("/api/v1/tenants/{t}/download-jobs", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request downloadservice.Request
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := downloads.Start(r.Context(), who, tid, r.Header.Get("Idempotency-Key"), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusAccepted, out)
	})
	r.Get("/api/v1/tenants/{t}/download-jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := downloads.Get(r.Context(), who, tid, id)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
	r.Post("/api/v1/tenants/{t}/download-jobs/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := downloads.Cancel(r.Context(), who, tid, id)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
	r.Post("/api/v1/tenants/{t}/download-jobs/{id}/download-intent", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request downloadservice.IntentRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := downloads.CreateIntent(r.Context(), who, tid, id, r.Header.Get("Idempotency-Key"), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, out)
	})
	r.Post("/api/v1/tenants/{t}/transfer-owner", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var req struct {
			Target   uuid.UUID `json:"target_user_id"`
			Expected string    `json:"expected_revision"`
		}
		if err = decodeJSON(r, &req); err != nil {
			writeError(w, r, err)
			return
		}
		rev, err := strconv.ParseInt(req.Expected, 10, 64)
		if err != nil || rev < 1 || strconv.FormatInt(rev,10) != req.Expected || req.Target == uuid.Nil {
			writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
			return
		}
		if err = tenants.TransferOwner(r.Context(), who, tid, req.Target, rev); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := tenants.Get(r.Context(), who, tid)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, 200, out)
	})
	r.Get("/api/v1/tenants/{t}/members", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		pageSize, err := limit(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := tenants.ListMembers(r.Context(), who, tid, pageSize, r.URL.Query().Get("cursor"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, 200, out)
	})
	r.Patch("/api/v1/tenants/{t}/members/{user}", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		target, err := routeID(r, "user")
		if err != nil {
			writeError(w, r, err)
			return
		}
		rev, err := revision(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request struct {
			Role   domain.Role `json:"role"`
			Status string      `json:"status"`
		}
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := tenants.UpdateMember(r.Context(), who, tid, target, rev, request.Role, request.Status)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("ETag", `"`+out.Revision+`"`)
		writeJSON(w, 200, out)
	})
	r.Delete("/api/v1/tenants/{t}/members/{user}", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		target, err := routeID(r, "user")
		if err != nil {
			writeError(w, r, err)
			return
		}
		rev, err := revision(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if err = tenants.RemoveMember(r.Context(), who, tid, target, rev); err != nil {
			writeError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	r.Get("/api/v1/tenants/{t}/projects", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		n, err := limit(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := projects.List(r.Context(), who, tid, n, r.URL.Query().Get("cursor"), r.URL.Query().Get("search"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, 200, out)
	})
	r.Post("/api/v1/tenants/{t}/projects", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var req project.Create
		if err = decodeJSON(r, &req); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := projects.Create(r.Context(), who, tid, r.Header.Get("Idempotency-Key"), req)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(out.Status)
		_, _ = w.Write(out.Body)
	})
	r.Get("/api/v1/tenants/{t}/projects/{id}", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := projects.Get(r.Context(), who, tid, id)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("ETag", `"`+out.Revision+`"`)
		writeJSON(w, 200, out)
	})
	r.Patch("/api/v1/tenants/{t}/projects/{id}", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		rev, err := revision(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var req project.Patch
		if err = decodeJSON(r, &req); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := projects.Update(r.Context(), who, tid, id, rev, req)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("ETag", `"`+out.Revision+`"`)
		writeJSON(w, 200, out)
	})
	r.Delete("/api/v1/tenants/{t}/projects/{id}", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		rev, err := revision(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var req project.DeleteRequest
		if err = decodeJSON(r, &req); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := projects.Archive(r.Context(), who, tid, id, rev, req)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusAccepted, out)
	})
	r.Put("/api/v1/tenants/{t}/projects/{id}/acl/{user}", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		user, err := routeID(r, "user")
		if err != nil {
			writeError(w, r, err)
			return
		}
		rev, err := revision(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var req project.GrantRequest
		if err = decodeJSON(r, &req); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := projects.SetGrant(r.Context(), who, tid, id, user, rev, req.Role)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("ETag", `"`+out.Revision+`"`)
		writeJSON(w, http.StatusOK, out)
	})
	r.Get("/api/v1/tenants/{t}/styles", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		n, err := limit(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := styles.List(r.Context(), who, tid, n, r.URL.Query().Get("cursor"), r.URL.Query().Get("project_id"), r.URL.Query().Get("search"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
	r.Post("/api/v1/tenants/{t}/styles", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var req styleservice.Create
		if err = decodeJSON(r, &req); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := styles.Create(r.Context(), who, tid, r.Header.Get("Idempotency-Key"), req)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(out.Status)
		_, _ = w.Write(out.Body)
	})
	r.Get("/api/v1/tenants/{t}/styles/{id}", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := styles.Get(r.Context(), who, tid, id)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("ETag", `"`+out.Revision+`"`)
		writeJSON(w, http.StatusOK, out)
	})
	r.Patch("/api/v1/tenants/{t}/styles/{id}", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		rev, err := revision(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var req styleservice.Patch
		if err = decodeJSON(r, &req); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := styles.Update(r.Context(), who, tid, id, rev, req)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("ETag", `"`+out.Revision+`"`)
		writeJSON(w, http.StatusOK, out)
	})
	r.Get("/api/v1/tenants/{t}/styles/{id}/versions", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		n, err := limit(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := styles.ListVersions(r.Context(), who, tid, id, n, r.URL.Query().Get("cursor"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
	r.Post("/api/v1/tenants/{t}/styles/{id}/versions", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request styleservice.AcceptStyleRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := styles.AcceptCandidate(r.Context(), who, tid, id, r.Header.Get("Idempotency-Key"), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if out.Replayed {
			w.Header().Set("Idempotency-Replayed", "true")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(out.Status)
		_, _ = w.Write(out.Body)
	})
	r.Get("/api/v1/tenants/{t}/styles/{id}/versions/{version}/export", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		versionID, err := routeID(r, "version")
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := styles.Export(r.Context(), who, tid, id, versionID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
	r.Post("/api/v1/tenants/{t}/styles/{id}/training-sets", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		var empty struct{}
		if err = decodeJSON(r, &empty); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := styles.CreateTrainingSet(r.Context(), who, tid, id, r.Header.Get("Idempotency-Key"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(out.Status)
		_, _ = w.Write(out.Body)
	})
	r.Post("/api/v1/tenants/{t}/styles/{id}/analyses", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		var req styleservice.AnalyzeRequest
		if err = decodeJSON(r, &req); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := styles.Analyze(r.Context(), who, tid, id, r.Header.Get("Idempotency-Key"), req)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(out.Status)
		_, _ = w.Write(out.Body)
	})
	r.Get("/api/v1/tenants/{t}/training-sets/{id}", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		n, err := limit(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := styles.GetTrainingSet(r.Context(), who, tid, id, n, r.URL.Query().Get("cursor"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("ETag", `"`+out.Revision+`"`)
		writeJSON(w, http.StatusOK, out)
	})
	r.Post("/api/v1/tenants/{t}/training-sets/{id}/holdout-snapshots", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		var req styleservice.HoldoutSnapshotRequest
		if err = decodeJSON(r, &req); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := styles.CreateHoldoutSnapshot(r.Context(), who, tid, id, r.Header.Get("Idempotency-Key"), req)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(out.Status)
		_, _ = w.Write(out.Body)
	})
	r.Post("/api/v1/tenants/{t}/training-sets/{id}/pairs", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		var req styleservice.PairCreate
		if err = decodeJSON(r, &req); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := styles.AddPair(r.Context(), who, tid, id, r.Header.Get("Idempotency-Key"), req)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(out.Status)
		_, _ = w.Write(out.Body)
	})
	r.Post("/api/v1/tenants/{t}/collections/{id}/feedback-pairs", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		collectionID, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		var req styleservice.FeedbackRequest
		if err = decodeJSON(r, &req); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := styles.AddFeedback(r.Context(), who, tid, collectionID, r.Header.Get("Idempotency-Key"), req)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(out.Status)
		_, _ = w.Write(out.Body)
	})
	r.Patch("/api/v1/tenants/{t}/training-sets/{id}/pairs/{pair}", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		pair, err := routeID(r, "pair")
		if err != nil {
			writeError(w, r, err)
			return
		}
		rev, err := revision(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var req styleservice.PairPatch
		if err = decodeJSON(r, &req); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := styles.UpdatePair(r.Context(), who, tid, id, pair, rev, req)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("ETag", `"`+out.Revision+`"`)
		writeJSON(w, http.StatusOK, out)
	})
	r.Post("/api/v1/tenants/{t}/collections/{id}/apply-profile", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		var req runservice.ApplyRequest
		if err = decodeJSON(r, &req); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := runs.Apply(r.Context(), who, tid, id, r.Header.Get("Idempotency-Key"), req)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(out.Status)
		_, _ = w.Write(out.Body)
	})
	r.Post("/api/v1/tenants/{t}/runs", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var req runservice.CreateRequest
		if err = decodeJSON(r, &req); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := runs.Create(r.Context(), who, tid, r.Header.Get("Idempotency-Key"), req)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(out.Status)
		_, _ = w.Write(out.Body)
	})
	r.Get("/api/v1/tenants/{t}/runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := runs.Get(r.Context(), who, tid, id)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("ETag", `"`+out.Revision+`"`)
		writeJSON(w, 200, out)
	})
	r.Get("/api/v1/tenants/{t}/runs/{id}/outputs", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := runs.Outputs(r.Context(), who, tid, id)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, 200, map[string]any{"items": out})
	})
	r.Get("/api/v1/media/run-output", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		query := r.URL.Query()
		if len(query) != 2 || len(query["token"]) != 1 || len(query["versionId"]) != 1 ||
			query.Get("token") == "" || query.Get("versionId") == "" || r.Header.Get("Range") != "" {
			writeError(w, r, domain.NewError("NOT_FOUND", 404))
			return
		}
		now := time.Now()
		grant, err := runservice.OpenOutputGrant(runs.OutputKey, query.Get("token"), now)
		if err != nil || grant.ObjectVersion != query.Get("versionId") {
			writeError(w, r, domain.NewError("NOT_FOUND", 404))
			return
		}
		if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(20 * time.Minute)); err != nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Minute)
		defer cancel()
		verified, err := runs.ReadRevocableOutput(ctx, runs.OutputKey, query.Get("token"), now)
		if err != nil {
			writeError(w, r, err)
			return
		}
		defer verified.Close()
		if err := ctx.Err(); err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", verified.Output.MediaType)
		w.Header().Set("Content-Length", strconv.FormatInt(verified.Output.Bytes, 10))
		w.Header().Set("Content-Disposition", `inline; filename="`+verified.Output.ArtifactID.String()+`"`)
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, verified.File)
	})
	r.Get("/api/v1/tenants/{t}/runs", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		pageSize, err := limit(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := runs.List(r.Context(), who, tid, pageSize, r.URL.Query().Get("cursor"), r.URL.Query().Get("status"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, 200, out)
	})
	r.Post("/api/v1/tenants/{t}/runs/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := runs.Cancel(r.Context(), who, tid, id, r.Header.Get("Idempotency-Key"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("ETag", `"`+out.Revision+`"`)
		writeJSON(w, 200, out)
	})
	r.Post("/api/v1/tenants/{t}/runs/{id}/retry", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request runservice.RetryRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := runs.Retry(r.Context(), who, tid, id, r.Header.Get("Idempotency-Key"), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("ETag", `"`+out.Revision+`"`)
		writeJSON(w, http.StatusOK, out)
	})
	r.Get("/api/v1/tenants/{t}/projects/{id}/raw-capabilities", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if s.Uploads == nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		projectID, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil || projectID == uuid.Nil {
			writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
			return
		}
		serveRAWCapabilities(w, r, who, tid, projectID, s.Uploads)
	})
	r.Post("/api/v1/tenants/{t}/uploads", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if s.Uploads == nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		var request upload.BeginRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := s.Uploads.Begin(r.Context(), who, tid, r.Header.Get("Idempotency-Key"), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("ETag", `"`+out.Revision+`"`)
		writeJSON(w, 201, out)
	})
	r.Get("/api/v1/tenants/{t}/uploads/{id}", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if s.Uploads == nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := s.Uploads.Get(r.Context(), who, tid, id)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("ETag", `"`+out.Revision+`"`)
		writeJSON(w, 200, out)
	})
	r.Post("/api/v1/tenants/{t}/uploads/{id}/parts", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if s.Uploads == nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request upload.PartRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := s.Uploads.SignParts(r.Context(), who, tid, id, request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, 200, out)
	})
	r.Post("/api/v1/tenants/{t}/uploads/{id}/complete", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if s.Uploads == nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request upload.CompleteRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := s.Uploads.Complete(r.Context(), who, tid, id, r.Header.Get("Idempotency-Key"), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, 202, out)
	})
	r.Post("/api/v1/tenants/{t}/uploads/{id}/abort", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if s.Uploads == nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := s.Uploads.Abort(r.Context(), who, tid, id)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("ETag", `"`+out.Revision+`"`)
		writeJSON(w, 200, out)
	})
	r.Post("/api/v1/tenants/{t}/uploads/{id}/moderation", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if s.Uploads == nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request upload.ModerationRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := s.Uploads.Moderate(r.Context(), who, tid, id, request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, 200, out)
	})
	r.Get("/api/v1/tenants/{t}/events", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		n, err := limit(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if r.Header.Get("Accept") == "text/event-stream" {
			s.streamEvents(w, r, events, who, tid, n)
			return
		}
		out, err := events.List(r.Context(), who, tid, r.URL.Query().Get("after"), n)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
	r.Post("/api/v1/tenants/{t}/overrides", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request editor.OverrideCreate
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := edits.CreateOverride(r.Context(), who, tid, r.Header.Get("Idempotency-Key"), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if out.Replayed {
			w.Header().Set("Idempotency-Replayed", "true")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(out.Status)
		_, _ = w.Write(out.Body)
	})
	r.Get("/api/v1/tenants/{t}/assets/{id}/editor-draft", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := edits.GetDraft(r.Context(), who, tid, id)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("ETag", `"`+out.Revision+`"`)
		writeJSON(w, 200, out)
	})
	r.Put("/api/v1/tenants/{t}/assets/{id}/editor-draft", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		expected, err := revisionAllowZero(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request editor.DraftSave
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := edits.SaveDraft(r.Context(), who, tid, id, expected, request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("ETag", `"`+out.Revision+`"`)
		writeJSON(w, 200, out)
	})
	r.Get("/api/v1/tenants/{t}/export-presets", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		n, err := limit(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := presets.List(r.Context(), who, tid, n, r.URL.Query().Get("cursor"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, 200, out)
	})
	r.Post("/api/v1/tenants/{t}/export-presets", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request exportpreset.Create
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := presets.Create(r.Context(), who, tid, r.Header.Get("Idempotency-Key"), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if out.Replayed {
			w.Header().Set("Idempotency-Replayed", "true")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(out.Status)
		_, _ = w.Write(out.Body)
	})
	r.Patch("/api/v1/tenants/{t}/export-presets/{preset}/metadata-policy", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := uuid.Parse(chi.URLParam(r, "preset"))
		if err != nil {
			writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
			return
		}
		var request exportpreset.MetadataPolicyChange
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := presets.ChangeMetadataPolicy(r.Context(), who, tid, id, r.Header.Get("Idempotency-Key"), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if out.Replayed {
			w.Header().Set("Idempotency-Replayed", "true")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(out.Status)
		_, _ = w.Write(out.Body)
	})
	r.Get("/api/v1/tenants/{t}/capabilities", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := capabilities.Get(r.Context(), who, tid)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if err = r.Context().Err(); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
}
func (s *Server) tenantRequest(r *http.Request, mutate bool) (auth.Identity, uuid.UUID, error) {
	var who auth.Identity
	var err error
	if mutate {
		who, err = s.mutation(r)
	} else {
		who, err = s.authenticate(r)
	}
	if err != nil {
		return who, uuid.Nil, err
	}
	tid, err := routeID(r, "t")
	return who, tid, err
}
