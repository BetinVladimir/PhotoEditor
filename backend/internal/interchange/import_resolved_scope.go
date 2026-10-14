package interchange

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// BoundImportResolvedScope ties generated style documents to scheduler-owned
// old/new source pairs. Physical child bytes remain the recursive resolver's
// responsibility; this authority never treats archive inventory as consent.
type BoundImportResolvedScope struct {
	WorkICCSHA string
	Masters    ImportMasterReader
	Sources    ImportRecipeSourceQualifier
	byOld      map[uuid.UUID]ImportCorrectionBinding
	byTarget   map[uuid.UUID]ImportCorrectionBinding
	archive    map[uuid.UUID]v2input.Entry
}

func NewBoundImportResolvedScope(parsedRaw []byte, bindings []ImportCorrectionBinding, sources ImportRecipeSourceQualifier, masters ImportMasterReader, workICCSHA string) (*BoundImportResolvedScope, error) {
	if sources == nil || masters == nil || !validImportRefForSHA(workICCSHA) || len(bindings) < 1 || len(bindings) > 5000 {
		return nil, v2input.ErrInvalid
	}
	if _, err := validateParsedImportDocument(context.Background(), parsedRaw); err != nil {
		return nil, err
	}
	var parsed importParsedDocument
	if err := decodeImportDocument(parsedRaw, &parsed); err != nil {
		return nil, err
	}
	result := &BoundImportResolvedScope{WorkICCSHA: workICCSHA, Masters: masters, Sources: sources, byOld: map[uuid.UUID]ImportCorrectionBinding{}, byTarget: map[uuid.UUID]ImportCorrectionBinding{}, archive: map[uuid.UUID]v2input.Entry{}}
	for _, item := range parsed.Manifest.Inventory {
		result.archive[item.ID] = v2input.Entry{Ref: v2input.Ref{ArtifactID: item.ID, SHA256: item.SHA}, Contract: item.Contract}
	}
	for _, item := range bindings {
		if item.Entry == uuid.Nil || item.Old.AssetVersionID == uuid.Nil || item.New.AssetVersionID == uuid.Nil || item.Old.AssetVersionID == item.New.AssetVersionID || !validImportRef(item.Old.Source) || !validImportRef(item.New.Source) || result.byOld[item.Old.AssetVersionID].Entry != uuid.Nil || result.byTarget[item.New.AssetVersionID].Entry != uuid.Nil {
			return nil, v2input.ErrInvalid
		}
		if result.archive[item.Old.Source.ArtifactID] != (v2input.Entry{Ref: item.Old.Source, Contract: "WorkingImage"}) {
			return nil, v2input.ErrInvalid
		}
		result.byOld[item.Old.AssetVersionID] = item
		result.byTarget[item.New.AssetVersionID] = item
	}
	return result, nil
}

func (s *BoundImportResolvedScope) QualifyArchivedResolvedScope(ctx context.Context, _ []byte, old v2input.SessionVersionBinding, children []v2input.Entry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || s.Sources == nil {
		return v2input.ErrInvalid
	}
	item, ok := s.byOld[old.AssetVersionID]
	if !ok || item.Old != old {
		return v2input.ErrInvalid
	}
	if err := s.validateChildren(children, old.Source, true); err != nil {
		return err
	}
	return s.qualifySource(ctx, item)
}

func (s *BoundImportResolvedScope) QualifyTargetResolvedScope(ctx context.Context, _ []byte, target v2input.SessionVersionBinding, children []v2input.Entry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || s.Sources == nil || s.Masters == nil {
		return v2input.ErrInvalid
	}
	item, ok := s.byTarget[target.AssetVersionID]
	if !ok || item.New != target {
		return v2input.ErrInvalid
	}
	if err := s.validateChildren(children, target.Source, false); err != nil {
		return err
	}
	if err := s.qualifySource(ctx, item); err != nil {
		return err
	}
	raw, err := s.Masters.ReadImportMaster(ctx, target.Source, 1<<20)
	if err != nil {
		return err
	}
	if len(raw) < 2 || len(raw) > 1<<20 || rawSHA(raw) != target.Source.SHA256 {
		return v2input.ErrInvalid
	}
	var typed review.WorkingImage
	if err := decodeImportDocument(raw, &typed); err != nil {
		return err
	}
	image, err := review.ParseWorkingImage(raw)
	if err != nil {
		return err
	}
	canvas, err := canonical.Hash(image.Canvas)
	if err != nil {
		return err
	}
	if canvas != target.CanvasHash || image.DType != "float32" || image.Color.ProfileID != "work_rec2020_gamma24_v1" || image.Color.Encoding != "gamma24" || image.Color.ICCSHA256 != s.WorkICCSHA {
		return v2input.ErrInvalid
	}
	if err := s.qualifySource(ctx, item); err != nil {
		return err
	}
	return ctx.Err()
}

func (s *BoundImportResolvedScope) validateChildren(children []v2input.Entry, source v2input.Ref, archived bool) error {
	seenImage := false
	for _, child := range children {
		if !validImportRef(child.Ref) {
			return v2input.ErrInvalid
		}
		if child.Contract == "WorkingImage" {
			if seenImage || child.Ref != source {
				return v2input.ErrInvalid
			}
			seenImage = true
		} else if archived && s.archive[child.Ref.ArtifactID] != child {
			return v2input.ErrInvalid
		}
	}
	if !seenImage {
		return v2input.ErrInvalid
	}
	return nil
}

func (s *BoundImportResolvedScope) qualifySource(ctx context.Context, item ImportCorrectionBinding) error {
	return s.Sources.QualifyImportRecipeSource(ctx, item.Entry, item.Old, item.New)
}

var _ ImportResolvedScopeAuthority = (*BoundImportResolvedScope)(nil)
