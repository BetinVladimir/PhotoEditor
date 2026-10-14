package v2learning

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type TrainingInventoryReader interface {
	Load(context.Context, TrainingStart) (TrainingInventory, error)
}

type TrainingCaseBuilder interface {
	DiscoverICC(context.Context, TrainingStart, TrainingCandidate) (string, error)
	Assemble(context.Context, TrainingStart, TrainingCandidate, uuid.UUID,
		string, string) (TrainingCaseEvidence, error)
}

type TrainingCaseFactory interface {
	ForCandidate(TrainingStart, TrainingCandidate) (TrainingCaseBuilder, error)
}

// SQLTrainingCaseFactory builds a fresh project/asset scoped catalog for
// every event. A catalog bound to one asset must never be reused for another.
type SQLTrainingCaseFactory struct {
	API       *pgxpool.Pool
	Scheduler *pgxpool.Pool
	Objects   *objectstore.Store
	Bucket    string
}

func (f SQLTrainingCaseFactory) ForCandidate(start TrainingStart,
	candidate TrainingCandidate) (TrainingCaseBuilder, error) {
	if f.API == nil || f.Scheduler == nil || f.Objects == nil ||
		f.Bucket == "" || start.ProjectID == uuid.Nil ||
		candidate.AssetVersionID == uuid.Nil || candidate.EventID == uuid.Nil {
		return nil, ErrInvalidTrainingDraft
	}
	catalog := v2draft.SQLArtifactCatalog{DB: f.API, Bucket: f.Bucket,
		ProjectID:      start.ProjectID,
		AssetVersionID: candidate.AssetVersionID}
	reader := v2draft.Service{DB: f.API, Objects: f.Objects,
		Bucket: f.Bucket}
	return TrainingCaseAssembler{
		Series: SQLTrainingSeriesPreparer{
			DB: f.Scheduler, Catalog: catalog, Objects: f.Objects,
			Series: SQLSeriesObjects{DB: f.Scheduler,
				Objects: f.Objects, Bucket: f.Bucket}},
		PortraitDetail: SQLTrainingPortraitDetailPreparer{
			DB: f.Scheduler, Catalog: catalog, Objects: f.Objects,
			Producer: SQLPortraitDetailObjects{DB: f.Scheduler,
				Objects: f.Objects, Bucket: f.Bucket},
			Masks: SQLPortraitDetailMaskObjects{DB: f.Scheduler,
				Objects: f.Objects, Bucket: f.Bucket}},
		ReviewedDust: SQLTrainingReviewedDustPreparer{
			DB: f.Scheduler, Catalog: catalog, Objects: f.Objects,
			Reviews: SQLReviewedDustObjects{DB: f.Scheduler,
				Objects: f.Objects, Bucket: f.Bucket}},
		ReviewedDefects: SQLTrainingReviewedDefectPreparer{
			DB: f.Scheduler, Catalog: catalog, Objects: f.Objects,
			Reviews: SQLReviewedDefectObjects{DB: f.Scheduler,
				Objects: f.Objects, Bucket: f.Bucket}},
		Documents: SQLTrainingDocumentSource{DB: f.Scheduler,
			Objects: f.Objects, Bucket: f.Bucket,
			ProjectID: start.ProjectID},
		Recipes: TrainingRecipeSource{Catalog: catalog,
			Objects: f.Objects, Learning: reader,
			ProjectID: start.ProjectID},
		Images:      TrainingImageSource{Catalog: catalog, Objects: f.Objects},
		Descriptors: TrainingDescriptorSource{Reader: reader},
		Faces:       TrainingFacesSource{Catalog: catalog, Objects: f.Objects},
		Backgrounds: TrainingBackgroundSource{Catalog: catalog, Objects: f.Objects, Assets: v2draft.SQLBackgroundAssetResolver{DB: f.API, Bucket: f.Bucket}},
		Regions: TrainingRegionSource{Catalog: catalog, Objects: f.Objects, Selector: TrainingMaskSelector{
			Roots:  SQLTrainingMaskRoots{DB: f.Scheduler},
			Reader: ScopedTrainingMaskRootReader{Catalog: catalog, Objects: f.Objects},
			Masks:  TrainingMaskSource{Catalog: catalog, Objects: f.Objects, Provenance: SQLTrainingMaskProvenance{DB: f.Scheduler}}}},
		Protection: TrainingMaskSelector{
			Roots: SQLTrainingMaskRoots{DB: f.Scheduler},
			Reader: ScopedTrainingMaskRootReader{Catalog: catalog,
				Objects: f.Objects},
			Masks: TrainingMaskSource{Catalog: catalog,
				Objects:    f.Objects,
				Provenance: SQLTrainingMaskProvenance{DB: f.Scheduler}}}}, nil
}

// TrainingSourceResolver is the missing-draft path. It does not publish or
// freeze; CompileTrainingDraft performs the final split and the admission
// orchestrator commits the exact resulting manifest.
type TrainingSourceResolver struct {
	Inventory TrainingInventoryReader
	Context   TrainingContextSource
	Cases     TrainingCaseFactory
	Frozen    TrainingFrozenReader
}

func NewSQLTrainingSourceResolver(api, scheduler *pgxpool.Pool,
	objects *objectstore.Store, bucket string) (TrainingSourceResolver, error) {
	if api == nil || scheduler == nil || objects == nil || bucket == "" {
		return TrainingSourceResolver{}, ErrInvalidTrainingDraft
	}
	return TrainingSourceResolver{
		Inventory: SQLTrainingInventorySource{DB: scheduler},
		Context: TrainingContextSource{
			Model: TrainingModelLockSource{DB: scheduler},
			Calibration: TrainingCalibrationSource{DB: scheduler,
				Objects: objects, Bucket: bucket},
			Baseline: TrainingActiveBaselineSource{DB: scheduler,
				Objects: objects, Bucket: bucket}},
		Cases: SQLTrainingCaseFactory{API: api, Scheduler: scheduler,
			Objects: objects, Bucket: bucket},
		Frozen: SQLTrainingFrozenSource{DB: scheduler, Objects: objects,
			Bucket: bucket}}, nil
}

const trainingOutputReservationBytes int64 = 16 * 1048576

func (s TrainingSourceResolver) Resolve(ctx context.Context,
	start TrainingStart) (TrainingSourceSnapshot, error) {
	var empty TrainingSourceSnapshot
	if s.Inventory == nil || s.Cases == nil ||
		start.TenantID == uuid.Nil || start.ActorID == uuid.Nil ||
		start.ProfileID == uuid.Nil || start.ProjectID == uuid.Nil ||
		start.AuthSessionID == uuid.Nil {
		return empty, ErrInvalidTrainingDraft
	}
	if _, err := ValidateTrainCreate(start.Request); err != nil {
		return empty, ErrInvalidTrainingDraft
	}
	runID, err := v2input.RunID(start.TenantID, start.ActorID, start.Key)
	if err != nil {
		return empty, ErrInvalidTrainingDraft
	}
	inventory, err := s.Inventory.Load(ctx, start)
	if err != nil {
		return empty, err
	}
	if inventory.ProjectID != start.ProjectID ||
		inventory.Mode != start.Request.Policy.Mode ||
		len(inventory.Rows) > 5000 {
		return empty, ErrTrainingDraftConflict
	}
	eligible, _ := PreselectTrainingCandidates(inventory.Rows)
	result := TrainingSourceSnapshot{Candidates: inventory.Rows,
		Evidence:      make([]TrainingCaseEvidence, 0, len(eligible)),
		Excluded:      make([]ExcludedTrainingCandidate, 0),
		ReservedBytes: trainingOutputReservationBytes}
	var iccLock string
	for _, candidate := range eligible {
		builder, err := s.Cases.ForCandidate(start, candidate)
		if err != nil {
			return empty, err
		}
		if builder == nil {
			return empty, ErrInvalidTrainingDraft
		}
		foundICC, err := builder.DiscoverICC(ctx, start, candidate)
		if errors.Is(err, ErrInvalidTrainingExample) {
			result.Excluded = append(result.Excluded,
				ExcludedTrainingCandidate{EventID: candidate.EventID,
					Reason: "UNSUPPORTED_ARTIFACT"})
			continue
		}
		if err != nil {
			return empty, err
		}
		if !validSHA(foundICC) {
			return empty, ErrInvalidTrainingDraft
		}
		if iccLock != "" && foundICC != iccLock {
			result.Excluded = append(result.Excluded,
				ExcludedTrainingCandidate{EventID: candidate.EventID,
					Reason: "UNSUPPORTED_ARTIFACT"})
			continue
		}
		if iccLock == "" {
			result.Context, err = s.Context.Load(ctx, start, inventory,
				foundICC)
			if err != nil {
				return empty, err
			}
		}
		item, err := builder.Assemble(ctx, start, candidate, runID,
			foundICC, result.Context.ModelLockSHA)
		if errors.Is(err, ErrInvalidTrainingExample) {
			result.Excluded = append(result.Excluded,
				ExcludedTrainingCandidate{EventID: candidate.EventID,
					Reason: "UNSUPPORTED_ARTIFACT"})
			continue
		}
		if err != nil {
			return empty, err
		}
		iccLock = foundICC
		result.Evidence = append(result.Evidence, item)
	}
	if len(result.Evidence) == 0 {
		return empty, ErrInsufficientTrainingData
	}
	return result, nil
}
