package v2render

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// SQLAuthoredFinalPreparation reads actual published phase/Params objects and
// current compose/2 installation. It never stages a compose/1 shell, fabricates
// a future publication ref, or treats a detached compiler result as admission.
type SQLAuthoredFinalPreparation struct {
	API            *pgxpool.Pool
	Params         AuthoredFinalParamsPublication
	Calibrations   AuthoredFinalCalibrationCatalog
	NativeReplays  *SQLAuthoredFinalReplayPreparation
	Generation     *SQLAuthoredFinalGenerationReader
	GenerationRef  *v2input.Ref
	generationRefs map[uuid.UUID]v2input.Ref
}

func (s SQLAuthoredFinalPreparation) Prepare(ctx context.Context, who auth.Identity, tenant, parent uuid.UUID, ordinal int) (AuthoredFinalTaskPlan, error) {
	empty := AuthoredFinalTaskPlan{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	publication := s.Params.Continuation
	if s.API == nil || (s.Calibrations == nil && s.GenerationRef == nil && len(s.generationRefs) == 0) || publication.Scheduler == nil || publication.PhaseObjects.Objects == nil {
		return empty, v2input.ErrInvalid
	}
	inventory, found, err := ReadAuthoredFinalInventory(ctx, publication.Scheduler, tenant, parent, ordinal)
	if err != nil {
		return empty, err
	}
	if !found {
		return empty, v2input.ErrExpired
	}
	scope := inventory.Context
	if scope.Actor != who.UserID || scope.Session != who.SessionID {
		return empty, v2input.ErrInvalid
	}
	generationRef := s.GenerationRef
	if len(s.generationRefs) > 0 {
		ref, ok := s.generationRefs[tenant]
		if !ok {
			return empty, renderUnavailable()
		}
		generationRef = &ref
	}
	var generation VerifiedAuthoredFinalGeneration
	calibrations := s.Calibrations
	if generationRef != nil {
		if s.Generation == nil {
			return empty, renderUnavailable()
		}
		generation, err = s.Generation.Read(ctx, who, scope, *generationRef)
		if err != nil {
			return empty, err
		}
		calibrations = verifiedFinalGenerationCatalog{proof: generation}
	}
	// Both the original recipe and the new target keep their own actual geometry.
	// The former must never be rewritten to make a changed-canvas final pass.
	var base struct {
		Original v2input.SessionVersionBinding `json:"original_source"`
	}
	if json.Unmarshal([]byte(inventory.BaseReceiptJCS), &base) != nil || !authoredBindingValid(base.Original) {
		return empty, v2input.ErrInvalid
	}
	item := RenderSelectionItem{Binding: base.Original, Recipe: scope.OriginalRecipe}
	calibrated, err := LoadCalibratedAuthoredRequest(ctx, s.API, publication.PhaseObjects.Objects, publication.Bucket, calibrations, tenant, scope.Project, who, item)
	if err != nil {
		return empty, err
	}
	paramsRef, published, err := s.Params.Publish(ctx, who, tenant, parent, ordinal)
	if err != nil {
		return empty, err
	}
	evidence, err := ReadAuthoredFinalInputEvidence(ctx, publication.Scheduler, publication.PhaseObjects, who, tenant, parent, ordinal, paramsRef)
	if err != nil {
		return empty, err
	}
	if !bytes.Equal(evidence.Params.JSON, published.JSON) {
		return empty, v2input.ErrInvalid
	}
	target, err := publication.PhaseObjects.ReadJSON(ctx, who, tenant, scope.Project, parent, ordinal, published.Params.Source.Source)
	if err != nil {
		return empty, err
	}
	calibration, err := calibrations.ResolveAuthoredFinal(ctx, calibrated.Qualification)
	if err != nil {
		return empty, err
	}
	calibrationSHA, err := canonical.Hash(calibration)
	if err != nil {
		return empty, err
	}
	if calibrationSHA != calibrated.CalibrationSHA {
		return empty, renderUnavailable()
	}
	profile, err := CompileAuthoredFinalExecutionProfile(calibrated.Request, published, target, calibrated.Qualification, calibration)
	if err != nil {
		return empty, err
	}
	var plan AuthoredFinalTaskPlan
	if generationRef != nil {
		profile, err = CompileAuthoredFinalRoot3ExecutionProfile(ctx, calibrated.Request, published, target, generation)
		if err == nil {
			plan, err = compileAuthoredFinalRoot3Task(ctx, evidence, paramsRef, profile, generation)
		}
	} else {
		plan, err = compileAuthoredFinalTask(evidence, paramsRef, profile)
	}
	if err != nil {
		return empty, err
	}
	// Publication/object IO must not turn a stale authorization into a shell.
	current, err := (SQLAuthoredFinalQualificationResolver{API: s.API}).Resolve(ctx, who, tenant, scope.Project)
	if err != nil {
		return empty, err
	}
	if current != calibrated.Qualification {
		return empty, renderUnavailable()
	}
	calibration, err = calibrations.ResolveAuthoredFinal(ctx, current)
	if err != nil {
		return empty, err
	}
	if err = calibration.validate(current); err != nil {
		return empty, err
	}
	calibrationSHA, err = canonical.Hash(calibration)
	if err != nil {
		return empty, err
	}
	if calibrationSHA != calibrated.CalibrationSHA {
		return empty, renderUnavailable()
	}
	after, present, err := ReadAuthoredFinalInventory(ctx, publication.Scheduler, tenant, parent, ordinal)
	if err != nil {
		return empty, err
	}
	if !present {
		return empty, v2input.ErrExpired
	}
	beforeRaw, err := canonical.Marshal(inventory)
	if err != nil {
		return empty, err
	}
	afterRaw, err := canonical.Marshal(after)
	if err != nil {
		return empty, err
	}
	if !bytes.Equal(beforeRaw, afterRaw) {
		return empty, v2input.ErrExpired
	}
	if err = ctx.Err(); err != nil {
		return empty, err
	}
	if generationRef != nil {
		current, e := s.Generation.Read(ctx, who, scope, *generationRef)
		if e != nil {
			return empty, e
		}
		if current.reference != generation.reference || current.documentJCS != generation.documentJCS || current.reportJCS != generation.reportJCS {
			return empty, v2input.ErrExpired
		}
	}
	return plan, ctx.Err()
}
