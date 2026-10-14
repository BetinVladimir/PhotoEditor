package httpapi

import (
	"context"
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
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Disposition", "inline")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(preview)
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
	if entries == nil {
		entries = []interchange.ImportPersonMaskCandidateEntry{}
	}
	writeJSON(w, http.StatusOK, struct {
		WorkflowID uuid.UUID                                    `json:"workflow_id"`
		Entries    []interchange.ImportPersonMaskCandidateEntry `json:"entries"`
	}{workflow, entries})
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
	for _, candidate := range candidates {
		if candidate.Decision.Action != "grant" || candidate.Decision.EntryID != entry || candidate.Decision.TargetVersionID == uuid.Nil || candidate.Decision.OverrideArtifactID == uuid.Nil || candidate.Decision.MaskArtifactID == uuid.Nil {
			writeError(w, r, domain.NewError("INTERCHANGE_MASK_CANDIDATE_INVALID", 503))
			return
		}
	}
	writeJSON(w, http.StatusOK, struct {
		WorkflowID uuid.UUID                               `json:"workflow_id"`
		EntryID    uuid.UUID                               `json:"entry_id"`
		Candidates []interchange.ImportPersonMaskCandidate `json:"candidates"`
	}{workflow, entry, candidates})
}
