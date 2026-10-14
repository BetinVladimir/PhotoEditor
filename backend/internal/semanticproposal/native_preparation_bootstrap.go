package semanticproposal

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// Server-owned initial native-stage dependencies. These are not selected by user
// intent. Public parent admission and durable orchestration remain separate.
type nativePreparationBootstrap struct {
	Reader    SQLTransferSourceReader
	Scheduler *pgxpool.Pool
	Catalog   v2draft.ArtifactCatalog
	Documents v2draft.Publisher
	Transport v2input.Publisher
}

type nativePreparationAnchor struct {
	Request      Request
	Key          string
	Published    PublishedIntent
	Snapshot     v2input.Ref
	SnapshotBody []byte
	Color        TransferColorProfile
}

func (b nativePreparationBootstrap) validate(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if b.Scheduler == nil || b.Catalog == nil || b.Transport == nil || b.Reader.DB == nil || b.Reader.Objects == nil || b.Reader.Bucket == "" || b.Reader.ProjectID == uuid.Nil || b.Documents.DB == nil || b.Documents.Objects == nil || b.Documents.Bucket != b.Reader.Bucket {
		return v2input.ErrInvalid
	}
	return nil
}

// The returned private plan is a recovery checkpoint even if persistence or
// commit fails. It is not an acceptance receipt or proof that inputs were saved.
// Resume must use the complete recovery/admission boundary; NULL never permits
// draft reconstruction. No successful child ticket is a parent API ticket.
func (b nativePreparationBootstrap) bootstrapDetector(ctx context.Context, who auth.Identity, anchor nativePreparationAnchor, report v2input.Ref, score float64, maxFaces int, budget nativePreparationBudget) (detectorDraftPlan, v2input.JobTicket, error) {
	return b.bootstrapDetectorMode(ctx, who, anchor, report, score, maxFaces, budget, false, nil)
}

// New initial processing opts into atomic detector seal plus durable scheduling.
// The returned detector receipt remains private, not public parent admission.
func (b nativePreparationBootstrap) bootstrapInitialDetector(ctx context.Context, who auth.Identity, anchor nativePreparationAnchor, report v2input.Ref, score float64, maxFaces int, budget nativePreparationBudget) (detectorDraftPlan, v2input.JobTicket, error) {
	return b.bootstrapDetectorMode(ctx, who, anchor, report, score, maxFaces, budget, true, nil)
}

func (b nativePreparationBootstrap) bootstrapDetectorMode(ctx context.Context, who auth.Identity, anchor nativePreparationAnchor, report v2input.Ref, score float64, maxFaces int, budget nativePreparationBudget, initial bool, manual *nativeInitialManualSelection) (detectorDraftPlan, v2input.JobTicket, error) {
	if err := b.validate(ctx); err != nil {
		return detectorDraftPlan{}, v2input.JobTicket{}, err
	}
	anchor.SnapshotBody = append([]byte(nil), anchor.SnapshotBody...)
	return bootstrapNativePreparation(ctx,
		func() (detectorDraftPlan, error) {
			return b.Reader.prepareDetectorDraftPlan(ctx, b.Catalog, b.Documents, who, anchor.Request, anchor.Key, anchor.Published, anchor.Snapshot, anchor.SnapshotBody, anchor.Color, report, score, maxFaces, budget)
		},
		func(plan detectorDraftPlan) error {
			if initial && manual != nil {
				_, err := b.Reader.persistInitialManualDetectorInputs(ctx, b.Scheduler, b.Transport, who, plan, anchor.Color, manual.Pixels, manual.Diameter)
				return err
			}
			_, err := b.Reader.persistDetectorPreparationInputs(ctx, b.Scheduler, b.Transport, who, plan, anchor.Color)
			return err
		},
		func(plan detectorDraftPlan) (v2input.JobTicket, error) {
			if initial {
				if manual != nil {
					return b.Reader.resumeInitialDetectorWithManualSelection(ctx, b.Scheduler, who, plan, manual.Pixels, manual.Diameter)
				}
				return b.Reader.resumeInitialDetectorPreparation(ctx, b.Scheduler, who, plan)
			}
			return b.Reader.resumeDetectorPreparation(ctx, b.Scheduler, who, plan)
		})
}
func (b nativePreparationBootstrap) bootstrapPerson(ctx context.Context, who auth.Identity, anchor nativePreparationAnchor, report v2input.Ref, tile int, budget nativePreparationBudget) (personDraftPlan, v2input.JobTicket, error) {
	if err := b.validate(ctx); err != nil {
		return personDraftPlan{}, v2input.JobTicket{}, err
	}
	anchor.SnapshotBody = append([]byte(nil), anchor.SnapshotBody...)
	return bootstrapNativePreparation(ctx,
		func() (personDraftPlan, error) {
			return b.Reader.preparePersonDraftPlan(ctx, b.Catalog, b.Documents, who, anchor.Request, anchor.Key, anchor.Published, anchor.Snapshot, anchor.SnapshotBody, anchor.Color, report, tile, budget)
		},
		func(plan personDraftPlan) error {
			_, err := b.Reader.persistPersonPreparationInputs(ctx, b.Scheduler, b.Transport, who, plan, anchor.Color)
			return err
		},
		func(plan personDraftPlan) (v2input.JobTicket, error) {
			return b.Reader.resumePersonPreparation(ctx, b.Scheduler, who, plan)
		})
}

// A failed preparation has no checkpoint. After preparation, retain the exact
// owned plan on any failure, but expose a ticket only after admission commits.
func bootstrapNativePreparation[P any](ctx context.Context, prepare func() (P, error), persist func(P) error, admit func(P) (v2input.JobTicket, error)) (P, v2input.JobTicket, error) {
	var zero P
	if err := ctx.Err(); err != nil {
		return zero, v2input.JobTicket{}, err
	}
	if prepare == nil || persist == nil || admit == nil {
		return zero, v2input.JobTicket{}, v2input.ErrInvalid
	}
	plan, err := prepare()
	if err != nil {
		return zero, v2input.JobTicket{}, fmt.Errorf("prepare native stage: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return plan, v2input.JobTicket{}, err
	}
	if err := persist(plan); err != nil {
		return plan, v2input.JobTicket{}, fmt.Errorf("persist native stage: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return plan, v2input.JobTicket{}, err
	}
	ticket, err := admit(plan)
	if err != nil {
		return plan, v2input.JobTicket{}, err
	}
	if err := ctx.Err(); err != nil {
		return plan, v2input.JobTicket{}, err
	}
	return plan, ticket, nil
}

type nativeInitialManualSelection struct {
	Pixels   []v2input.Ref
	Diameter [2]float64
}

func (b nativePreparationBootstrap) bootstrapInitialDetectorWithManualSelection(ctx context.Context, who auth.Identity, anchor nativePreparationAnchor, report v2input.Ref, score float64, maxFaces int, budget nativePreparationBudget, pixels []v2input.Ref, diameter [2]float64) (detectorDraftPlan, v2input.JobTicket, error) {
	if err := b.validate(ctx); err != nil {
		return detectorDraftPlan{}, v2input.JobTicket{}, err
	}
	if err := validateInitialManualPixelSelection(pixels); err != nil {
		return detectorDraftPlan{}, v2input.JobTicket{}, err
	}
	if err := validateInitialManualDiameter(diameter); err != nil {
		return detectorDraftPlan{}, v2input.JobTicket{}, err
	}
	owned := nativeInitialManualSelection{Pixels: append([]v2input.Ref(nil), pixels...), Diameter: diameter}
	return b.bootstrapDetectorMode(ctx, who, anchor, report, score, maxFaces, budget, true, &owned)
}
