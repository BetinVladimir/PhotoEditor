package semanticproposal

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Project-scoped durable original person/face/defect advancement.
// Automatic completion requires sealed parent handoff; manual completion retains
// its separately admitted child/recovery contract. No public receipt is returned.
type SQLNativeInitialPipelineRecovery struct {
	Preparer            TransferDraftPreparer
	CatalogForReference NativeParentArtifactCatalogFactory
	Timeout             time.Duration
}

func (j SQLNativeInitialPipelineRecovery) validate(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if j.Preparer.Scheduler == nil || j.Preparer.Scheduler.Config().MaxConns < 2 || j.CatalogForReference == nil || j.Preparer.Reader.DB == nil || j.Preparer.Reader.Objects == nil || j.Preparer.Reader.ProjectID == uuid.Nil || j.Preparer.Documents.DB == nil || j.Preparer.Documents.Objects == nil || j.Preparer.Documents.Bucket != j.Preparer.Reader.Bucket || j.Preparer.TransportDocuments == nil || j.Timeout <= 0 || j.Timeout > 29*time.Minute {
		return v2input.ErrInvalid
	}
	_, err := initialNativeFaceBudget(j.Preparer.Configuration, j.Preparer.ReservedBytes, v2qualification.Metrics{})
	return err
}
func (j SQLNativeInitialPipelineRecovery) Recover(ctx context.Context, who auth.Identity, tenantID, parent uuid.UUID) (nativeFacePipelineAdvance, bool, error) {
	var out nativeFacePipelineAdvance
	if err := j.validate(ctx); err != nil {
		return out, false, err
	}
	work, cancel := context.WithTimeout(ctx, j.Timeout)
	defer cancel()
	locked, err := withNativeManualParentLock(work, j.Preparer.Scheduler, tenantID, parent, func(work context.Context) error {
		terminal, err := j.Preparer.Reader.readNativeInitialTerminalState(work, who, tenantID, parent)
		if err != nil {
			return err
		}
		if terminal != "" {
			out = nativeFacePipelineAdvance{phase: nativePreparationFailed, operation: terminal}
			return nil
		}
		capacity := ownTransferPreparation(j.Preparer.Configuration)
		anchor, err := j.Preparer.Reader.loadNativePreparationAnchor(work, who, tenantID, parent, capacity.Color, capacity.MetadataBytes)
		if err != nil {
			return err
		}
		catalog, err := resolveNativeParentRecoveryCatalog(work, who, anchor, j.Preparer.Reader.ProjectID, nil, j.CatalogForReference)
		if err != nil {
			return err
		}
		// Saved all-native graphs retain exact original-person locators. Route
		// before initial unique-candidate discovery, which remains mandatory for
		// new graphs and must reject ambiguous shared-source producers.
		allNative, err := j.Preparer.Reader.hasNativeAllTargetSavedParent(work, who, anchor.Request)
		if err != nil {
			return err
		}
		if allNative {
			ticket, err := j.Preparer.loadAndResumeNativeAllTargetTransferLocked(work, who, anchor.Request, anchor.Key, capacity.Color)
			if err != nil {
				return err
			}
			if ticket.RunID != parent || ticket.WorkflowID != anchor.Request.WorkflowID || ticket.State != "queued" {
				return v2input.ErrConflict
			}
			producers, err := j.Preparer.Reader.restoreNativeInputFaceProducers(work, catalog, who, anchor.Request, anchor.Key, anchor.Published, anchor.Snapshot, anchor.SnapshotBody, capacity.Color)
			if err != nil {
				return err
			}
			out = nativeFacePipelineAdvance{phase: nativePreparationSucceeded, producers: producers}
			return nil
		}
		checkpoint, checkpointSHA, present, err := j.Preparer.Reader.readNativeTargetCheckpoint(work, who, anchor.Request)
		if err != nil {
			return err
		}
		if present {
			out, err = j.Preparer.recoverNativeTargetCheckpoint(work, who, anchor, catalog, checkpoint, checkpointSHA, j.CatalogForReference)
			return err
		}
		detectorRun, _, err := detectorPreparationIDs(parent)
		if err != nil {
			return err
		}
		// One bounded attempt owns the meter; nested physical rechecks share it.
		reader := j.Preparer.Reader.withManualRasterScope()
		bootstrap := nativePreparationBootstrap{Reader: reader, Scheduler: j.Preparer.Scheduler, Catalog: catalog, Documents: j.Preparer.Documents, Transport: v2input.ScopedPublisher{Documents: j.Preparer.TransportDocuments, Tenant: tenantID, Run: detectorRun}}
		if err := bootstrap.ensureInitialPersonPreparation(work, who, anchor, capacity, j.Preparer.ReservedBytes, j.Preparer.TransportDocuments); err != nil {
			return err
		}
		out, err = bootstrap.advanceQualifiedInitialFacePipeline(work, who, anchor, capacity, j.Preparer.ReservedBytes, j.Preparer.TransportDocuments)
		if err != nil || out.phase != nativePreparationSucceeded {
			return err
		}
		// Face readiness must not retire the scheduling checkpoint while its
		// original person child is still pending. This observes state only;
		// successful person bytes still require independent result authority.
		personState, err := reader.readNativeInputSavedStageState(work, who, anchor.Request, anchor.Published.Ref, anchor.Snapshot, "mask.person")
		if err != nil {
			return err
		}
		personPending, err := nativeInitialPersonPending(personState)
		if err != nil {
			return err
		}
		if personPending != nil {
			out = *personPending
			return nil
		}
		personPlan, err := reader.restorePersonDraftPlan(work, catalog, who, anchor)
		if err != nil {
			return err
		}
		personInput, err := reader.readOriginalNativePersonInput(work, who, personPlan)
		if err != nil {
			return err
		}
		presence, err := reader.readNativeManualSelectionPresence(work, who, out.producers.detector)
		if err != nil {
			return err
		}
		switch presence {
		case nativeStageAbsent:
			advanced, err := bootstrap.advanceInitialPersonDefect(work, who, out.producers, personInput, capacity, j.Preparer.ReservedBytes, j.Preparer.TransportDocuments)
			if err != nil {
				return err
			}
			if advanced.phase == nativePreparationSucceeded {
				preparer := j.Preparer
				preparer.Reader = reader
				ticket, err := preparer.admitInitialPersonDefectParent(work, who, advanced)
				if err != nil {
					var required *nativeTargetPreparationRequired
					if !errors.As(err, &required) {
						return err
					}
					saved, err := preparer.checkpointNativeTargetPreparation(work, who, out.producers.detector, required.manifest)
					if err != nil {
						return err
					}
					completedProducers := out.producers
					detector := out.producers.detector
					out, err = preparer.advanceNativeTargetPersons(work, who, detector, saved, j.CatalogForReference)
					if err == nil && out.phase == nativePreparationWaiting && out.operation == "semantic.transfer" {
						out, err = preparer.advanceNativeTargetDetectors(work, who, detector, saved, j.CatalogForReference)
					}
					if err == nil && out.phase == nativePreparationWaiting && out.operation == "semantic.transfer" {
						out, err = preparer.advanceNativeTargetFaces(work, who, detector, saved, j.CatalogForReference)
					}
					if err == nil && out.phase == nativePreparationWaiting && out.operation == "semantic.transfer" {
						out, err = preparer.advanceNativeTargetSkins(work, who, detector, saved, j.CatalogForReference)
					}
					if err == nil && out.phase == nativePreparationWaiting && out.operation == "semantic.transfer" {
						out, err = preparer.advanceNativeTargetDefects(work, who, detector, saved, j.CatalogForReference)
					}
					if err == nil && out.phase == nativePreparationWaiting && out.operation == "semantic.transfer" {
						ticket, err = preparer.admitPreparedNativeAllTargetParent(work, who, detector, advanced, saved, j.CatalogForReference)
						if err == nil {
							if ticket.RunID != detector.request.RunID || ticket.WorkflowID != detector.request.WorkflowID || ticket.State != "queued" {
								return v2input.ErrConflict
							}
							out = nativeFacePipelineAdvance{phase: nativePreparationSucceeded, producers: completedProducers}
						}
					}
					return err
				}
				original := out.producers.detector.request
				if ticket.RunID != original.RunID || ticket.WorkflowID != original.WorkflowID || ticket.State != "queued" {
					return v2input.ErrConflict
				}
				// Retain the fully verified original face-producer outcome. The
				// commit-aware recorder independently requires original person,
				// defect protection and sealed parent receipt under the live lease.
				return nil
			}
			// A queued defect child cannot complete the original parent handoff.
			out = nativeFacePipelineAdvance{phase: nativePreparationWaiting, operation: "defects.propose"}
			return nil
		case nativeStagePresent:
			// Presence/SHA is only a hint. Even terminal transitions must retain the
			// complete original scope/source/selection codec, not accept damaged JSON.
			if _, err := reader.readNativeManualSelection(work, who, out.producers.detector); err != nil {
				return err
			}
			terminal, err := reader.readNativeManualChildTerminal(work, who, out.producers.detector)
			if err != nil {
				return err
			}
			if terminal {
				out = nativeFacePipelineAdvance{phase: nativePreparationFailed, operation: "defects.propose"}
				return nil
			}
			_, err = bootstrap.ensureCheckpointSelectedManualDefect(work, who, out.producers, capacity, j.Preparer.ReservedBytes, j.Preparer.TransportDocuments)
			return err
		default:
			return v2input.ErrConflict
		}
	})
	if err != nil || !locked {
		return nativeFacePipelineAdvance{}, locked, err
	}
	return out, true, nil
}

// Returns only pending state, never a result or parent acceptance proof.
// Terminal person state retires only through the independently checked SQL
// terminal recorder; it never grants face-chain completion authority.
func nativeInitialPersonPending(state nativeInputSavedStageState) (*nativeFacePipelineAdvance, error) {
	phase, err := nativeInputStagePreparationPhase(state)
	if err != nil {
		return nil, err
	}
	switch phase {
	case nativePreparationWaiting, nativePreparationBlocked:
		return &nativeFacePipelineAdvance{phase: phase, operation: "mask.person"}, nil
	case nativePreparationSucceeded:
		return nil, nil
	case nativePreparationFailed:
		return &nativeFacePipelineAdvance{phase: phase, operation: "mask.person"}, nil
	default:
		return nil, v2input.ErrConflict
	}
}

func nativeInitialPipelineRecoveryOutcome(parent uuid.UUID, out nativeFacePipelineAdvance, attemptErr error) (string, error) {
	if attemptErr != nil {
		return "retry", attemptErr
	}
	empty := reflect.DeepEqual(out.producers, nativeInputRestoredFaceProducers{})
	validOperation := out.operation == "face.detect" || out.operation == "face.landmarks" || out.operation == "face.parse" || out.operation == "mask.face_skin"
	switch out.phase {
	case nativePreparationWaiting, nativePreparationBlocked:
		if !empty || (!validOperation && out.operation != "mask.person" && out.operation != "defects.propose" && out.operation != "semantic.transfer") {
			return "retry", v2input.ErrConflict
		}
		return "waiting", nil
	case nativePreparationFailed:
		if !empty || (!validOperation && out.operation != "defects.propose" && out.operation != "mask.person") {
			return "retry", v2input.ErrConflict
		}
		return "failed", nil
	case nativePreparationSucceeded:
		p := out.producers
		if parent == uuid.Nil || out.operation != "" || p.detector.request.RunID != parent || p.landmarks.detector.seal != p.detector.seal || p.parser.detector.seal != p.detector.seal || p.skin.parser.seal != p.parser.seal || p.landmarks.prepared.operation != "face.landmarks" || p.parser.prepared.operation != "face.parse" {
			return "retry", v2input.ErrConflict
		}
		for _, verify := range []func() error{func() error { return verifyDetectorPlanSeal(context.Background(), p.detector) }, func() error { return verifyNativeFacePlanSeal(context.Background(), p.landmarks) }, func() error { return verifyNativeFacePlanSeal(context.Background(), p.parser) }, func() error { return verifyNativeSkinPlanSeal(context.Background(), p.skin) }} {
			if err := verify(); err != nil {
				return "retry", err
			}
		}
		return "completed", nil
	default:
		return "retry", v2input.ErrConflict
	}
}

func (j SQLNativeInitialPipelineRecovery) ProcessOne(ctx context.Context) (bool, error) {
	if err := j.validate(ctx); err != nil {
		return false, err
	}
	var tenantID, detector, parent, token uuid.UUID
	var who auth.Identity
	leaseSeconds := int((j.Timeout+time.Second-1)/time.Second) + 10
	claim, stop := context.WithTimeout(ctx, 5*time.Second)
	err := j.Preparer.Scheduler.QueryRow(claim, "SELECT tenant_id,detector_run_id,parent_run_id,actor_id,session_id,auth_epoch,lease_token FROM photo.claim_v27_native_initial_pipeline($1,$2)", j.Preparer.Reader.ProjectID, leaseSeconds).Scan(&tenantID, &detector, &parent, &who.UserID, &who.SessionID, &who.Epoch, &token)
	stop()
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if tenantID == uuid.Nil || detector == uuid.Nil || parent == uuid.Nil || token == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return true, v2input.ErrConflict
	}
	out, locked, attemptErr := j.Recover(ctx, who, tenantID, parent)
	if ctx.Err() != nil {
		return true, ctx.Err()
	}
	outcome := "waiting"
	if locked || attemptErr != nil {
		outcome, attemptErr = nativeInitialPipelineRecoveryOutcome(parent, out, attemptErr)
	}
	finish, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var recorded bool
	recordErr := j.Preparer.Scheduler.QueryRow(finish, "SELECT photo.record_v27_native_initial_pipeline_handoff($1,$2,$3,$4,$5)", tenantID, detector, token, outcome, out.producers.detector.policy.body).Scan(&recorded)
	if recordErr != nil {
		return true, errors.Join(attemptErr, recordErr)
	}
	if !recorded {
		return true, errors.Join(attemptErr, v2input.ErrConflict)
	}
	return true, attemptErr
}
