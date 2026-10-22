package httpapi

import (
	"bytes"
	"context"
	"image/png"
	"net/http"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/interchange"
)

type InterchangeMaskCandidateReader interface {
	ReadEntry(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID) ([]interchange.ImportPersonMaskCandidate, error)
	ListEntries(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID) ([]interchange.ImportPersonMaskCandidateEntry, error)
	RenderPreview(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID) ([]byte, error)
}

func serveInterchangeMaskPreview(w http.ResponseWriter, r *http.Request, who auth.Identity, tenant, project, workflow, entry, overrideID, maskID uuid.UUID, reader InterchangeMaskCandidateReader) {
	if reader == nil {
		writeError(w, r, domain.NewError("INTERCHANGE_UNAVAILABLE", 503))
		return
	}
	preview, err := reader.RenderPreview(r.Context(), who, tenant, project, workflow, entry, overrideID, maskID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if len(preview) < 8 || len(preview) > 8<<20 || string(preview[:8]) != "\x89PNG\r\n\x1a\n" {
		writeError(w, r, domain.NewError("INTERCHANGE_MASK_PREVIEW_INVALID", 503))
		return
	}
	config, err := png.DecodeConfig(bytes.NewReader(preview))
	if err != nil || config.Width < 1 || config.Height < 1 || config.Width > 1024 || config.Height > 1024 {
		writeError(w, r, domain.NewError("INTERCHANGE_MASK_PREVIEW_INVALID", 503))
		return
	}
	// Config bounds are checked before decoding, limiting image allocation.
	if _, err := png.Decode(bytes.NewReader(preview)); err != nil {
		writeError(w, r, domain.NewError("INTERCHANGE_MASK_PREVIEW_INVALID", 503))
		return
	}
	if r.Context().Err()!=nil{return}
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Disposition", "inline")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(preview)
}

func validateImportMaskEntries(entries []interchange.ImportPersonMaskCandidateEntry) error {
	if len(entries) > 5000 {
		return domain.NewError("INTERCHANGE_MASK_CANDIDATE_INVALID", 503)
	}
	seen := make(map[uuid.UUID]bool, len(entries))
	for _, entry := range entries {
		if entry.EntryID == uuid.Nil || entry.TargetVersionID == uuid.Nil || entry.AssetID == uuid.Nil || len(entry.ExternalID) < 1 || len(entry.ExternalID) > 240 || seen[entry.EntryID] {
			return domain.NewError("INTERCHANGE_MASK_CANDIDATE_INVALID", 503)
		}
		seen[entry.EntryID] = true
	}
	return nil
}

func serveInterchangeMaskCandidateEntries(w http.ResponseWriter, r *http.Request, who auth.Identity, tenant, project, workflow uuid.UUID, reader InterchangeMaskCandidateReader) {
	if reader == nil {
		writeError(w, r, domain.NewError("INTERCHANGE_UNAVAILABLE", 503))
		return
	}
	entries, err := reader.ListEntries(r.Context(), who, tenant, project, workflow)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := validateImportMaskEntries(entries); err != nil {
		writeError(w, r, err)
		return
	}
	if entries == nil {
		entries = []interchange.ImportPersonMaskCandidateEntry{}
	}
	if r.Context().Err()!=nil{return}
	writeJSON(w, http.StatusOK, struct {
		WorkflowID uuid.UUID                                    `json:"workflow_id"`
		Entries    []interchange.ImportPersonMaskCandidateEntry `json:"entries"`
	}{workflow, entries})
}

func validateImportMaskCandidates(entry uuid.UUID, candidates []interchange.ImportPersonMaskCandidate) error {
	if entry == uuid.Nil || len(candidates) > 5000 {
		return domain.NewError("INTERCHANGE_MASK_CANDIDATE_INVALID", 503)
	}
	seen := make(map[[3]uuid.UUID]bool, len(candidates))
	for _, candidate := range candidates {
		decision := candidate.Decision
		key := [3]uuid.UUID{decision.OverrideArtifactID, decision.MaskArtifactID, decision.TargetVersionID}
		if decision.Action != "grant" || decision.EntryID != entry || decision.TargetVersionID == uuid.Nil || decision.OverrideArtifactID == uuid.Nil || decision.MaskArtifactID == uuid.Nil || len(candidate.ExternalID) < 1 || len(candidate.ExternalID) > 240 || seen[key] {
			return domain.NewError("INTERCHANGE_MASK_CANDIDATE_INVALID", 503)
		}
		seen[key] = true
	}
	return nil
}

func serveInterchangeMaskCandidates(w http.ResponseWriter, r *http.Request, who auth.Identity, tenant, project, workflow, entry uuid.UUID, reader InterchangeMaskCandidateReader) {
	if reader == nil {
		writeError(w, r, domain.NewError("INTERCHANGE_UNAVAILABLE", 503))
		return
	}
	candidates, err := reader.ReadEntry(r.Context(), who, tenant, project, workflow, entry)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if candidates == nil {
		candidates = []interchange.ImportPersonMaskCandidate{}
	}
	if err := validateImportMaskCandidates(entry, candidates); err != nil {
		writeError(w, r, err)
		return
	}

	if r.Context().Err()!=nil{return}
	writeJSON(w, http.StatusOK, struct {
		WorkflowID uuid.UUID                               `json:"workflow_id"`
		EntryID    uuid.UUID                               `json:"entry_id"`
		Candidates []interchange.ImportPersonMaskCandidate `json:"candidates"`
	}{workflow, entry, candidates})
}
