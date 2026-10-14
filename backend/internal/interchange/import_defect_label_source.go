package interchange

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// ImportArchivedDefectLabelSource discovers every qualified proposal that
// actually refers to the archived label array. It is scoped to one archive and
// one old WorkingImage; its results are re-read by the physical verifier.
type ImportArchivedDefectLabelSource struct {
	Parsed    []byte
	Reader    ImportArchiveDocumentReader
	Qualifier ImportDefectCandidatesQualifier
	Old       v2input.SessionVersionBinding
}

func (s ImportArchivedDefectLabelSource) ResolveExportDefectLabels(ctx context.Context, entry v2input.Entry) ([]review.VerifiedProposal, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if entry.Contract != "DefectLabelMap" || !validImportRef(entry.Ref) || s.Reader == nil || s.Qualifier == nil || !validImportRef(s.Old.Source) {
		return nil, v2input.ErrInvalid
	}
	if _, err := validateParsedImportDocument(ctx, s.Parsed); err != nil {
		return nil, err
	}
	var parsed importParsedDocument
	if err := decodeImportDocument(s.Parsed, &parsed); err != nil {
		return nil, err
	}
	files := map[string]v2input.Ref{}
	for _, file := range parsed.Files {
		files[file.Path] = file.Content
	}
	var proposals []review.VerifiedProposal
	for _, item := range parsed.Manifest.Inventory {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if item.Contract != "DefectCandidates" {
			continue
		}
		staged := files[item.Path]
		if !validImportRef(staged) || staged.SHA256 != item.SHA {
			return nil, v2input.ErrInvalid
		}
		raw, err := s.Reader.ReadImportArchiveDocument(ctx, staged, 1<<20)
		if err != nil {
			return nil, err
		}
		if len(raw) < 2 || len(raw) > 1<<20 || rawSHA(raw) != item.SHA {
			return nil, v2input.ErrInvalid
		}
		var refs struct {
			Image  v2input.Ref `json:"image"`
			Labels v2input.Ref `json:"label_map"`
		}
		if err := json.Unmarshal(raw, &refs); err != nil {
			return nil, err
		}
		if refs.Labels.ArtifactID != entry.Ref.ArtifactID {
			continue
		}
		if refs.Image != s.Old.Source || refs.Labels != entry.Ref {
			return nil, v2input.ErrInvalid
		}
		parent := review.ArtifactRef{ID: item.ID, SHA256: item.SHA}
		verified, err := review.ParseDefectProposalStructure(ctx, raw, parent)
		if err != nil {
			return nil, err
		}
		if err := s.Qualifier.ValidateArchivedImportDefectCandidates(ctx, append([]byte(nil), raw...), s.Old); err != nil {
			return nil, err
		}
		proposals = append(proposals, verified)
		if len(proposals) > v2input.MaxEntries {
			return nil, v2input.ErrInvalid
		}
	}
	if len(proposals) == 0 {
		return nil, v2input.ErrInvalid
	}
	return proposals, ctx.Err()
}

// ResolveImportDefectLabelParent identifies the exact archive parent used in
// a SQL occurrence certificate, after the same semantic source scan.
func (s ImportArchivedDefectLabelSource) ResolveImportDefectLabelParent(ctx context.Context, entry v2input.Entry) (v2input.Ref, error) {
	proposals, err := s.ResolveExportDefectLabels(ctx, entry)
	if err != nil {
		return v2input.Ref{}, err
	}
	if len(proposals) != 1 {
		return v2input.Ref{}, &ImportRecipeUnsupported{Reason: "defect_label_shared_parent_certificate_required"}
	}
	binding, err := proposals[0].LabelBinding()
	if err != nil || binding.Proposal.ID == uuid.Nil {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	return v2input.Ref{ArtifactID: binding.Proposal.ID, SHA256: binding.Proposal.SHA256}, ctx.Err()
}

var _ ExportDefectLabelSource = ImportArchivedDefectLabelSource{}
