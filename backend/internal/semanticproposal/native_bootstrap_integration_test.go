package semanticproposal

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"hash/crc32"
	"os"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Physical object publication/admission, with explicitly synthetic imported
// qualification reports. This test performs no model inference or GPU benchmark.
func TestIntegrationNativePreparationBootstrap(t *testing.T) {
	raw := os.Getenv("PHOTO_SEMANTIC_INTENT_PUBLISH_CASE")
	if raw == "" {
		t.Skip("isolated PostgreSQL/object-store fixture required")
	}
	var f struct {
		Tenant, Actor, Session uuid.UUID
		Intent                 v2input.SemanticEditIntent
		Objects                objectstore.Config
		RequiredProtection     v2input.Ref
		PersonQualification    struct{ Report v2input.Ref }
		ParseQualification     struct{ Report v2input.Ref }
		FaceQualifications     []struct{ Report v2input.Ref }
		SceneQualifications    [2]v2input.SemanticQualificationPin
	}
	if json.Unmarshal([]byte(raw), &f) != nil || len(f.FaceQualifications) != 2 {
		t.Fatal("native fixture")
	}
	timeout := 180 * time.Second
	if os.Getenv("PHOTO_TEST_V27_SCENE_WIRE") == "1" {
		timeout = 6 * time.Minute
	}
	if os.Getenv("PHOTO_TEST_V27_PERSON_DEFECT_PARENT") == "1" {
		timeout = 10 * time.Minute
	}
	if os.Getenv("PHOTO_TEST_V27_ORIGINAL_PERSON_RECOVER") == "1" {
		timeout = 15 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cfg, err := pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal("database configuration")
	}
	var timing *nativeTimingTracer
	if os.Getenv("PHOTO_TEST_NATIVE_TIMING") == "1" {
		timing = newNativeTimingTracer(t)
		cfg.ConnConfig.Tracer = timing
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_api"
	api, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("API connection")
	}
	defer api.Close()
	cfg = cfg.Copy()
	cfg.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	scheduler, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("scheduler connection")
	}
	defer scheduler.Close()
	objects, err := objectstore.New(f.Objects)
	if err != nil {
		t.Fatal("object configuration")
	}
	color, err := LoadTransferColorProfile("../../../resources/icc")
	if err != nil {
		t.Fatal(err)
	}
	who := auth.Identity{UserID: f.Actor, SessionID: f.Session}
	body, err := json.Marshal(f.Intent)
	if err != nil {
		t.Fatal(err)
	}
	key := "native-bootstrap-physical"
	request, err := CompileRequest(f.Tenant, f.Actor, key, body)
	if err != nil {
		t.Fatal(err)
	}
	documents := v2draft.Publisher{DB: api, Objects: objects, Bucket: f.Objects.Bucket}
	published, err := PublishIntent(ctx, documents, who, request)
	if err != nil {
		t.Fatal("native intent publication", err)
	}
	reader := SQLTransferSourceReader{DB: api, Objects: objects, Bucket: f.Objects.Bucket, ProjectID: published.Inventory.Scope.ProjectID}
	snapshot, snapshotBody, err := reader.PublishTargetSnapshot(ctx, documents, who, request, key, published)
	if err != nil {
		t.Fatal("native snapshot publication", err)
	}
	anchor := nativePreparationAnchor{Request: request, Key: key, Published: published, Snapshot: snapshot, SnapshotBody: snapshotBody, Color: color}
	transportDocs := artifactjson.Publisher{Repository: artifactjson.SQLRepository{DB: scheduler, Bucket: f.Objects.Bucket}, Objects: objects}
	catalog := v2draft.SQLArtifactCatalog{DB: api, Bucket: f.Objects.Bucket, ProjectID: reader.ProjectID, AssetVersionID: request.Intent.Reference.AssetVersionID}
	budget := nativePreparationBudget{Costs: v2input.SemanticQualifiedCosts{VRAMBytes: 128 << 20}, RAM: 2 << 30, VRAM: 1 << 30, Scratch: 1 << 30, Output: 8 << 20, Reserved: 64 << 20}
	detectorRun, _, err := detectorPreparationIDs(request.RunID)
	if err != nil {
		t.Fatal(err)
	}
	bootstrap := nativePreparationBootstrap{Reader: reader, Scheduler: scheduler, Catalog: catalog, Documents: documents, Transport: v2input.ScopedPublisher{Documents: transportDocs, Tenant: f.Tenant, Run: detectorRun}}
	initialAnchor, err := bootstrap.prepareInitialNativeAnchor(ctx, who, f.Tenant, key, body, TransferPreparation{Color: color, RAM: budget.RAM, VRAM: budget.VRAM, Scratch: budget.Scratch, Output: budget.Output, MetadataBytes: 16 << 20}, budget.Reserved)
	if err != nil || !reflect.DeepEqual(initialAnchor, anchor) {
		t.Fatal("new initial anchor preserves original published intent and snapshot", err)
	}
	initialPresence, err := reader.readInitialNativeDetectorPresence(ctx, who, request, key)
	if err != nil || initialPresence != nativeStageAbsent {
		t.Fatal("authenticated new detector absence", err)
	}
	detector, ticket, err := bootstrap.bootstrapInitialDetector(ctx, who, anchor, f.FaceQualifications[1].Report, .8, 20, budget)
	if err != nil {
		// Diagnose the rejected boundary without bypassing admission or
		// committing an exploratory retry. Never log evidence/credentials.
		tx, beginErr := scheduler.Begin(ctx)
		if beginErr == nil {
			built, transportErr := recoverDetectorTransportInTransaction(ctx, tx, who, detector)
			t.Log("bootstrap diagnostic transport", transportErr)
			if transportErr == nil {
				packet, evidenceErr := reader.marshalDetectorAdmissionEvidence(ctx, who, detector, built)
				t.Log("bootstrap diagnostic evidence", evidenceErr)
				if evidenceErr == nil {
					_, _ = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", f.Tenant.String())
					var checks []byte
					diagnosticErr := tx.QueryRow(ctx, `SELECT jsonb_build_object(
                        'anchor',photo.lock_semantic_native_admission_anchor($1,$2,$3::jsonb),
                        'source',photo.lock_semantic_native_source_authority($1,($3::jsonb->>'project_id')::uuid,($3::jsonb->>'actor_id')::uuid,($3::jsonb->>'session_id')::uuid,$3::jsonb),
                        'registration',photo.semantic_native_registration_binding_state($1,$2,'face.detect',$3::jsonb->'registration'),
                        'budget',photo.lock_semantic_native_graph_budget($1,$2,$3::jsonb),
                        'evidence',photo.lock_semantic_native_admission_evidence($1,$2,$3::jsonb))`, f.Tenant, detector.shell.Run, string(packet)).Scan(&checks)
					t.Log("bootstrap diagnostic SQL gates", string(checks), diagnosticErr)
				}
			}
			_ = tx.Rollback(ctx)
		} else {
			t.Log("bootstrap diagnostic transaction", beginErr)
		}
		t.Fatal("physical detector bootstrap", err)
	}
	initialPresence, err = reader.readInitialNativeDetectorPresence(ctx, who, request, key)
	if err != nil || initialPresence != nativeStagePresent {
		t.Fatal("original saved detector presence", err)
	}
	replay, err := reader.resumeInitialDetectorPreparation(ctx, scheduler, who, detector)
	if err != nil || replay != ticket || ticket.RunID != detectorRun {
		t.Fatal("detector replay", err)
	}
	if os.Getenv("PHOTO_TEST_V27_SCENE_WIRE") == "1" {
		probeSceneLifecycle(t, ctx, who, detector, reader, scheduler, documents, transportDocs, catalog, color, f.SceneQualifications)
		return
	}
	if operation := os.Getenv("PHOTO_TEST_V27_NATIVE_TERMINAL_JOB"); operation != "" {
		capacity := TransferPreparation{Color: color, RAM: budget.RAM, VRAM: budget.VRAM, Scratch: budget.Scratch, Output: budget.Output, MetadataBytes: 16 << 20}
		if operation == "mask.person" {
			if os.Getenv("PHOTO_TEST_V27_NATIVE_TERMINAL_DRAFT") == "1" {
				personRun, _, err := personPreparationIDs(request.RunID)
				if err != nil {
					t.Fatal(err)
				}
				interrupted := &nativePersonFailTransport{Run: personRun, Delegate: transportDocs}
				if err := bootstrap.ensureInitialPersonPreparation(ctx, who, anchor, capacity, budget.Reserved, interrupted); err == nil || interrupted.Calls != 1 {
					t.Fatal("original draft publication interruption", err)
				}
				state, err := reader.readNativeInputSavedStageState(ctx, who, request, published.Ref, snapshot, "mask.person")
				if err != nil || state.sealed || state.taskState != "blocked" || state.runState != "draft" {
					t.Fatal("original interrupted draft state", err)
				}
			} else if err := bootstrap.ensureInitialPersonPreparation(ctx, who, anchor, capacity, budget.Reserved, transportDocs); err != nil {
				t.Fatal("terminal original person bootstrap", err)
			}
		}
		preparer := TransferDraftPreparer{Reader: reader, Scheduler: scheduler, Documents: documents, TransportDocuments: transportDocs, Configuration: capacity, ReservedBytes: budget.Reserved}
		probeNativeTerminalProcessOne(t, ctx, preparer, who, detector, operation)
		return
	}
	if os.Getenv("PHOTO_TEST_V27_NATIVE_TERMINAL_CANCEL") == "1" {
		probeNativeTerminalCancellation(t, ctx, reader, scheduler, who, detector)
		return
	}
	if os.Getenv("PHOTO_TEST_V27_FACE_WIRE") == "1" {
		var personInput nativePersonInputAuthority
		if os.Getenv("PHOTO_TEST_V27_ORIGINAL_PERSON") == "1" {
			capacity := TransferPreparation{Color: color, RAM: budget.RAM, VRAM: budget.VRAM, Scratch: budget.Scratch, Output: budget.Output, MetadataBytes: 16 << 20}
			if err := bootstrap.ensureInitialPersonPreparation(ctx, who, anchor, capacity, budget.Reserved, transportDocs); err != nil {
				t.Fatal("initial original person preparation", err)
			}
			originalPlan, err := reader.restorePersonDraftPlan(ctx, catalog, who, anchor)
			if err != nil {
				t.Fatal("original person saved plan", err)
			}
			task, err := originalPlan.prepared.catalog.frozen.DraftTask()
			if err != nil {
				t.Fatal(err)
			}
			probeNativeWireTaskOutputs(t, ctx, task.ID)
			personInput, err = reader.readOriginalNativePersonInput(ctx, who, originalPlan)
			if err != nil {
				t.Fatal("new original person physical authority", err)
			}
			t.Log("V27_ORIGINAL_PERSON_NEW_RESULT_VERIFIED_OK")
		} else {
			personInput, err = reader.readNativePersonInputAuthority(ctx, who, request, f.PersonQualification.Report)
			if err != nil {
				t.Fatal("independent original person input", err)
			}
		}
		personSource, err := reader.Read(ctx, who, request.TenantID, request.Intent.Reference)
		if err != nil {
			t.Fatal("original person protection source", err)
		}
		personProtection, err := reader.prepareNativePersonProtectionInput(ctx, who, request, personInput, personSource)
		if err != nil || !personProtection.Protected || personProtection.Artifact.ID != personInput.pixels.ArtifactID || personProtection.Artifact.SHA256 != personInput.pixels.SHA256 {
			t.Fatal("qualified original person protection projection", err)
		}
		err = database.InTransaction(ctx, scheduler, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", f.Tenant.String()); err != nil {
				return err
			}
			if err := reader.lockNativePersonInputAuthority(ctx, tx, who, request, personInput); err != nil {
				return err
			}
			for i := range personInput.objects {
				changed := personInput
				changed.objects[i].VersionID += "-substituted"
				changed.seal, err = nativePersonInputAuthorityDigest(changed)
				if err != nil {
					return err
				}
				if err := reader.lockNativePersonInputAuthority(ctx, tx, who, request, changed); !errors.Is(err, v2input.ErrConflict) {
					return fmt.Errorf("substituted person input object accepted: %v", err)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal("independent person input locks", err)
		}
		t.Log("V27_ORIGINAL_PERSON_INPUT_AUTHORITY_OK")
		task, err := detector.prepared.frozen.DraftTask()
		if err != nil {
			t.Fatal(err)
		}
		probeNativeWireTaskOutputs(t, ctx, task.ID)
		completed, err := reader.readCompletedDetectorPreparation(ctx, who, detector, color)
		if err != nil || len(completed.boxes.Faces) != 1 {
			t.Fatal("new detector physical result", err)
		}
		var parserPlan, landmarksPlan nativeFaceDraftPlan
		for _, operation := range []string{"face.landmarks", "face.parse"} {
			run, _, _, err := nativeInputSavedStageIdentity(request.RunID, operation)
			if err != nil {
				t.Fatal(err)
			}
			bootstrap.Transport = v2input.ScopedPublisher{Documents: transportDocs, Tenant: f.Tenant, Run: run}
			report := f.FaceQualifications[0].Report
			if operation == "face.parse" {
				report = f.ParseQualification.Report
			}
			child, err := bootstrap.ensureFace(ctx, who, detector, color, operation, report, budget)
			if err != nil {
				t.Fatal("new face child", operation, err)
			}
			task, err := child.plan.prepared.frozen.DraftTask()
			if err != nil {
				t.Fatal(err)
			}
			probeNativeWireTaskOutputs(t, ctx, task.ID)
			if operation == "face.landmarks" {
				landmarksPlan = child.plan
				result, err := reader.readCompletedNativeLandmarks(ctx, who, child.plan, color)
				if err != nil || len(result.faces) != 1 {
					t.Fatal("new landmark physical result", err)
				}
			} else {
				parserPlan = child.plan
				result, err := reader.readCompletedNativeFaceParse(ctx, who, child.plan, color)
				if err != nil || result.labels.ParsedFaces != 1 {
					t.Fatal("new parser physical result", err)
				}
			}
		}
		skinRun, _, _, err := nativeInputSavedStageIdentity(request.RunID, "mask.face_skin")
		if err != nil {
			t.Fatal(err)
		}
		bootstrap.Transport = v2input.ScopedPublisher{Documents: transportDocs, Tenant: f.Tenant, Run: skinRun}
		skin, err := bootstrap.ensureSkin(ctx, who, parserPlan, color, budget)
		if err != nil {
			t.Fatal("new skin child", err)
		}
		skinTask, err := skin.plan.prepared.frozen.DraftTask()
		if err != nil {
			t.Fatal(err)
		}
		probeNativeWireTaskOutputs(t, ctx, skinTask.ID)
		completedSkin, err := reader.readCompletedNativeSkin(ctx, who, skin.plan, color)
		if err != nil || completedSkin.mask.Selector != "skin" || completedSkin.mask.Protected {
			t.Fatal("new skin physical result", err)
		}
		restoredSkin, err := reader.restoreNativeSkinDraftPlan(ctx, who, parserPlan, color)
		if err != nil || restoredSkin.seal != skin.plan.seal {
			t.Fatal("new skin exact restore", err)
		}
		replaySkin, err := bootstrap.ensureSkin(ctx, who, parserPlan, color, budget)
		if err != nil || replaySkin.plan.seal != skin.plan.seal || replaySkin.ticket != skin.ticket {
			t.Fatal("new skin exact replay", err)
		}
		producers := nativeInputRestoredFaceProducers{detector: detector, landmarks: landmarksPlan, parser: parserPlan, skin: skin.plan}
		restoredProducers, err := reader.restoreNativeInputFaceProducers(ctx, catalog, who, request, key, published, snapshot, snapshotBody, color)
		if err != nil || !sameNativeInputFaceProducers(ctx, producers, restoredProducers) {
			t.Fatal("independently restored producer semantic equality", err)
		}
		changedProducers := restoredProducers
		changedProducers.skin.key += "-substituted"
		if sameNativeInputFaceProducers(ctx, producers, changedProducers) {
			t.Fatal("changed producer key accepted")
		}
		manualStageEquality := nativeInputManualInitialStage{dependencies: nativeInputManualInitialDependencies{dependencies: nativeInputManualDependencies{producers: producers}}}
		if !sameNativeInputManualInitialStage(ctx, manualStageEquality, manualStageEquality) {
			t.Fatal("actual producer closures break manual stage equality")
		}
		for _, kind := range []string{"dependency", "qualification", "params", "source"} {
			changed := manualStageEquality
			switch kind {
			case "dependency":
				changed.dependencies.dependencies.landmarks = "substituted"
			case "qualification":
				changed.qualification.dependencySeal = "substituted"
			case "params":
				changed.dependencies.params.params.object.VersionID = "substituted"
			case "source":
				changed.dependencies.dependencies.selection.sourceBody = "substituted"
			}
			if sameNativeInputManualInitialStage(ctx, manualStageEquality, changed) {
				t.Fatal("changed manual stage data accepted", kind)
			}
		}
		transports, err := recoverNativeInputFaceProducerTransports(ctx, scheduler, who, producers)
		if err != nil {
			t.Fatal("new defect producer transports", err)
		}
		personPrepared, err := reader.prepareNativeDefectPersonFaceParams(ctx, who, landmarksPlan, skin.plan, color, [2]float64{.002, .012}, personInput)
		if err != nil {
			t.Fatal("original person defect Params", err)
		}
		personPins, err := nativeDefectPersonInputPins(ctx, personPrepared)
		if err != nil || len(personPins) != 1 || personPins[0].pin.ref != personInput.pixels || personPins[0].pin.object != personInput.objects[1] {
			t.Fatal("original person exact worker input pin", err)
		}
		personDependencies, err := reader.prepareNativeDefectDependencies(ctx, who, personPrepared, color, transports.landmarks, transports.skin, transports.parser, transports.detector)
		if err != nil {
			t.Fatal("original person defect dependencies", err)
		}
		err = database.InTransaction(ctx, scheduler, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", f.Tenant.String()); err != nil {
				return err
			}
			if err := reader.lockNativeDefectDependenciesInTransaction(ctx, tx, who, personDependencies); err != nil {
				return err
			}
			changed := personDependencies
			changedPerson := *personPrepared.person
			changedPerson.objects[1].VersionID += "-substituted"
			changedPerson.seal, err = nativePersonInputAuthorityDigest(changedPerson)
			if err != nil {
				return err
			}
			changed.preparation.person = &changedPerson
			changed.seal, err = nativeDefectDependencyDigest(changed)
			if err != nil {
				return err
			}
			if err := reader.lockNativeDefectDependenciesInTransaction(ctx, tx, who, changed); !errors.Is(err, v2input.ErrConflict) {
				return fmt.Errorf("substituted person dependency accepted: %v", err)
			}
			return nil
		})
		if err != nil {
			t.Fatal("original person defect dependency locks", err)
		}
		if err := validateNativeDefectInputAuthorityScope(ctx, personPrepared.params, personPins); !errors.Is(err, v2input.ErrInvalid) {
			t.Fatal("person defect admission enabled before recovery/SQL packet support", err)
		}
		t.Log("V27_NATIVE_DEFECT_PERSON_DEPENDENCIES_OK")
		defectConfiguration, err := reader.selectInitialDefectConfiguration(ctx, who, f.Tenant, TransferPreparation{Color: color, RAM: 6 << 30, VRAM: 1 << 30, Scratch: 1 << 30, Output: 64 << 20, MetadataBytes: 16 << 20}, 128<<20)
		if err != nil {
			t.Fatal("new defect qualification", err)
		}
		defectRun, _, err := defectPreparationIDs(request.RunID)
		if err != nil {
			t.Fatal(err)
		}
		bootstrap.Transport = v2input.ScopedPublisher{Documents: transportDocs, Tenant: f.Tenant, Run: defectRun}
		if os.Getenv("PHOTO_TEST_V27_PERSON_DEFECT_FRESH") == "1" {
			presence, err := reader.readNativeStagePresence(ctx, who, detector, "defects.propose")
			if err != nil || presence != nativeStageAbsent {
				t.Fatal("automatic fresh defect was not absent", err)
			}
			capacity := TransferPreparation{Color: color, RAM: 6 << 30, VRAM: 1 << 30, Scratch: 1 << 30, Output: 64 << 20, MetadataBytes: 16 << 20}
			submission, err := bootstrap.ensureInitialPersonDefect(ctx, who, restoredProducers, personInput, capacity, 128<<20, transportDocs)
			if err != nil || submission.ticket.RunID != defectRun {
				t.Fatal("automatic absent person defect creation", err)
			}
			replay, err := bootstrap.ensureInitialPersonDefect(ctx, who, restoredProducers, personInput, capacity, 128<<20, transportDocs)
			if err != nil || replay.plan.seal != submission.plan.seal || replay.ticket != submission.ticket {
				t.Fatal("automatic fresh defect original replay", err)
			}
			pending, err := bootstrap.advanceInitialPersonDefect(ctx, who, restoredProducers, personInput, capacity, 128<<20, transportDocs)
			if err != nil || pending.phase != nativePreparationWaiting || !reflect.DeepEqual(pending.protection, registeredNativeInputProtection{}) {
				t.Fatal("pending automatic defect exposed protection", err)
			}
			changed := personInput
			changed.objects[1].VersionID += "-substituted"
			changed.seal, err = nativePersonInputAuthorityDigest(changed)
			if err != nil {
				t.Fatal(err)
			}
			if rejected, err := bootstrap.ensureInitialPersonDefect(ctx, who, restoredProducers, changed, capacity, 128<<20, transportDocs); err == nil || rejected.plan.seal != "" || rejected.ticket != (v2input.JobTicket{}) {
				t.Fatal("automatic existing defect replaced person authority", err)
			}
			_, err = recoverNativeInputDefectTransport(ctx, scheduler, who, submission.plan)
			if err != nil {
				t.Fatal("automatic fresh registered transport", err)
			}
			task, err := submission.plan.prepared.frozen.DraftTask()
			if err != nil {
				t.Fatal(err)
			}
			probeNativeWireTaskOutputs(t, ctx, task.ID)
			completed, err := reader.readCompletedNativeInputDefect(ctx, who, submission.plan, color)
			if err != nil {
				t.Fatal("automatic fresh canonical defect output", err)
			}
			advanced, err := bootstrap.advanceInitialPersonDefect(ctx, who, restoredProducers, personInput, capacity, 128<<20, transportDocs)
			if err != nil || advanced.phase != nativePreparationSucceeded {
				t.Fatal("automatic fresh defect protection advancement", err)
			}
			protection := advanced.protection
			if err != nil || protection.published.preparation.pixels != completed.catalog.Outputs[1] {
				t.Fatal("automatic fresh canonical protection publication", err)
			}
			policies, err := selectQualificationPolicies(personInput.policy, []string{"mask.person"})
			if err != nil {
				t.Fatal(err)
			}
			_, _, covered, err := reader.verifyNativeInputPersonProtectionCoverage(ctx, who, request, protection, color, personInput.report, policies["mask.person"])
			if err != nil || covered.root != personInput.root {
				t.Fatal("automatic fresh person probability coverage", err)
			}
			if os.Getenv("PHOTO_TEST_V27_PERSON_DEFECT_PARENT") == "1" {
				preparer := TransferDraftPreparer{Reader: reader, Scheduler: scheduler, Documents: documents, TransportDocuments: transportDocs, Configuration: capacity, ReservedBytes: 128 << 20}
				targetDraftOnly := os.Getenv("PHOTO_TEST_V27_TARGET_PERSON_DRAFT") == "1"
				var denied v2input.JobTicket
				if !targetDraftOnly {
					denied, err = preparer.admitInitialPersonDefectParent(ctx, who, advanced)
				}
				if !targetDraftOnly && (!errors.Is(err, v2input.ErrConflict) || denied != (v2input.JobTicket{})) {
					t.Fatal("ambiguous automatic targets admitted parent", err)
				}
				existingParent, err := reader.hasNativeInputSavedParent(ctx, who, request)
				if err != nil || existingParent {
					t.Fatal("ambiguous target selection published parent", err)
				}
				if !targetDraftOnly {
					t.Log("V27_NATIVE_PARENT_AMBIGUOUS_TARGET_REFUSED_OK")
				}
				manifest, err := compileNativeTargetPreparationManifest(ctx, who, request, restoredProducers.detector.requestKey, published, restoredProducers.detector.anchor.snapshotRef, restoredProducers.detector.anchor.snapshotBody)
				if err != nil {
					t.Fatal("target prerequisite plan", err)
				}
				locked, err := withNativeManualParentLock(ctx, scheduler, request.TenantID, request.RunID, func(lockCtx context.Context) error {
					for i := 0; i < 2; i++ {
						saved, err := preparer.checkpointNativeTargetPreparation(lockCtx, who, restoredProducers.detector, manifest)
						if err != nil {
							return err
						}
						if saved.sha != manifest.sha || string(saved.body) != string(manifest.body) {
							return v2input.ErrConflict
						}
					}
					return nil
				})
				if err != nil || !locked {
					t.Fatal("target prerequisite durable checkpoint/replay", locked, err)
				}
				t.Log("V27_TARGET_MANIFEST_PRODUCTION_CHECKPOINT_REPLAY_OK")
				if targetDraftOnly {
					var transferConfig TransferPreparation
					var transferReference nativeInputTransferReferenceDocuments
					if os.Getenv("PHOTO_TEST_V27_NATIVE_ALL_TARGET_TRANSFER") == "1" {
						transferConfig, transferReference = prepareNativeAllTargetTransferProbeReference(t, ctx, preparer, who, restoredProducers.detector, advanced)
					}
					probeNativeTargetPersonDraft(t, ctx, preparer, who, restoredProducers.detector, manifest, personInput.report)
					if os.Getenv("PHOTO_TEST_V27_NATIVE_ALL_TARGET_TRANSFER") == "1" {
						probeNativeAllTargetTransfer(t, ctx, preparer, who, restoredProducers.detector, manifest, transferConfig, transferReference)
					}
					return
				}
				// Explicit server-selected target policy. Keep all competing fixture
				// roots live: no newest/first fallback or inventory retirement.
				preparer.Configuration.TargetProtection = make([]v2input.Ref, len(published.Inventory.Targets))
				for i := range preparer.Configuration.TargetProtection {
					preparer.Configuration.TargetProtection[i] = f.RequiredProtection
				}
				parentTicket, err := preparer.admitInitialPersonDefectParent(ctx, who, advanced)
				if err != nil || parentTicket.RunID != request.RunID || parentTicket.WorkflowID != request.WorkflowID || parentTicket.State != "queued" {
					t.Fatal("automatic native parent admission", err)
				}
				parentReplay, err := preparer.admitInitialPersonDefectParent(ctx, who, advanced)
				if err != nil || parentReplay != parentTicket {
					t.Fatal("automatic native parent original replay", err)
				}
				if os.Getenv("PHOTO_TEST_V27_ORIGINAL_PERSON") == "1" {
					if os.Getenv("PHOTO_TEST_V27_INITIAL_PROCESS_ONE") == "1" {
						var claimed, expiredToken uuid.UUID
						if err := scheduler.QueryRow(ctx, "SELECT detector_run_id,lease_token FROM photo.claim_v27_native_initial_pipeline($1,60)", reader.ProjectID).Scan(&claimed, &expiredToken); err != nil || claimed != detectorRun {
							t.Fatal("initial stale lease claim", err)
						}
						owner, err := pgxpool.New(ctx, os.Getenv("PHOTO_TEST_DATABASE_URL"))
						if err != nil {
							t.Fatal(err)
						}
						defer owner.Close()
						// Simulate elapsed lease time through owner-only fixture metadata.
						// Do not disable guards or grant application mutation privileges.
						if err := database.InTransaction(ctx, owner, func(tx pgx.Tx) error {
							if _, err := tx.Exec(ctx, "SET LOCAL ROLE photo_fn_owner"); err != nil {
								return err
							}
							changed, err := tx.Exec(ctx, "UPDATE photo.runs SET v27_initial_recovery_until=clock_timestamp()-interval '1 second' WHERE tenant_id=$1 AND id=$2 AND v27_initial_recovery_token=$3", f.Tenant, detectorRun, expiredToken)
							if err != nil {
								return err
							}
							if changed.RowsAffected() != 1 {
								return v2input.ErrConflict
							}
							return nil
						}); err != nil {
							t.Fatal("initial lease expiry fixture", err)
						}
						var recorded bool
						if err := scheduler.QueryRow(ctx, "SELECT photo.record_v27_native_initial_pipeline_handoff($1,$2,$3,'completed')", f.Tenant, detectorRun, expiredToken).Scan(&recorded); err != nil || recorded {
							t.Fatal("expired initial lease accepted", err)
						}
						job := SQLNativeInitialPipelineRecovery{Preparer: preparer, CatalogForReference: (SQLNativeParentArtifactCatalogFactory{API: api, Bucket: reader.Bucket}).ForReference, Timeout: 6 * time.Minute}
						if worked, err := job.ProcessOne(ctx); err != nil || !worked {
							t.Fatal("initial ProcessOne complete cycle", err)
						}
						if worked, err := job.ProcessOne(ctx); err != nil || worked {
							t.Fatal("completed initial ProcessOne was reclaimed", err)
						}
						if err := scheduler.QueryRow(ctx, "SELECT photo.record_v27_native_initial_pipeline_handoff($1,$2,$3,'completed')", f.Tenant, detectorRun, expiredToken).Scan(&recorded); err != nil || recorded {
							t.Fatal("replaced initial lease accepted", err)
						}
						t.Log("V27_INITIAL_PROCESS_ONE_EXPIRED_LEASE_COMPLETED_OK")
					} else {
						if os.Getenv("PHOTO_TEST_V27_ORIGINAL_PERSON_RECOVER") == "1" {
							job := SQLNativeInitialPipelineRecovery{Preparer: preparer, CatalogForReference: (SQLNativeParentArtifactCatalogFactory{API: api, Bucket: reader.Bucket}).ForReference, Timeout: 6 * time.Minute}
							out, locked, err := job.Recover(ctx, who, f.Tenant, request.RunID)
							if err != nil || !locked {
								t.Fatal("original automatic full Recover", err)
							}
							if outcome, err := nativeInitialPipelineRecoveryOutcome(request.RunID, out, nil); err != nil || outcome != "completed" {
								t.Fatal("original automatic full Recover outcome", outcome, err)
							}
							t.Log("V27_ORIGINAL_PERSON_FULL_RECOVER_OK")
						}
						var claimed, token uuid.UUID
						if err := scheduler.QueryRow(ctx, "SELECT detector_run_id,lease_token FROM photo.claim_v27_native_initial_pipeline($1,1800)", reader.ProjectID).Scan(&claimed, &token); err != nil || claimed != detectorRun {
							t.Fatal("automatic handoff original lease", err)
						}
						var recorded bool
						if err := scheduler.QueryRow(ctx, "SELECT photo.record_v27_native_initial_pipeline_handoff($1,$2,$3,'completed')", f.Tenant, detectorRun, token).Scan(&recorded); err != nil || !recorded {
							t.Fatal("automatic original person durable handoff", err)
						}
						if err := scheduler.QueryRow(ctx, "SELECT photo.record_v27_native_initial_pipeline_handoff($1,$2,$3,'completed')", f.Tenant, detectorRun, token).Scan(&recorded); err != nil || recorded {
							t.Fatal("consumed automatic handoff lease replay", err)
						}
					}
					t.Log("V27_AUTOMATIC_INITIAL_DURABLE_HANDOFF_OK")
				}
				t.Log("V27_NATIVE_DEFECT_PERSON_PARENT_ADMITTED_OK")
			}
			t.Log("V27_NATIVE_DEFECT_PERSON_AUTOMATIC_ADVANCE_OK")
			t.Log("V27_NATIVE_DEFECT_PERSON_AUTOMATIC_FRESH_OK")
			t.Log("V27_NATIVE_DEFECT_PERSON_SAVED_RECOVERY_OK")
			t.Log("V27_NATIVE_DEFECT_PERSON_CANONICAL_OUTPUT_OK")
			t.Log("V27_NATIVE_FACE_NEW_OUTPUT_CHAIN_OK")
			return
		}
		if os.Getenv("PHOTO_TEST_V27_PERSON_DEFECT_RECOVERY") == "1" {
			qualified, err := reader.prepareNativeDefectStageQualification(ctx, who, personPrepared, color, defectConfiguration.Report, defectConfiguration.Budget.Costs)
			if err != nil {
				t.Fatal("person defect qualification", err)
			}
			params, err := reader.publishNativeDefectVersionParams(ctx, who, personPrepared, color, documents)
			if err != nil {
				t.Fatal("person defect Params publication", err)
			}
			document := uuid.NewSHA1(request.RunID, []byte("semantic.defect.params:reference"))
			obj, err := reader.defectVersionParamsObject(ctx, who, request.TenantID, request.Intent.Reference, document, params.Ref)
			if err != nil {
				t.Fatal("person defect Params original object", err)
			}
			budget := defectConfiguration.Budget
			preparedTask, err := reader.compileNativeDefectTask(ctx, who, personDependencies, qualified, defectEvidenceObject{ref: params.Ref, object: obj}, personPins, budget.RAM, budget.VRAM, budget.Scratch, budget.Output)
			if err != nil {
				t.Fatal("person defect original task", err)
			}
			plan, err := reader.compileNativeDefectDraftPlan(ctx, who, preparedTask, budget.Reserved)
			if err != nil {
				t.Fatal("person defect original plan", err)
			}
			var defectConfig map[string]json.RawMessage
			if json.Unmarshal(plan.shell.Config, &defectConfig) != nil || string(defectConfig["queue_class"]) != `"batch"` {
				t.Fatal("native defect batch queue missing")
			}
			legacyDefect := preparedTask
			legacyDefect.legacyQueueOmitted = true
			legacyPlan, err := reader.compileNativeDefectDraftPlan(ctx, who, legacyDefect, budget.Reserved)
			if err != nil {
				t.Fatal("historical defect compile", err)
			}
			var legacyConfig map[string]json.RawMessage
			if json.Unmarshal(legacyPlan.shell.Config, &legacyConfig) != nil {
				t.Fatal("historical defect config")
			}
			if _, present := legacyConfig["queue_class"]; present {
				t.Fatal("historical defect queue fabricated")
			}
			legacyReplay, err := reader.compileNativeDefectDraftPlan(ctx, who, legacyPlan.prepared, budget.Reserved)
			if err != nil || legacyReplay.seal != legacyPlan.seal || string(legacyReplay.shell.Config) != string(legacyPlan.shell.Config) {
				t.Fatal("historical defect replay changed", err)
			}
			interrupted := &nativePersonFailTransport{Run: defectRun, Delegate: transportDocs}
			failedTransport := v2input.ScopedPublisher{Documents: interrupted, Tenant: f.Tenant, Run: defectRun}
			if _, err := reader.persistNativeDefectPreparationInputs(ctx, scheduler, failedTransport, who, plan, color); err == nil || interrupted.Calls != 1 {
				t.Fatal("person defect transport interruption not observed", err)
			}
			restored, err := reader.restoreNativeInputDefectDraftPlan(ctx, scheduler, who, restoredProducers, color, [2]float64{.002, .012}, nil, nil, nil, nil)
			if err != nil || restored.seal != plan.seal || string(restored.shell.Config) != string(plan.shell.Config) {
				t.Fatal("person defect original saved plan reconstruction", err)
			}
			if _, err := recoverNativeInputDefectTransport(ctx, scheduler, who, restored); !errors.Is(err, v2input.ErrConflict) {
				t.Fatal("missing person defect transport was fabricated", err)
			}
			if _, err := reader.persistNativeDefectPreparationInputs(ctx, scheduler, bootstrap.Transport, who, restored, color); err != nil {
				t.Fatal("person defect blocked persistence after interruption", err)
			}
			if _, err := recoverNativeInputDefectTransport(ctx, scheduler, who, restored); err != nil {
				t.Fatal("person defect exact transport after interruption", err)
			}
			if _, err := reader.persistNativeDefectPreparationInputs(ctx, scheduler, bootstrap.Transport, who, restored, color); err != nil {
				t.Fatal("person defect blocked replay", err)
			}
			wrongPins := append([]nativeDefectInputPin(nil), personPins...)
			wrongPins[0].pin.object.VersionID += "-substituted"
			if _, err := reader.restoreNativeInputDefectDraftPlan(ctx, scheduler, who, restoredProducers, color, [2]float64{.002, .012}, nil, nil, nil, wrongPins); !errors.Is(err, v2input.ErrConflict) {
				t.Fatal("caller substituted person recovery input accepted", err)
			}
			for i := range plan.prepared.dependencies.preparation.person.objects {
				locator, err := nativeDefectPersonRecoveryFromProof(ctx, *plan.prepared.dependencies.preparation.person)
				if err != nil {
					t.Fatal(err)
				}
				locator.Files[i].Version += "-substituted"
				if _, err := reader.restoreNativeDefectPersonInput(ctx, who, request, locator); !errors.Is(err, v2input.ErrConflict) {
					t.Fatal("substituted original recovery version accepted", err)
				}
			}
			built, err := recoverNativeInputDefectTransport(ctx, scheduler, who, restored)
			if err != nil {
				t.Fatal(err)
			}
			packet, err := reader.marshalNativeDefectAdmissionEvidence(ctx, who, restored, built)
			if err != nil {
				t.Fatal("person defect admission packet", err)
			}
			err = database.InTransaction(ctx, scheduler, func(tx pgx.Tx) error {
				if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", f.Tenant.String()); err != nil {
					return err
				}
				var ready bool
				if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_native_defect_admission_evidence($1,$2,$3::jsonb)", f.Tenant, restored.shell.Run, string(packet)).Scan(&ready); err != nil {
					return err
				}
				if !ready {
					return fmt.Errorf("original person SQL admission evidence refused")
				}
				for i := range personInput.objects {
					changed := personInput
					changed.objects[i].VersionID += "-substituted"
					changed.seal, err = nativePersonInputAuthorityDigest(changed)
					if err != nil {
						return err
					}
					locator, err := nativeDefectPersonRecoveryFromProof(ctx, changed)
					if err != nil {
						return err
					}
					authority, err := canonical.Marshal(nativePersonInputAuthorityPayload(changed))
					if err != nil {
						return err
					}
					proof, err := canonical.Marshal(map[string]any{"authority_text": string(authority), "params_text": personPrepared.params.params})
					if err != nil {
						return err
					}
					binding, err := canonical.Marshal(personInput.binding)
					if err != nil {
						return err
					}
					locatorBody, err := canonical.Marshal(locator)
					if err != nil {
						return err
					}
					paramsRef, err := canonical.Marshal(params.Ref)
					if err != nil {
						return err
					}
					if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_native_defect_person_authority($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7::jsonb,$8::jsonb,$9::jsonb)", f.Tenant, reader.ProjectID, who.UserID, who.SessionID, string(binding), string(locatorBody), string(proof), personInput.policy.body, string(paramsRef)).Scan(&ready); err != nil {
						return err
					}
					if ready {
						return fmt.Errorf("resealed substituted person object version accepted by SQL")
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal("person defect composed original admission", err)
			}
			for _, kind := range []string{"missing", "null", "authority", "params"} {
				var altered map[string]json.RawMessage
				if err := json.Unmarshal(packet, &altered); err != nil {
					t.Fatal(err)
				}
				if kind == "missing" {
					delete(altered, "person")
				} else if kind == "null" {
					altered["person"] = json.RawMessage(`null`)
				} else {
					var proof map[string]any
					if err := json.Unmarshal(altered["person"], &proof); err != nil {
						t.Fatal(err)
					}
					if kind == "authority" {
						proof["authority_text"] = `{}`
					} else {
						proof["params_text"] = `{}`
					}
					altered["person"], err = canonical.Marshal(proof)
					if err != nil {
						t.Fatal(err)
					}
				}
				body, err := canonical.Marshal(altered)
				if err != nil {
					t.Fatal(err)
				}
				err = database.InTransaction(ctx, scheduler, func(tx pgx.Tx) error {
					_, err := sealNativeDefectPacketInTransaction(ctx, tx, f.Tenant, restored.shell.Run, restored.shell.WorkflowID, body)
					return err
				})
				if !errors.Is(err, v2input.ErrConflict) {
					t.Fatal("altered person admission accepted", kind, err)
				}
			}
			state, err := reader.readNativeInputSavedStageState(ctx, who, request, published.Ref, snapshot, "defects.propose")
			if err != nil || state.sealed || state.taskState != "blocked" {
				t.Fatal("person defect no longer blocked", err)
			}
			if os.Getenv("PHOTO_TEST_V27_PERSON_DEFECT_EXECUTION") == "1" {
				submission, err := reader.startNativeDefectPreparation(ctx, scheduler, documents, bootstrap.Transport, who, personPrepared, color, defectConfiguration.Report, budget.Costs, transports.landmarks, transports.skin, transports.parser, transports.detector, budget.RAM, budget.VRAM, budget.Scratch, budget.Output, budget.Reserved)
				if err != nil || submission.ticket.RunID != restored.shell.Run || submission.plan.seal != restored.seal {
					t.Fatal("person defect queued with original authority", err)
				}
				automatic, err := bootstrap.ensureInitialPersonDefect(ctx, who, restoredProducers, personInput, TransferPreparation{Color: color, RAM: 6 << 30, VRAM: 1 << 30, Scratch: 1 << 30, Output: 64 << 20, MetadataBytes: 16 << 20}, 128<<20, transportDocs)
				if err != nil || automatic.plan.seal != restored.seal || automatic.ticket != submission.ticket {
					t.Fatal("automatic person defect original checkpoint replay", err)
				}
				t.Log("V27_NATIVE_DEFECT_PERSON_AUTOMATIC_REPLAY_OK")
				task, err := restored.prepared.frozen.DraftTask()
				if err != nil {
					t.Fatal(err)
				}
				probeNativeWireTaskOutputs(t, ctx, task.ID)
				completed, err := reader.readCompletedNativeInputDefect(ctx, who, restored, color)
				if err != nil {
					t.Fatal("person defect canonical probability coverage", err)
				}
				protection, err := reader.publishRegisteredNativeInputProtection(ctx, scheduler, who, restored, color, documents, built)
				if err != nil || protection.published.preparation.pixels != completed.catalog.Outputs[1] {
					t.Fatal("person defect canonical protection publication", err)
				}
				policies, err := selectQualificationPolicies(personInput.policy, []string{"mask.person"})
				if err != nil {
					t.Fatal(err)
				}
				_, _, covered, err := reader.verifyNativeInputPersonProtectionCoverage(ctx, who, request, protection, color, personInput.report, policies["mask.person"])
				if err != nil || covered.root != personInput.root {
					t.Fatal("original person positive protection coverage", err)
				}
				t.Log("V27_NATIVE_DEFECT_PERSON_CANONICAL_OUTPUT_OK")
			}
			t.Log("V27_NATIVE_DEFECT_PERSON_SAVED_RECOVERY_OK")
			t.Log("V27_NATIVE_FACE_NEW_OUTPUT_CHAIN_OK")
			return
		}
		defect, err := bootstrap.bootstrapDefect(ctx, who, landmarksPlan, skin.plan, color, [2]float64{.002, .012}, defectConfiguration.Report, nativeDefectProducerTransports{Landmarks: transports.landmarks, Skin: transports.skin, Parser: transports.parser, Detector: transports.detector}, defectConfiguration.Budget)
		if err != nil {
			t.Fatal("new defect child", err)
		}
		defectTask, err := defect.plan.prepared.frozen.DraftTask()
		if err != nil {
			t.Fatal(err)
		}
		probeNativeWireTaskOutputs(t, ctx, defectTask.ID)
		completedDefect, err := reader.readCompletedNativeInputDefect(ctx, who, defect.plan, color)
		if err != nil {
			t.Fatal("new defect physical result", err)
		}
		if err := reader.verifyNativeDefectPersonOutput(ctx, who, personPrepared, []review.ProtectionMask{personProtection}, completedDefect.objects[1]); !errors.Is(err, masknpy.ErrCoverageMissing) {
			t.Fatal("feature-only native output accepted as complete person probability protection", err)
		}
		t.Log("V27_NATIVE_DEFECT_PERSON_OUTPUT_UNCOVERED_OK")
		built, err := recoverNativeInputDefectTransport(ctx, scheduler, who, defect.plan)
		if err != nil {
			t.Fatal("new defect transport", err)
		}
		protection, err := reader.publishRegisteredNativeInputProtection(ctx, scheduler, who, defect.plan, color, documents, built)
		if err != nil || protection.published.preparation.pixels != completedDefect.catalog.Outputs[1] {
			t.Fatal("new registered defect protection", err)
		}
		// Equality checks only: no fabricated coverage or parent admission is used.
		nested := nativeInputTransferReferenceDocuments{person: nativeInputReferencePerson{source: nativeInputSourceContinuation{protection: protection}, person: nativeInputPersonProtectionProof{registered: protection}}, selection: nativeInputSelectionSeparationProof{registered: protection}}
		if !sameNativeInputTransferDocuments(ctx, nested, nested) {
			t.Fatal("populated closure breaks nested semantic equality")
		}
		changedNested := nested
		changedNested.person.person.count++
		if sameNativeInputTransferDocuments(ctx, nested, changedNested) {
			t.Fatal("changed nested person coverage count accepted")
		}
		changedNested = nested
		changedNested.selection.pixelsObject.VersionID += "-substituted"
		if sameNativeInputTransferDocuments(ctx, nested, changedNested) {
			t.Fatal("changed nested selection version accepted")
		}
		t.Log("V27_NESTED_SEMANTIC_EQUALITY_OK")
		personPolicies, err := selectQualificationPolicies(personInput.policy, []string{"mask.person"})
		if err != nil {
			t.Fatal(err)
		}
		_, _, uncovered, coverageErr := reader.verifyNativeInputPersonProtectionCoverage(ctx, who, request, protection, color, f.PersonQualification.Report, personPolicies["mask.person"])
		if !errors.Is(coverageErr, v2qualification.ErrUnqualified) || uncovered.root != (v2input.Ref{}) {
			t.Fatal("native uncovered person dependency classification", coverageErr)
		}
		t.Log("V27_NATIVE_PERSON_COVERAGE_UNAVAILABLE_OK")
		t.Log("V27_NATIVE_DEFECT_NEW_PROTECTION_OK")
		t.Log("V27_NATIVE_FACE_NEW_OUTPUT_CHAIN_OK")
		return
	}
	personRun, _, err := personPreparationIDs(request.RunID)
	if err != nil {
		t.Fatal(err)
	}
	bootstrap.Transport = v2input.ScopedPublisher{Documents: transportDocs, Tenant: f.Tenant, Run: personRun}

	initialPersonCapacity := TransferPreparation{Color: color, RAM: budget.RAM, VRAM: budget.VRAM, Scratch: budget.Scratch, Output: budget.Output, MetadataBytes: 16 << 20}
	initialPersonJob := SQLNativeInitialPipelineRecovery{Preparer: TransferDraftPreparer{Reader: reader, Scheduler: scheduler, Documents: documents, TransportDocuments: transportDocs, Configuration: initialPersonCapacity, ReservedBytes: budget.Reserved}, CatalogForReference: (SQLNativeParentArtifactCatalogFactory{API: api, Bucket: reader.Bucket}).ForReference, Timeout: time.Minute}
	failedPersonPublisher := &nativePersonFailTransport{Run: personRun, Delegate: transportDocs}
	interruptedPersonJob := initialPersonJob
	interruptedPersonJob.Preparer.TransportDocuments = failedPersonPublisher
	if interrupted, locked, err := interruptedPersonJob.Recover(ctx, who, f.Tenant, request.RunID); err == nil || locked || interrupted.phase != "" || failedPersonPublisher.Calls != 1 {
		t.Fatal("person transport interruption not preserved", err)
	}
	interruptedPerson, err := reader.restorePersonDraftPlan(ctx, catalog, who, anchor)
	if err != nil {
		t.Fatal("interrupted person shell lost original recovery inputs", err)
	}
	blockedPerson, err := reader.readNativeInputSavedStageState(ctx, who, request, published.Ref, snapshot, "mask.person")
	if err != nil || blockedPerson.sealed || blockedPerson.taskState != "blocked" {
		t.Fatal("interrupted person shell was admitted", err)
	}
	inspector, err := pgxpool.New(ctx, os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal("inspection connection")
	}
	defer inspector.Close()
	var ordinaryEnabled bool
	if err := inspector.QueryRow(ctx, "SELECT v27_parent_recovery_enabled FROM photo.runs WHERE tenant_id=$1 AND id=$2", f.Tenant, personRun).Scan(&ordinaryEnabled); err != nil || ordinaryEnabled {
		t.Fatal("initial/manual person enabled ordinary parent", err)
	}
	ordinaryJob := SQLOrdinaryParentRecovery{SQLNativePersonAdmissionRecovery: SQLNativePersonAdmissionRecovery{SQLNativeInitialPipelineRecovery: initialPersonJob}}
	if completed, locked, err := ordinaryJob.Recover(ctx, who, f.Tenant, request.RunID); err == nil || completed || locked {
		t.Fatal("initial/manual person converted to ordinary parent", err)
	}
	t.Log("V27_INITIAL_PERSON_ORDINARY_CONTINUATION_DENIED_OK")
	personAdmissionJob := SQLNativePersonAdmissionRecovery{SQLNativeInitialPipelineRecovery: initialPersonJob}
	failedAdmissionJob := personAdmissionJob
	failedAdmissionJob.Preparer.TransportDocuments = failedPersonPublisher
	if worked, err := failedAdmissionJob.ProcessOne(ctx); err == nil || !worked {
		t.Fatal("durable person publication failure did not retain retry", err)
	}
	var failures int
	var retryPending bool
	if err := inspector.QueryRow(ctx, "SELECT v27_initial_recovery_failures,v27_initial_recovery_token IS NULL AND v27_initial_recovery_until IS NULL AND v27_initial_recovery_after>clock_timestamp() AND NOT v27_initial_recovery_done AND NOT graph_sealed FROM photo.runs WHERE tenant_id=$1 AND id=$2", f.Tenant, personRun).Scan(&failures, &retryPending); err != nil || failures != 1 || !retryPending {
		t.Fatal("person retry lease/backoff state", err)
	}
	if worked, err := personAdmissionJob.ProcessOne(ctx); err != nil || worked {
		t.Fatal("person retry ignored persisted backoff", err)
	}
	// Advance only this fixture's due time as the metadata owner; no test sleep.
	if err := database.InTransaction(ctx, inspector, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SET LOCAL ROLE photo_fn_owner"); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "UPDATE photo.runs SET v27_initial_recovery_after=clock_timestamp()-interval '1 second' WHERE tenant_id=$1 AND id=$2", f.Tenant, personRun)
		return err
	}); err != nil {
		t.Fatal("person retry clock fixture", err)
	}
	if worked, err := personAdmissionJob.ProcessOne(ctx); err != nil || !worked {
		t.Fatal("durable blocked person admission recovery", err)
	}
	if worked, err := personAdmissionJob.ProcessOne(ctx); err != nil || worked {
		t.Fatal("completed person admission reclaimed", err)
	}
	t.Log("V27_PERSON_DURABLE_ADMISSION_RECOVERY_OK")
	createPersonPolicy, err := reader.selectNativePersonProtectionPolicy(ctx, who, f.Tenant, 16<<20)
	if err != nil {
		t.Fatal("public person preparation policy", err)
	}
	createPersonPreparer := initialPersonJob.Preparer
	createPersonPreparer.Configuration.PersonProtection = &createPersonPolicy
	if worked, err := createPersonPreparer.prepareMissingCreatePerson(ctx, who, request, key, published); err != nil || !worked {
		t.Fatal("public missing person blocked checkpoint recovery", err)
	}
	if worked, err := createPersonPreparer.prepareMissingCreatePerson(ctx, who, request, key, published); err != nil || !worked {
		t.Fatal("public missing person receipt replay", err)
	}
	t.Log("V27_CREATE_PERSON_PREPARATION_REPLAY_OK")
	initialPersonOut, initialPersonLocked, err := initialPersonJob.Recover(ctx, who, f.Tenant, request.RunID)
	if err != nil || !initialPersonLocked || initialPersonOut.phase != nativePreparationWaiting || initialPersonOut.operation != "face.detect" {
		t.Fatal("production initial person creation while detector pending", err)
	}
	person, err := reader.restorePersonDraftPlan(ctx, catalog, who, anchor)
	if err != nil {
		t.Fatal("production-created person checkpoint", err)
	}
	if person.seal != interruptedPerson.seal {
		t.Fatal("person transport recovery replaced saved shell")
	}
	t.Log("V27_PERSON_PRODUCTION_INTERRUPTED_TRANSPORT_OK")

	ticket, err = reader.resumePersonPreparation(ctx, scheduler, who, person)
	if err != nil {
		t.Fatal("production-created person receipt", err)
	}
	// A second production pass must recover the saved plan, not replace it.
	if _, locked, err := initialPersonJob.Recover(ctx, who, f.Tenant, request.RunID); err != nil || !locked {
		t.Fatal("production initial person existing checkpoint", err)
	}
	unchangedPerson, err := reader.restorePersonDraftPlan(ctx, catalog, who, anchor)
	if err != nil || unchangedPerson.seal != person.seal {
		t.Fatal("production initial person replaced original compiler inputs", err)
	}
	t.Log("V27_PERSON_PRODUCTION_INITIAL_ENSURE_OK")
	replay, err = reader.resumePersonPreparation(ctx, scheduler, who, person)
	if err != nil || replay != ticket || ticket.RunID != personRun {
		t.Fatal("person replay", err)
	}

	// Discard the preparation reader/pool: reconstruct from persisted inputs using
	// an independently opened API connection, then replay the exact child receipt.
	restartConfig, err := pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	if timing != nil {
		restartConfig.ConnConfig.Tracer = timing
	}
	restartConfig.ConnConfig.RuntimeParams["role"] = "photo_api"
	restartedAPI, err := pgxpool.NewWithConfig(ctx, restartConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer restartedAPI.Close()
	restartedReader := SQLTransferSourceReader{DB: restartedAPI, Objects: objects, Bucket: f.Objects.Bucket, ProjectID: reader.ProjectID}
	restartedCatalog := v2draft.SQLArtifactCatalog{DB: restartedAPI, Bucket: f.Objects.Bucket, ProjectID: reader.ProjectID, AssetVersionID: request.Intent.Reference.AssetVersionID}
	recoveredPersonAnchor, err := restartedReader.loadPersonPreparationAnchor(ctx, who, f.Tenant, request.RunID, color, 16<<20)
	if err != nil || !reflect.DeepEqual(recoveredPersonAnchor, anchor) {
		t.Fatal("persisted person anchor reconstruction", err)
	}
	if denied, err := restartedReader.loadPersonPreparationAnchor(ctx, who, f.Tenant, request.RunID, color, 1); err == nil || denied.Request.RunID != uuid.Nil {
		t.Fatal("person anchor budget bypass", err)
	}
	t.Log("V27_PERSON_SAVED_ANCHOR_RECONSTRUCTED_OK")
	restoredPerson, err := restartedReader.restorePersonDraftPlan(ctx, restartedCatalog, who, recoveredPersonAnchor)
	if err != nil || restoredPerson.seal != person.seal {
		t.Fatal("fresh-pool person plan reconstruction", err)
	}
	recoveredPersonTicket, err := restartedReader.resumePersonPreparation(ctx, scheduler, who, restoredPerson)
	if err != nil || recoveredPersonTicket != ticket {
		t.Fatal("fresh-pool person receipt replay", err)
	}
	t.Log("V27_PERSON_SAVED_PLAN_RECONSTRUCTED_OK")

	var initialEnabled, initialDone bool
	var initialLease *uuid.UUID
	if err := inspector.QueryRow(ctx, "SELECT v27_initial_recovery_enabled,v27_initial_recovery_done,v27_initial_recovery_token FROM photo.runs WHERE tenant_id=$1 AND id=$2", f.Tenant, detectorRun).Scan(&initialEnabled, &initialDone, &initialLease); err != nil || !initialEnabled || initialDone || initialLease != nil {
		t.Fatal("detector bootstrap did not atomically persist initial scheduling", err)
	}
	var sealed, ready, receipts int
	runs := []uuid.UUID{detectorRun, personRun}
	if err := inspector.QueryRow(ctx, "SELECT count(*) FROM photo.runs WHERE tenant_id=$1 AND id=ANY($2) AND graph_sealed AND status='queued'", f.Tenant, runs).Scan(&sealed); err != nil || sealed != 2 {
		t.Fatal("native sealed run state", err)
	}
	if err := inspector.QueryRow(ctx, "SELECT count(*) FROM photo.tasks WHERE tenant_id=$1 AND run_id=ANY($2) AND state='ready'", f.Tenant, runs).Scan(&ready); err != nil || ready != 2 {
		t.Fatal("native ready task state", err)
	}
	if err := inspector.QueryRow(ctx, "SELECT count(*) FROM photo.idempotency_records WHERE tenant_id=$1 AND resource_id=ANY($2) AND response_status=202", f.Tenant, runs).Scan(&receipts); err != nil || receipts != 2 {
		t.Fatal("native committed receipts", err)
	}

	waitingPipeline, err := reader.advanceSavedNativeFacePipeline(ctx, scheduler, catalog, who, anchor)
	if err != nil || waitingPipeline.phase != nativePreparationWaiting || waitingPipeline.operation != "face.detect" || waitingPipeline.producers.detector.seal != "" || waitingPipeline.producers.landmarks.seal != "" || waitingPipeline.producers.parser.seal != "" || waitingPipeline.producers.skin.seal != "" {
		t.Fatal("pending detector exposed downstream producer authority", err)
	}

	// Publish actual synthetic FaceBoxes bytes and completed producer metadata.
	// This verifies content handoff without claiming inference/GPU measurements.
	detectorTask, err := detector.prepared.frozen.DraftTask()
	if err != nil {
		t.Fatal(err)
	}
	boxesBody, err := canonical.Marshal(map[string]any{"image": request.Intent.Reference.Source, "faces": []any{}, "model_id": "sfd", "model_lock_id": strings.Repeat("c", 64), "backend": nativeQualifiedFixtureBackend(), "preprocessing_id": "sfd_long1024_bgr_v1", "preview_width": 400, "preview_height": 300, "candidate_count": 0, "accepted_count": 0, "alpha_excluded_fraction": 1, "warnings": []string{"no_faces"}})
	if err != nil {
		t.Fatal(err)
	}
	checksum, err := canonical.Hash(json.RawMessage(boxesBody))
	if err != nil {
		t.Fatal(err)
	}
	outputID, worker := uuid.New(), uuid.New()
	outputObject, err := objects.PutImmutable(ctx, "native-detector-result/"+outputID.String(), bytes.NewReader(boxesBody), int64(len(boxesBody)), checksum, "application/json")
	if err != nil {
		t.Fatal("synthetic detector object publication", err)
	}
	if _, err := inspector.Exec(ctx, "UPDATE photo.runs SET status='running' WHERE id=$1", detectorRun); err != nil {
		t.Fatal(err)
	}
	if _, err := inspector.Exec(ctx, "UPDATE photo.tasks SET attempt=1,fencing_token=1 WHERE id=$1", detectorTask.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := inspector.Exec(ctx, "INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES($1,$2,1,1,$3,clock_timestamp(),clock_timestamp(),$4,$5)", f.Tenant, detectorTask.ID, worker, nativeQualifiedFixtureBackend(), `{"backend":"`+nativeQualifiedFixtureBackend()+`"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := inspector.Exec(ctx, "INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES($1,$2,$3,$4,$5,$6,'application/json',$7,clock_timestamp(),'face_boxes',$8,1,1)", outputID, f.Tenant, f.Objects.Bucket, outputObject.Key, checksum, len(boxesBody), outputObject.VersionID, detectorTask.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := inspector.Exec(ctx, "INSERT INTO photo.output_uploads(tenant_id,task_id,attempt,fence,worker_id,certificate_sha256,idempotency_key,artifact_id,role,bucket,object_key,sha256,bytes,media_type,state,object_version,completed_at) VALUES($1,$2,1,1,$3,$4,$5,$6,'face_boxes',$7,$8,$9,$10,'application/json','ready',$11,clock_timestamp())", f.Tenant, detectorTask.ID, worker, strings.Repeat("a", 64), uuid.New(), outputID, f.Objects.Bucket, outputObject.Key, checksum, len(boxesBody), outputObject.VersionID); err != nil {
		t.Fatal(err)
	}
	outputPin := v2input.Ref{ArtifactID: outputID, SHA256: checksum}
	outputRefs, err := canonical.Marshal([]v2input.Ref{outputPin})
	if err != nil {
		t.Fatal(err)
	}
	var resultID uuid.UUID
	if err := inspector.QueryRow(ctx, "INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES($1,$2,1,1,$3::jsonb,'{}',$4) RETURNING id", f.Tenant, detectorTask.ID, string(outputRefs), checksum).Scan(&resultID); err != nil {
		t.Fatal("synthetic detector result ingestion", err)
	}
	if _, err := inspector.Exec(ctx, "UPDATE photo.tasks SET state='succeeded',canonical_result_id=$1 WHERE id=$2", resultID, detectorTask.ID); err != nil {
		t.Fatal(err)
	}
	completed, err := reader.readCompletedDetectorPreparation(ctx, who, detector, color)
	if err != nil || completed.catalog.ResultID != resultID || completed.catalog.Output != outputPin || completed.body != string(boxesBody) || len(completed.boxes.Faces) != 0 {
		t.Fatal("physical detector content handoff", err)
	}

	for _, operation := range []string{"face.landmarks", "face.parse", "mask.face_skin"} {
		presence, err := reader.readNativeStagePresence(ctx, who, detector, operation)
		if err != nil || presence != nativeStageAbsent {
			t.Fatal("authorized original child absence", operation, err)
		}
	}
	var downstreamPlans []nativeFaceDraftPlan
	var retainedLandmarks nativeFaceDraftPlan
	var retainedLandmarkTransport v2input.Built
	var retainedSkin nativeSkinDraftPlan
	var retainedSkinTransport v2input.Built
	var downstreamTransports []v2input.Built
	var downstreamDetectorTransports []v2input.Built
	for _, operation := range []string{"face.landmarks", "face.parse"} {
		prepared, err := reader.publishNativeFaceStageParams(ctx, who, detector, color, documents, operation)
		if err != nil || prepared.ref.ArtifactID == uuid.Nil || prepared.detector != completed.catalog {
			t.Fatal("physical downstream Params", operation, err)
		}

		report := f.FaceQualifications[0].Report
		if operation == "face.parse" {
			report = f.ParseQualification.Report
		}
		taskPreparation, err := reader.prepareNativeFaceTask(ctx, catalog, who, detector, color, prepared, report, budget)
		if err != nil || taskPreparation.frozen == nil || taskPreparation.taskSHA == "" {
			t.Fatal("qualified downstream frozen task", operation, err)
		}
		task, err := taskPreparation.frozen.DraftTask()
		if err != nil || task.Operation != operation || task.ResourceClass != "gpu" || task.ParamsArtifactID != prepared.ref.ArtifactID {
			t.Fatal("downstream frozen task binding", operation, err)
		}
		_, workflow, err := nativeFacePreparationIDs(request.RunID, operation)
		if err != nil {
			t.Fatal(err)
		}
		expectedTask, err := v2input.TaskID(workflow, operation, 0)
		if err != nil || task.ID != expectedTask {
			t.Fatal("downstream stable task identity", err)
		}

		graph, err := reader.compileNativeFaceDraftPlan(ctx, who, detector, taskPreparation, budget.Reserved)
		if err != nil || len(graph.shell.Tasks) != 1 || graph.shell.Tasks[0].ID != task.ID || graph.shell.Tasks[0].Operation != operation {
			t.Fatal("owned downstream graph plan", operation, err)
		}
		var queueConfig map[string]json.RawMessage
		if json.Unmarshal(graph.shell.Config, &queueConfig) != nil || string(queueConfig["queue_class"]) != `"batch"` {
			t.Fatal("new face queue missing")
		}
		legacyPreparation := taskPreparation
		legacyPreparation.legacyQueueOmitted = true
		legacyGraph, err := reader.compileNativeFaceDraftPlan(ctx, who, detector, legacyPreparation, budget.Reserved)
		if err != nil {
			t.Fatal(err)
		}
		var legacyConfig map[string]json.RawMessage
		if json.Unmarshal(legacyGraph.shell.Config, &legacyConfig) != nil {
			t.Fatal("invalid legacy config")
		}
		if _, present := legacyConfig["queue_class"]; present {
			t.Fatal("legacy face queue changed")
		}
		replay, err := reader.compileNativeFaceDraftPlan(ctx, who, legacyGraph.detector, legacyGraph.prepared, legacyGraph.shell.Bytes)
		if err != nil || replay.seal != legacyGraph.seal || string(replay.shell.Config) != string(legacyGraph.shell.Config) {
			t.Fatal("legacy face replay changed", err)
		}
		if err := verifyNativeFacePlanSeal(ctx, graph); err != nil {
			t.Fatal("downstream plan fingerprint", err)
		}

		for _, expectedOutcome := range []string{"created", "pending"} {
			if err := database.InTransaction(ctx, scheduler, func(tx pgx.Tx) error {
				admission, err := reader.beginNativeFaceDraftInTransaction(ctx, tx, who, graph, color, completed)
				if err != nil {
					return err
				}
				if admission.RunID != graph.shell.Run || admission.Outcome != expectedOutcome {
					return fmt.Errorf("unexpected blocked downstream admission")
				}
				return nil
			}); err != nil {
				t.Fatal("blocked downstream persistence/replay", operation, err)
			}
		}
		var blocked, readyTasks, receipts int
		if err := inspector.QueryRow(ctx, "SELECT count(*) FROM photo.runs WHERE id=$1 AND status='draft' AND NOT graph_sealed", graph.shell.Run).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if err := inspector.QueryRow(ctx, "SELECT count(*) FROM photo.tasks WHERE run_id=$1 AND state='ready'", graph.shell.Run).Scan(&readyTasks); err != nil {
			t.Fatal(err)
		}
		if err := inspector.QueryRow(ctx, "SELECT count(*) FROM photo.idempotency_records WHERE resource_id=$1 AND response_status=202", graph.shell.Run).Scan(&receipts); err != nil {
			t.Fatal(err)
		}
		if blocked != 1 || readyTasks != 0 || receipts != 0 {
			t.Fatal("downstream blocked draft activated prematurely")
		}
		foreignResult := completed
		foreignResult.catalog.ResultID = uuid.New()
		if err := database.InTransaction(ctx, scheduler, func(tx pgx.Tx) error {
			_, err := reader.beginNativeFaceDraftInTransaction(ctx, tx, who, graph, color, foreignResult)
			return err
		}); err == nil {
			t.Fatal("foreign completed SFD accepted by downstream draft")
		}

		physicalPublisher := v2input.ScopedPublisher{Documents: transportDocs, Tenant: f.Tenant, Run: graph.shell.Run}
		physicalTransport, physicalProof, err := publishNativeFaceDraftTransport(ctx, graph, physicalPublisher)
		if err != nil || physicalTransport.Graph.ID != task.ID || physicalProof.Count != physicalTransport.Graph.Built.Count {
			t.Fatal("physical downstream transport publication", operation, err)
		}
		if _, err := marshalNativeFaceRegistrationBinding(ctx, graph, physicalTransport.Graph.Built); err != nil {
			t.Fatal("physical downstream registration binding", operation, err)
		}
		physicalReplay, _, err := publishNativeFaceDraftTransport(ctx, graph, physicalPublisher)
		if err != nil || physicalReplay.Graph.Built.Manifest != physicalTransport.Graph.Built.Manifest || physicalReplay.InputDigest != physicalTransport.InputDigest {
			t.Fatal("physical downstream transport replay", operation, err)
		}

		if err := database.InTransaction(ctx, scheduler, func(tx pgx.Tx) error {
			missing, err := recoverNativeFaceTransportInTransaction(ctx, tx, who, graph)
			if err == nil || missing.Manifest.ArtifactID != uuid.Nil {
				return fmt.Errorf("unregistered face recovery exposed transport")
			}
			return nil
		}); err != nil {
			t.Fatal("missing downstream transport recovery", operation, err)
		}
		for i := 0; i < 2; i++ {
			if err := database.InTransaction(ctx, scheduler, func(tx pgx.Tx) error {
				return reader.registerNativeFaceInputsInTransaction(ctx, tx, who, graph, color, completed, physicalTransport.Graph.Built)
			}); err != nil {
				t.Fatal("physical downstream registration/replay", operation, err)
			}
		}
		coordinated, err := reader.persistNativeFacePreparationInputs(ctx, scheduler, physicalPublisher, who, graph, color, completed)
		if err != nil || coordinated.Graph.Built.Manifest != physicalTransport.Graph.Built.Manifest || coordinated.InputDigest != physicalTransport.InputDigest {
			t.Fatal("downstream persistence coordinator replay", operation, err)
		}
		downstreamPlans = append(downstreamPlans, graph)
		downstreamTransports = append(downstreamTransports, physicalTransport.Graph.Built)
		if err := database.InTransaction(ctx, scheduler, func(tx pgx.Tx) error {
			recovered, err := recoverNativeFaceTransportInTransaction(ctx, tx, who, graph)
			if err != nil {
				return err
			}
			if recovered.Manifest != physicalTransport.Graph.Built.Manifest || recovered.SHA256 != physicalTransport.Graph.Built.SHA256 || recovered.Count != physicalTransport.Graph.Built.Count {
				return fmt.Errorf("registered face recovery changed transport")
			}
			for _, field := range []string{"task", "actor", "session"} {
				requestedTask, actor, session := task.ID, who.UserID, who.SessionID
				switch field {
				case "task":
					requestedTask = uuid.New()
				case "actor":
					actor = uuid.New()
				case "session":
					session = uuid.New()
				}
				var raw []byte
				if err := tx.QueryRow(ctx, "SELECT photo.semantic_face_registered_transport($1,$2,$3,$4,$5)", f.Tenant, graph.shell.Run, requestedTask, actor, session).Scan(&raw); err != nil {
					return err
				}
				if len(raw) != 0 {
					return fmt.Errorf("foreign face recovery %s accepted", field)
				}
			}
			return nil
		}); err != nil {
			t.Fatal("registered downstream transport recovery", operation, err)
		}
		physicalPacket, err := marshalNativeFaceRegistrationBinding(ctx, graph, physicalTransport.Graph.Built)
		if err != nil {
			t.Fatal(err)
		}
		for _, signature := range []string{
			"photo.semantic_face_registration_binding_state(uuid,uuid,text,jsonb)",
			"photo.semantic_face_registered_transport(uuid,uuid,uuid,uuid,uuid)",
			"photo.semantic_face_preparation_registration_state(uuid,uuid,uuid,uuid,text,text,jsonb,jsonb,jsonb,jsonb,jsonb,integer,text)",
		} {
			for _, role := range []string{"photo_api", "photo_scheduler", "photo_worker", "photo_publisher", "photo_portal", "photo_auth", "photo_gateway", "photo_maintenance"} {
				var allowed bool
				if err := inspector.QueryRow(ctx, "SELECT has_function_privilege($1,$2,'EXECUTE')", role, signature).Scan(&allowed); err != nil || allowed != (role == "photo_scheduler") {
					t.Fatal("native face registration ACL", role, err)
				}
			}
		}
		if err := database.InTransaction(ctx, scheduler, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", f.Tenant.String()); err != nil {
				return err
			}
			for _, mutation := range []string{"extra", "task", "count", "operation"} {
				var packet map[string]any
				if err := json.Unmarshal(physicalPacket, &packet); err != nil {
					return err
				}
				selectedOperation := operation
				switch mutation {
				case "extra":
					packet["extra"] = true
				case "task":
					packet["task_id"] = uuid.New().String()
				case "count":
					packet["transport"].(map[string]any)["count"] = physicalTransport.Graph.Built.Count + 1
				case "operation":
					if operation == "face.parse" {
						selectedOperation = "face.landmarks"
					} else {
						selectedOperation = "face.parse"
					}
				}
				raw, err := canonical.Marshal(packet)
				if err != nil {
					return err
				}
				var state string
				if err := tx.QueryRow(ctx, "SELECT photo.semantic_face_registration_binding_state($1,$2,$3,$4::jsonb)", f.Tenant, graph.shell.Run, selectedOperation, string(raw)).Scan(&state); err != nil {
					return err
				}
				if state != "conflict" {
					return fmt.Errorf("changed face registration %s accepted", mutation)
				}
			}
			return nil
		}); err != nil {
			t.Fatal("physical downstream registration substitutions", operation, err)
		}
		if err := inspector.QueryRow(ctx, "SELECT count(*) FROM photo.runs WHERE id=$1 AND status='draft' AND NOT graph_sealed", graph.shell.Run).Scan(&blocked); err != nil || blocked != 1 {
			t.Fatal("registration activated downstream draft", err)
		}
		if err := inspector.QueryRow(ctx, "SELECT count(*) FROM photo.tasks WHERE run_id=$1 AND state='blocked' AND attempt=0", graph.shell.Run).Scan(&blocked); err != nil || blocked != 1 {
			t.Fatal("registration activated downstream task", err)
		}

		var detectorTransport v2input.Built
		if err := database.InTransaction(ctx, scheduler, func(tx pgx.Tx) error {
			var err error
			detectorTransport, err = recoverDetectorTransportInTransaction(ctx, tx, who, detector)
			return err
		}); err != nil {
			t.Fatal("original detector transport for downstream admission", err)
		}
		admissionPacket, err := reader.marshalNativeFaceAdmissionEvidence(ctx, who, graph, completed, physicalTransport.Graph.Built, detectorTransport)
		if err != nil {
			t.Fatal("complete downstream admission packet", operation, err)
		}
		if err := database.InTransaction(ctx, scheduler, func(tx pgx.Tx) error {
			return reader.lockNativeFaceAdmissionInTransaction(ctx, tx, who, graph, completed, physicalTransport.Graph.Built, detectorTransport)
		}); err != nil {
			t.Fatal("complete downstream SQL admission barrier", operation, err)
		}
		for _, signature := range []string{"photo.lock_semantic_face_admission_evidence(uuid,uuid,jsonb)", "photo.lock_semantic_face_graph_budget(uuid,uuid,jsonb)"} {
			for _, role := range []string{"photo_api", "photo_scheduler", "photo_worker", "photo_publisher", "photo_portal", "photo_auth", "photo_gateway", "photo_maintenance"} {
				var allowed bool
				if err := inspector.QueryRow(ctx, "SELECT has_function_privilege($1,$2,'EXECUTE')", role, signature).Scan(&allowed); err != nil || allowed != (role == "photo_scheduler") {
					t.Fatal("native face admission ACL", role, err)
				}
			}
		}
		if err := database.InTransaction(ctx, scheduler, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", f.Tenant.String()); err != nil {
				return err
			}
			for _, mutation := range []string{"extra", "scope", "detector_result", "qualification", "face_object", "source_version", "profile"} {
				var packet map[string]any
				if err := json.Unmarshal(admissionPacket, &packet); err != nil {
					return err
				}
				face := packet["face"].(map[string]any)
				switch mutation {
				case "extra":
					packet["extra"] = true
				case "scope":
					face["actor_id"] = uuid.New().String()
				case "detector_result":
					packet["detector_result"].(map[string]any)["result_id"] = uuid.New().String()
				case "qualification":
					face["qualification"].(map[string]any)["version"] = "changed"
				case "face_object":
					for _, item := range face["files"].([]any) {
						file := item.(map[string]any)
						if file["Contract"] == "FaceBoxes" {
							file["Object"].(map[string]any)["VersionID"] = "changed"
						}
					}
				case "source_version":
					face["source_pins"].([]any)[1].(map[string]any)["version"] = "changed"
				case "profile":
					face["registration"].(map[string]any)["profile"].(map[string]any)["resource_estimate"].(map[string]any)["output_bytes"] = 1
				}
				raw, err := canonical.Marshal(packet)
				if err != nil {
					return err
				}
				var ready bool
				if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_face_admission_evidence($1,$2,$3::jsonb)", f.Tenant, graph.shell.Run, string(raw)).Scan(&ready); err != nil {
					return err
				}
				if ready {
					return fmt.Errorf("changed complete face admission %s accepted", mutation)
				}
			}
			return nil
		}); err != nil {
			t.Fatal("complete downstream admission substitutions", operation, err)
		}

		var originalReserved int64
		if err := inspector.QueryRow(ctx, "SELECT bytes FROM photo.quota_reservations WHERE tenant_id=$1 AND holder_type='run' AND holder_id=$2 AND state='reserved'", f.Tenant, graph.shell.Run).Scan(&originalReserved); err != nil {
			t.Fatal(err)
		}
		if _, err := inspector.Exec(ctx, "UPDATE photo.quota_reservations SET bytes=1 WHERE tenant_id=$1 AND holder_type='run' AND holder_id=$2 AND state='reserved'", f.Tenant, graph.shell.Run); err != nil {
			t.Fatal(err)
		}
		quotaErr := database.InTransaction(ctx, scheduler, func(tx pgx.Tx) error {
			return reader.lockNativeFaceAdmissionInTransaction(ctx, tx, who, graph, completed, physicalTransport.Graph.Built, detectorTransport)
		})
		if _, err := inspector.Exec(ctx, "UPDATE photo.quota_reservations SET bytes=$3 WHERE tenant_id=$1 AND holder_type='run' AND holder_id=$2 AND state='reserved'", f.Tenant, graph.shell.Run, originalReserved); err != nil {
			t.Fatal(err)
		}
		if !errors.Is(quotaErr, v2input.ErrConflict) {
			t.Fatal("insufficient live downstream reservation accepted", operation, quotaErr)
		}
		if err := database.InTransaction(ctx, scheduler, func(tx pgx.Tx) error {
			return reader.lockNativeFaceAdmissionInTransaction(ctx, tx, who, graph, completed, physicalTransport.Graph.Built, detectorTransport)
		}); err != nil {
			t.Fatal("restored downstream reservation rejected", operation, err)
		}

		// Test the sealed-evidence boundary using explicit metadata simulation.
		// This is not activation or a committed queue receipt.
		checkCompleted := func(packet []byte) (bool, error) {
			var ready bool
			err := database.InTransaction(ctx, scheduler, func(tx pgx.Tx) error {
				if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", f.Tenant.String()); err != nil {
					return err
				}
				return tx.QueryRow(ctx, "SELECT photo.lock_semantic_face_completed_evidence($1,$2,$3::jsonb)", f.Tenant, graph.shell.Run, string(packet)).Scan(&ready)
			})
			return ready, err
		}
		if ready, err := checkCompleted(admissionPacket); err != nil || ready {
			t.Fatal("unsealed face accepted as completed evidence", operation, err)
		}
		if _, err := inspector.Exec(ctx, "UPDATE photo.runs SET graph_sealed=true WHERE id=$1", graph.shell.Run); err != nil {
			t.Fatal(err)
		}
		completedReady, completedErr := checkCompleted(admissionPacket)
		if _, err := inspector.Exec(ctx, "UPDATE photo.runs SET graph_sealed=false WHERE id=$1", graph.shell.Run); err != nil {
			t.Fatal(err)
		}
		if completedErr != nil || !completedReady {
			t.Fatal("sealed face evidence simulation rejected", operation, completedErr)
		}
		for _, signature := range []string{
			"photo.semantic_face_completed_binding_state(uuid,uuid,text,jsonb)",
			"photo.semantic_face_completed_registration_state(uuid,uuid,uuid,uuid,text,text,jsonb,jsonb,jsonb,jsonb,jsonb,integer,text)",
			"photo.lock_semantic_face_completed_evidence(uuid,uuid,jsonb)",
		} {
			for _, role := range []string{"photo_api", "photo_scheduler", "photo_worker", "photo_publisher", "photo_portal", "photo_auth", "photo_gateway", "photo_maintenance"} {
				var allowed bool
				if err := inspector.QueryRow(ctx, "SELECT has_function_privilege($1,$2,'EXECUTE')", role, signature).Scan(&allowed); err != nil || allowed != (role == "photo_scheduler") {
					t.Fatal("completed face evidence ACL", role, err)
				}
			}
		}

		transportPublisher := &proposalMemoryTransport{}
		transport, proof, err := publishNativeFaceDraftTransport(ctx, graph, transportPublisher)
		if err != nil || transport.Graph.ID != task.ID || proof.Count != transport.Graph.Built.Count {
			t.Fatal("downstream transport", operation, err)
		}
		packet, err := marshalNativeFaceRegistrationBinding(ctx, graph, transport.Graph.Built)
		if err != nil || len(packet) == 0 {
			t.Fatal("downstream registration proof", operation, err)
		}
		alteredTransport := transport.Graph.Built
		alteredTransport.Count++
		if _, err := marshalNativeFaceRegistrationBinding(ctx, graph, alteredTransport); err == nil {
			t.Fatal("changed downstream transport accepted")
		}
		failingPublisher := &proposalMemoryTransport{failAt: 1}
		failedTask, failedProof, err := publishNativeFaceDraftTransport(ctx, graph, failingPublisher)
		if err == nil || failedTask.Graph.ID != uuid.Nil || failedProof.Count != 0 {
			t.Fatal("failed publication exposed usable downstream transport")
		}
		changed := graph
		changed.shell.Bytes++
		if err := verifyNativeFacePlanSeal(ctx, changed); err == nil {
			t.Fatal("mutated graph budget accepted")
		}
		untouched := &proposalMemoryTransport{}
		if _, _, err := publishNativeFaceDraftTransport(ctx, changed, untouched); err == nil || untouched.calls != 0 {
			t.Fatal("mutated plan reached publication")
		}

		for _, field := range []string{"tenant", "project", "actor", "session", "parent", "bucket", "policy"} {
			foreign := taskPreparation
			switch field {
			case "tenant":
				foreign.tenant = uuid.New()
			case "project":
				foreign.project = uuid.New()
			case "actor":
				foreign.actor = uuid.New()
			case "session":
				foreign.session = uuid.New()
			case "parent":
				foreign.parent = uuid.New()
			case "bucket":
				foreign.bucket = "foreign"
			case "policy":
				foreign.policySHA = strings.Repeat("0", 64)
			}
			if _, err := reader.compileNativeFaceDraftPlan(ctx, who, detector, foreign, budget.Reserved); err == nil {
				t.Fatal("foreign preparation scope accepted", field)
			}
		}
		if _, err := reader.compileNativeFaceDraftPlan(ctx, who, detector, taskPreparation, 1); err == nil {
			t.Fatal("insufficient graph quota accepted")
		}
		refused, err := reader.prepareNativeFaceTask(ctx, catalog, who, detector, color, prepared, f.PersonQualification.Report, budget)
		if err == nil || refused.frozen != nil {
			t.Fatal("wrong downstream qualification accepted", operation)
		}
		paramsReplay, err := reader.publishNativeFaceStageParams(ctx, who, detector, color, documents, operation)
		if err != nil || paramsReplay.ref != prepared.ref || paramsReplay.document != prepared.document {
			t.Fatal("downstream Params replay", operation, err)
		}
		var originalCursor int64
		if err := inspector.QueryRow(ctx, "SELECT last_cursor FROM photo.tenant_event_cursors WHERE tenant_id=$1", f.Tenant).Scan(&originalCursor); err != nil {
			t.Fatal(err)
		}
		if _, err := inspector.Exec(ctx, "UPDATE photo.tenant_event_cursors SET last_cursor=9007199254740991 WHERE tenant_id=$1", f.Tenant); err != nil {
			t.Fatal(err)
		}
		failedTicket, sealErr := reader.resumeNativeFacePreparation(ctx, scheduler, who, graph, completed)
		if _, err := inspector.Exec(ctx, "UPDATE photo.tenant_event_cursors SET last_cursor=$2 WHERE tenant_id=$1", f.Tenant, originalCursor); err != nil {
			t.Fatal(err)
		}
		var postgres *pgconn.PgError
		if !errors.As(sealErr, &postgres) || postgres.Code != "23514" || failedTicket != (v2input.JobTicket{}) {
			t.Fatal("face activation rollback on cursor exhaustion", operation, sealErr)
		}
		if err := inspector.QueryRow(ctx, "SELECT count(*) FROM photo.runs WHERE id=$1 AND status='draft' AND NOT graph_sealed", graph.shell.Run).Scan(&blocked); err != nil || blocked != 1 {
			t.Fatal("failed face seal left activated run", err)
		}
		if err := inspector.QueryRow(ctx, "SELECT count(*) FROM photo.tasks WHERE run_id=$1 AND state='blocked' AND attempt=0", graph.shell.Run).Scan(&blocked); err != nil || blocked != 1 {
			t.Fatal("failed face seal left activated task", err)
		}
		var eventsBefore int
		if err := inspector.QueryRow(ctx, "SELECT count(*) FROM photo.run_events WHERE run_id=$1", graph.shell.Run).Scan(&eventsBefore); err != nil || eventsBefore != 0 {
			t.Fatal("failed face seal emitted event", err)
		}
		faceTicket, err := reader.resumeNativeFacePreparation(ctx, scheduler, who, graph, completed)
		if err != nil || faceTicket.RunID != graph.shell.Run || faceTicket.WorkflowID != graph.shell.WorkflowID || faceTicket.State != "queued" {
			t.Fatal("atomic face seal", operation, err)
		}
		faceReplay, err := reader.resumeNativeFacePreparation(ctx, scheduler, who, graph, completed)
		if err != nil || faceReplay != faceTicket {
			t.Fatal("exact face receipt replay", operation, err)
		}
		// Exercise the production assembly against the same original, admitted
		// child: immutable publication and admission must replay exact receipts.
		faceBootstrap := bootstrap
		faceBootstrap.Transport = v2input.ScopedPublisher{Documents: transportDocs, Tenant: f.Tenant, Run: graph.shell.Run}
		assembled, assemblyErr := faceBootstrap.bootstrapFace(ctx, who, detector, color, operation, report, budget)
		if assemblyErr != nil || assembled.ticket != faceTicket || assembled.plan.seal != graph.seal {
			t.Fatal("composed native face bootstrap replay", operation, assemblyErr)
		}
		ensured, ensureErr := faceBootstrap.ensureFace(ctx, who, detector, color, operation, report, budget)
		if ensureErr != nil || ensured.ticket != faceTicket || ensured.plan.seal != graph.seal {
			t.Fatal("original present child ensure replay", operation, ensureErr)
		}
		prematureResult, lookupErr := reader.nativeFaceResultCatalog(ctx, who, graph)
		if !errors.Is(lookupErr, v2input.ErrConflict) || prematureResult.ResultID != uuid.Nil || prematureResult.Outputs != nil {
			t.Fatal("queued downstream task exposed completed result", operation, lookupErr)
		}
		for _, query := range []string{
			"SELECT count(*) FROM photo.runs WHERE id=$1 AND status='queued' AND graph_sealed",
			"SELECT count(*) FROM photo.tasks WHERE run_id=$1 AND state='ready' AND attempt=0",
			"SELECT count(*) FROM photo.idempotency_records WHERE resource_id=$1 AND response_status=202",
			"SELECT count(*) FROM photo.run_events WHERE run_id=$1 AND type='v2.run.queued'",
			"SELECT count(*) FROM photo.outbox WHERE payload->>'run_id'=$1::text AND routing_key='v2.run.queued'",
		} {
			var count int
			if err := inspector.QueryRow(ctx, query, graph.shell.Run).Scan(&count); err != nil || count != 1 {
				t.Fatal("face seal/replay state", operation, err)
			}
		}
		for _, role := range []string{"photo_api", "photo_scheduler", "photo_worker", "photo_publisher", "photo_portal", "photo_auth", "photo_gateway", "photo_maintenance"} {
			var allowed bool
			if err := inspector.QueryRow(ctx, "SELECT has_function_privilege($1,'photo.seal_semantic_face_preparation(uuid,uuid,jsonb)','EXECUTE')", role).Scan(&allowed); err != nil || allowed != (role == "photo_scheduler") {
				t.Fatal("face sealing ACL", role, err)
			}
		}
		downstreamDetectorTransports = append(downstreamDetectorTransports, detectorTransport)
		// Actual immutable bytes and completed producer metadata, deliberately
		// synthetic placeholders: catalog validation, not contract/model QA.
		if _, err := inspector.Exec(ctx, "UPDATE photo.runs SET status='running' WHERE id=$1", graph.shell.Run); err != nil {
			t.Fatal(err)
		}
		if _, err := inspector.Exec(ctx, "UPDATE photo.tasks SET attempt=1,fencing_token=1 WHERE id=$1", task.ID); err != nil {
			t.Fatal(err)
		}
		faceWorker := uuid.New()
		if _, err := inspector.Exec(ctx, "INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES($1,$2,1,1,$3,clock_timestamp(),clock_timestamp(),$4,$5)", f.Tenant, task.ID, faceWorker, nativeQualifiedFixtureBackend(), `{"backend":"`+nativeQualifiedFixtureBackend()+`"}`); err != nil {
			t.Fatal(err)
		}
		roles, media := []string{"face_landmarks"}, []string{"application/json"}
		if operation == "face.parse" {
			roles = []string{"face_labels", "face_labels_ref"}
			media = []string{"application/octet-stream", "application/json"}
		}
		var faceOutputs []v2input.Ref
		for i, role := range roles {
			body := []byte(`{"synthetic_catalog_fixture":true}`)
			if role == "face_landmarks" {
				var err error
				body, err = canonical.Marshal(map[string]any{"image": request.Intent.Reference.Source, "faces": completed.catalog.Output, "landmarks": []any{}, "model_id": "fan2d", "model_lock_id": strings.Repeat("c", 64), "backend": nativeQualifiedFixtureBackend(), "preprocessing_id": "fan2d_crop256_v1", "mean_confidence": 0, "warnings": []string{"no_faces"}})
				if err != nil {
					t.Fatal(err)
				}
			}

			if operation == "face.parse" {
				image, err := review.ParseWorkingImage([]byte(completed.sourceBody))
				if err != nil {
					t.Fatal(err)
				}
				source := image.Source(review.ArtifactRef{ID: request.Intent.Reference.Source.ArtifactID, SHA256: request.Intent.Reference.Source.SHA256})
				if role == "face_labels" {
					header := fmt.Sprintf("{'descr': '<u2', 'fortran_order': False, 'shape': (%d, %d), }", source.Canvas.Height, source.Canvas.Width)
					header += strings.Repeat(" ", 64-(10+len(header)+1)%64) + "\n"
					body = []byte{0x93, 'N', 'U', 'M', 'P', 'Y', 1, 0, 0, 0}
					binary.LittleEndian.PutUint16(body[8:], uint16(len(header)))
					body = append(body, []byte(header)...)
					body = append(body, make([]byte, source.Canvas.Width*source.Canvas.Height*2)...)
				} else {
					labels := review.FaceLabels19{Image: source.Image, Faces: review.ArtifactRef{ID: completed.catalog.Output.ArtifactID, SHA256: completed.catalog.Output.SHA256}, Labels: review.ArtifactRef{ID: faceOutputs[0].ArtifactID, SHA256: faceOutputs[0].SHA256}, Canvas: source.Canvas, TransformChain: source.TransformChain, Classes: 19, ModelID: "bisenet_face19", ModelLockID: strings.Repeat("c", 64), Backend: nativeQualifiedFixtureBackend(), PreprocessingID: "bisenet_face19_crop512_v1", ParsedFaces: 0, MeanConfidence: 0, Warnings: []string{"no_faces"}}
					body, err = canonical.Marshal(labels)
					if err != nil {
						t.Fatal(err)
					}
				}
			}
			hash := sha256.Sum256(body)
			digest := fmt.Sprintf("%x", hash)
			id := uuid.New()
			object, err := objects.PutImmutable(ctx, "native-face-result/"+id.String(), bytes.NewReader(body), int64(len(body)), digest, media[i])
			if err != nil {
				t.Fatal("synthetic face output publication", operation, err)
			}
			if _, err := inspector.Exec(ctx, "INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES($1,$2,$3,$4,$5,$6,$7,$8,clock_timestamp(),$9,$10,1,1)", id, f.Tenant, f.Objects.Bucket, object.Key, digest, len(body), media[i], object.VersionID, role, task.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := inspector.Exec(ctx, "INSERT INTO photo.output_uploads(tenant_id,task_id,attempt,fence,worker_id,certificate_sha256,idempotency_key,artifact_id,role,bucket,object_key,sha256,bytes,media_type,state,object_version,completed_at) VALUES($1,$2,1,1,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'ready',$13,clock_timestamp())", f.Tenant, task.ID, faceWorker, strings.Repeat("a", 64), uuid.New(), id, role, f.Objects.Bucket, object.Key, digest, len(body), media[i], object.VersionID); err != nil {
				t.Fatal(err)
			}
			faceOutputs = append(faceOutputs, v2input.Ref{ArtifactID: id, SHA256: digest})
		}
		faceRefs, err := canonical.Marshal(faceOutputs)
		if err != nil {
			t.Fatal(err)
		}
		var faceResultID uuid.UUID
		if err := inspector.QueryRow(ctx, "INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES($1,$2,1,1,$3::jsonb,'{}',$4) RETURNING id", f.Tenant, task.ID, string(faceRefs), strings.Repeat("6", 64)).Scan(&faceResultID); err != nil {
			t.Fatal("synthetic completed face result", operation, err)
		}
		if _, err := inspector.Exec(ctx, "UPDATE photo.tasks SET state='succeeded',canonical_result_id=$1 WHERE id=$2", faceResultID, task.ID); err != nil {
			t.Fatal(err)
		}
		found, err := reader.nativeFaceResultCatalog(ctx, who, graph)
		if err != nil || found.ResultID != faceResultID || found.Params != prepared.ref || len(found.Outputs) != len(faceOutputs) {
			t.Fatal("positive native face result catalog", operation, err)
		}
		for i, ref := range faceOutputs {
			if found.Outputs[i] != ref {
				t.Fatal("native face catalog changed ordered outputs")
			}
		}
		if operation == "face.parse" {
			verified, err := reader.readCompletedNativeFaceParse(ctx, who, graph, color)
			if err != nil || verified.catalog.ResultID != faceResultID || verified.body == "" || verified.labels.ParsedFaces != 0 || verified.counts[0] != 120000 {
				t.Fatal("positive byte-verified BiSeNet content", err)
			}
			for _, count := range verified.counts[1:] {
				if count != 0 {
					t.Fatal("synthetic background labels changed class counts")
				}
			}
			skin, err := reader.publishNativeSkinStageParams(ctx, who, graph, color, documents)
			if err != nil || skin.document != uuid.NewSHA1(request.RunID, []byte("semantic.mask.face_skin.params:reference")) || skin.ref.ArtifactID == uuid.Nil || skin.parser.catalog.ResultID != faceResultID {
				t.Fatal("physical native skin Params publication", err)
			}
			skinReplay, err := reader.publishNativeSkinStageParams(ctx, who, graph, color, documents)
			if err != nil || skinReplay.document != skin.document || skinReplay.ref != skin.ref {
				t.Fatal("native skin Params replay changed immutable reference", err)
			}
			sourceMeta, err := v2input.ResolveSemanticSourceMetadata(request.Intent.Reference, json.RawMessage(verified.detector.sourceBody))
			if err != nil {
				t.Fatal(err)
			}
			aliases := []v2input.Ref{sourceMeta.Inputs.Pixels}
			if sourceMeta.Inputs.Alpha != nil {
				aliases = append(aliases, *sourceMeta.Inputs.Alpha)
			}
			aliases = append(aliases, sourceMeta.Geometry...)
			for _, alias := range aliases {
				ref, err := documents.PublishSemanticFaceSkinParams(ctx, who, f.Tenant, alias.ArtifactID, request.Intent.Reference, json.RawMessage(verified.detector.sourceBody), verified.catalog.Outputs[1], json.RawMessage(verified.body), verified.catalog.Params, json.RawMessage(verified.paramsBody), verified.detector.catalog.Output, json.RawMessage(verified.detector.body))
				if !errors.Is(err, v2input.ErrInvalid) || ref.ArtifactID != uuid.Nil {
					t.Fatal("skin publisher accepted source-child alias", err)
				}
			}
			skinTask, err := reader.prepareNativeSkinTask(ctx, who, graph, color, skin, 1<<30, 1<<30, 64<<20)
			if err != nil {
				t.Fatal("physical native CPU skin task preparation", err)
			}
			skinDraft, err := skinTask.frozen.DraftTask()
			if err != nil || skinDraft.Operation != "mask.face_skin" || skinDraft.ResourceClass != "cpu" || skinDraft.ParamsArtifactID != skin.ref.ArtifactID {
				t.Fatal("native skin task profile or Params changed", err)
			}
			if _, err := reader.prepareNativeSkinTask(ctx, who, graph, color, skin, 1, 1, 1); err == nil {
				t.Fatal("native CPU skin task accepted impossible resource limits")
			}
			skinGraph, err := reader.compileNativeSkinDraftPlan(ctx, who, graph, skinTask, 64<<20)
			if err != nil || verifyNativeSkinPlanSeal(ctx, skinGraph) != nil || len(skinGraph.shell.Tasks) != 1 || len(skinGraph.shell.Edges) != 0 {
				t.Fatal("owned native skin graph", err)
			}
			var skinConfig map[string]json.RawMessage
			if json.Unmarshal(skinGraph.shell.Config, &skinConfig) != nil || string(skinConfig["queue_class"]) != `"batch"` {
				t.Fatal("new skin queue missing")
			}
			legacySkin := skinTask
			legacySkin.legacyQueueOmitted = true
			legacyGraph, err := reader.compileNativeSkinDraftPlan(ctx, who, graph, legacySkin, 64<<20)
			if err != nil {
				t.Fatal(err)
			}
			var oldSkinConfig map[string]json.RawMessage
			if json.Unmarshal(legacyGraph.shell.Config, &oldSkinConfig) != nil {
				t.Fatal("invalid legacy skin")
			}
			if _, ok := oldSkinConfig["queue_class"]; ok {
				t.Fatal("historical skin queue changed")
			}
			replay, err := reader.compileNativeSkinDraftPlan(ctx, who, legacyGraph.parser, legacyGraph.prepared, legacyGraph.shell.Bytes)
			if err != nil || replay.seal != legacyGraph.seal || string(replay.shell.Config) != string(legacyGraph.shell.Config) {
				t.Fatal("historical skin replay changed", err)
			}

			lockSkin := func() error {
				return database.InTransaction(ctx, scheduler, func(tx pgx.Tx) error { return reader.lockNativeSkinDependencyInTransaction(ctx, tx, who, skinGraph) })
			}
			if err := lockSkin(); err != nil {
				t.Fatal("positive locked native parser dependency", err)
			}
			if _, err := inspector.Exec(ctx, "UPDATE photo.output_uploads SET completed_at=NULL WHERE artifact_id=$1", faceOutputs[0].ArtifactID); err != nil {
				t.Fatal(err)
			}
			lockErr := lockSkin()
			if _, err := inspector.Exec(ctx, "UPDATE photo.output_uploads SET completed_at=clock_timestamp() WHERE artifact_id=$1", faceOutputs[0].ArtifactID); err != nil {
				t.Fatal(err)
			}
			if !errors.Is(lockErr, v2input.ErrConflict) {
				t.Fatal("parser dependency lock accepted incomplete output", lockErr)
			}
			if err := lockSkin(); err != nil {
				t.Fatal("restored parser dependency lock", err)
			}
			for _, expected := range []string{"created", "pending"} {
				if err := database.InTransaction(ctx, scheduler, func(tx pgx.Tx) error {
					admission, err := reader.beginNativeSkinDraftInTransaction(ctx, tx, who, skinGraph, color)
					if err != nil {
						return err
					}
					if admission.RunID != skinGraph.shell.Run || admission.Outcome != expected {
						return fmt.Errorf("unexpected skin blocked draft outcome")
					}
					return nil
				}); err != nil {
					t.Fatal("skin blocked persistence/replay", expected, err)
				}
			}
			var skinBlocked int
			if err := inspector.QueryRow(ctx, "SELECT count(*) FROM photo.runs r JOIN photo.tasks t ON t.run_id=r.id AND t.tenant_id=r.tenant_id WHERE r.id=$1 AND r.status='draft' AND NOT r.graph_sealed AND t.state='blocked' AND t.attempt=0", skinGraph.shell.Run).Scan(&skinBlocked); err != nil || skinBlocked != 1 {
				t.Fatal("skin persistence activated task", err)
			}
			skinPublisher := v2input.ScopedPublisher{Documents: transportDocs, Tenant: f.Tenant, Run: skinGraph.shell.Run}
			if err := database.InTransaction(ctx, scheduler, func(tx pgx.Tx) error {
				missing, err := recoverNativeSkinTransportInTransaction(ctx, tx, who, skinGraph)
				if !errors.Is(err, v2input.ErrConflict) || missing.Manifest.ArtifactID != uuid.Nil {
					return fmt.Errorf("unregistered skin recovery exposed transport")
				}
				return nil
			}); err != nil {
				t.Fatal("skin missing recovery", err)
			}
			skinTransport, skinProof, err := publishNativeSkinDraftTransport(ctx, skinGraph, skinPublisher)
			if err != nil || skinTransport.Graph.ID != skinDraft.ID || skinProof.Count != 3 {
				t.Fatal("physical skin transport publication", err)
			}
			skinTransportReplay, _, err := publishNativeSkinDraftTransport(ctx, skinGraph, skinPublisher)
			if err != nil || skinTransportReplay.Graph.Built.Manifest != skinTransport.Graph.Built.Manifest || skinTransportReplay.InputDigest != skinTransport.InputDigest {
				t.Fatal("skin transport immutable replay", err)
			}
			invalidSkinTransport := skinTransport.Graph.Built
			invalidSkinTransport.Count++
			if _, err := skinTask.frozen.BindPublishedTransport(ctx, invalidSkinTransport); err == nil {
				t.Fatal("skin frozen transport accepted wrong entry count")
			}
			for i := 0; i < 2; i++ {
				if err := database.InTransaction(ctx, scheduler, func(tx pgx.Tx) error {
					return reader.registerNativeSkinInputsInTransaction(ctx, tx, who, skinGraph, color, skinTransport.Graph.Built)
				}); err != nil {
					t.Fatal("skin inputs registration/replay", err)
				}
			}
			coordinatedSkin, err := reader.persistNativeSkinPreparationInputs(ctx, scheduler, skinPublisher, who, skinGraph, color)
			if err != nil || coordinatedSkin.Graph.ID != skinDraft.ID || coordinatedSkin.Graph.Built.Manifest != skinTransport.Graph.Built.Manifest {
				t.Fatal("skin coordinator registered replay", err)
			}
			retainedSkin, retainedSkinTransport = skinGraph, coordinatedSkin.Graph.Built
			for _, role := range []string{"photo_api", "photo_scheduler", "photo_worker", "photo_publisher", "photo_portal", "photo_auth", "photo_gateway", "photo_maintenance"} {
				var callable bool
				if err := inspector.QueryRow(ctx, "SELECT has_function_privilege($1,'photo.semantic_native_skin_result_ready(uuid,uuid,integer,bigint,jsonb)','EXECUTE')", role).Scan(&callable); err != nil || callable {
					t.Fatal("private skin result helper exposed to service", role, err)
				}
			}
			skinAdmission, err := reader.marshalNativeSkinAdmissionEvidence(ctx, who, skinGraph, coordinatedSkin.Graph.Built, physicalTransport.Graph.Built, detectorTransport)
			if err != nil || len(skinAdmission) == 0 || len(skinAdmission) > 32<<20 {
				t.Fatal("bounded composed skin admission evidence", err)
			}
			if err := database.InTransaction(ctx, scheduler, func(tx pgx.Tx) error {
				return reader.lockNativeSkinAdmissionInTransaction(ctx, tx, who, skinGraph, coordinatedSkin.Graph.Built, physicalTransport.Graph.Built, detectorTransport)
			}); err != nil {
				t.Fatal("positive composed skin admission", err)
			}
			for _, mutation := range []string{"extra", "parser_result", "session", "parent", "params_version", "registration_count"} {
				var packet map[string]any
				if err := json.Unmarshal(skinAdmission, &packet); err != nil {
					t.Fatal(err)
				}
				switch mutation {
				case "extra":
					packet["unexpected"] = true
				case "parser_result":
					packet["parser_result"].(map[string]any)["result_id"] = uuid.New().String()
				case "params_version":
					packet["params_pin"].(map[string]any)["version"] = "foreign-version"
				case "registration_count":
					packet["registration"].(map[string]any)["transport"].(map[string]any)["count"] = 4
				case "session", "parent":
					var config map[string]any
					if err := json.Unmarshal([]byte(packet["config_text"].(string)), &config); err != nil {
						t.Fatal(err)
					}
					key := "auth_session_id"
					if mutation == "parent" {
						key = "parent_run_id"
					}
					config["v27_skin_prepare"].(map[string]any)[key] = uuid.New().String()
					body, err := canonical.Marshal(config)
					if err != nil {
						t.Fatal(err)
					}
					packet["config_text"] = string(body)
				}
				raw, err := json.Marshal(packet)
				if err != nil {
					t.Fatal(err)
				}
				if err := database.InTransaction(ctx, scheduler, func(tx pgx.Tx) error {
					if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", f.Tenant.String()); err != nil {
						return err
					}
					var allowed bool
					if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_skin_admission_evidence($1,$2,$3::jsonb)", f.Tenant, skinGraph.shell.Run, string(raw)).Scan(&allowed); err != nil {
						return err
					}
					if allowed {
						return fmt.Errorf("skin admission accepted %s substitution", mutation)
					}
					return nil
				}); err != nil {
					t.Fatal("skin admission mutation refusal", mutation, err)
				}
			}
			if err := database.InTransaction(ctx, scheduler, func(tx pgx.Tx) error {
				recovered, err := recoverNativeSkinTransportInTransaction(ctx, tx, who, skinGraph)
				if err != nil {
					return err
				}
				if recovered.Manifest != coordinatedSkin.Graph.Built.Manifest || recovered.Count != 3 {
					return fmt.Errorf("skin recovery changed registered transport")
				}
				return nil
			}); err != nil {
				t.Fatal("skin registered recovery", err)
			}
			lockSkinBudget := func() error {
				return database.InTransaction(ctx, scheduler, func(tx pgx.Tx) error {
					return reader.lockNativeSkinGraphBudgetInTransaction(ctx, tx, who, skinGraph, color, coordinatedSkin.Graph.Built)
				})
			}
			if err := lockSkinBudget(); err != nil {
				t.Fatal("positive skin graph budget", err)
			}
			var skinReserved int64
			if err := inspector.QueryRow(ctx, "SELECT bytes FROM photo.quota_reservations WHERE tenant_id=$1 AND holder_type='run' AND holder_id=$2 AND state='reserved'", f.Tenant, skinGraph.shell.Run).Scan(&skinReserved); err != nil {
				t.Fatal(err)
			}
			if _, err := inspector.Exec(ctx, "UPDATE photo.quota_reservations SET bytes=1 WHERE tenant_id=$1 AND holder_type='run' AND holder_id=$2 AND state='reserved'", f.Tenant, skinGraph.shell.Run); err != nil {
				t.Fatal(err)
			}
			budgetErr := lockSkinBudget()
			if _, err := inspector.Exec(ctx, "UPDATE photo.quota_reservations SET bytes=$3 WHERE tenant_id=$1 AND holder_type='run' AND holder_id=$2 AND state='reserved'", f.Tenant, skinGraph.shell.Run, skinReserved); err != nil {
				t.Fatal(err)
			}
			if !errors.Is(budgetErr, v2input.ErrConflict) {
				t.Fatal("skin graph accepted insufficient live quota", budgetErr)
			}
			if err := lockSkinBudget(); err != nil {
				t.Fatal("restored skin graph budget", err)
			}
			for _, signature := range []string{
				"photo.lock_semantic_native_parse_result(uuid,uuid,uuid,uuid,uuid,text,uuid,jsonb)",
				"photo.lock_semantic_native_skin_result(uuid,uuid,uuid,uuid,uuid,text,uuid,jsonb)",
				"photo.lock_semantic_native_landmark_result(uuid,uuid,uuid,uuid,uuid,text,uuid,jsonb)",
				"photo.lock_semantic_native_defect_result(uuid,uuid,uuid,uuid,uuid,text,uuid,jsonb)",
				"photo.semantic_native_defect_registration_binding_state(uuid,uuid,text,jsonb)",
				"photo.lock_semantic_native_defect_graph_budget(uuid,uuid,jsonb)",
				"photo.lock_semantic_native_defect_admission_evidence(uuid,uuid,jsonb)",
				"photo.lock_semantic_native_defect_completed_evidence(uuid,uuid,jsonb)",
				"photo.semantic_native_defect_completed_binding_state(uuid,uuid,text,jsonb)",
				"photo.seal_semantic_native_defect_preparation(uuid,uuid,jsonb)",
				"photo.lock_semantic_native_manual_admission_evidence(uuid,uuid,jsonb)",
				"photo.seal_semantic_native_manual_preparation(uuid,uuid,jsonb)",
				"photo.enable_v27_native_manual_parent(uuid,uuid,jsonb)",
				"photo.claim_v27_native_manual_parent(uuid,integer)",
				"photo.seal_and_enable_v27_native_initial_pipeline(uuid,uuid,jsonb)",
				"photo.claim_v27_native_initial_pipeline(uuid,integer)",
				"photo.record_v27_native_initial_pipeline_handoff(uuid,uuid,uuid,text)",
				"photo.next_v27_native_parent_recovery_project(uuid)",
				"photo.next_v27_native_initial_recovery_project(uuid)",
				"photo.record_v27_native_manual_parent(uuid,uuid,uuid,text)",
				"photo.lock_semantic_native_manual_result(uuid,uuid,uuid,uuid,uuid,text,uuid,jsonb)",
				"photo.lock_semantic_native_manual_input_protection_evidence(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,jsonb,uuid,text,jsonb,jsonb)",
				"photo.register_semantic_native_manual_input_protection(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,jsonb,uuid,uuid,text,text,text,jsonb)",
				"photo.semantic_native_defect_registered_transport(uuid,uuid,uuid,uuid,uuid)",
				"photo.register_semantic_native_input_protection(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,jsonb,uuid,uuid,text,text,text,jsonb)",
				"photo.lock_semantic_native_input_protection_evidence(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,jsonb,uuid,text,jsonb,jsonb)",
				"photo.semantic_native_input_transfer_registered_transport(uuid,uuid,uuid,uuid,uuid)",
				"photo.lock_semantic_skin_graph_budget(uuid,uuid,jsonb)",
				"photo.lock_semantic_skin_admission_evidence(uuid,uuid,jsonb)",
				"photo.seal_semantic_skin_preparation(uuid,uuid,jsonb)",
				"photo.lock_semantic_skin_completed_evidence(uuid,uuid,jsonb)",
				"photo.semantic_skin_completed_binding_state(uuid,uuid,text,jsonb)",
				"photo.semantic_skin_completed_registration_state(uuid,uuid,uuid,uuid,text,text,jsonb,jsonb,jsonb,jsonb,jsonb,integer,text)",
				"photo.semantic_skin_registered_transport(uuid,uuid,uuid,uuid,uuid)",
				"photo.semantic_skin_registration_binding_state(uuid,uuid,text,jsonb)",
				"photo.semantic_skin_preparation_registration_state(uuid,uuid,uuid,uuid,text,text,jsonb,jsonb,jsonb,jsonb,jsonb,integer,text)",
			} {
				for _, role := range []string{"photo_api", "photo_scheduler", "photo_worker", "photo_publisher", "photo_portal", "photo_auth", "photo_gateway", "photo_maintenance"} {
					var allowed bool
					if err := inspector.QueryRow(ctx, "SELECT has_function_privilege($1,$2,'EXECUTE')", role, signature).Scan(&allowed); err != nil || allowed != (role == "photo_scheduler") {
						t.Fatal("native skin SQL ACL", role, err)
					}
				}
			}

			for _, role := range []string{"photo_api", "photo_scheduler", "photo_worker", "photo_publisher", "photo_portal", "photo_auth", "photo_gateway", "photo_maintenance"} {
				var allowed bool
				for _, signature := range []string{"photo.v27_native_person_preparation_presence(uuid,uuid,uuid,uuid,uuid,text)", "photo.semantic_person_saved_anchor_document(uuid,uuid,uuid,uuid,uuid,text,text)", "photo.semantic_native_input_protection_objects(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,jsonb,uuid,text)", "photo.semantic_native_input_transfer_saved_config(uuid,uuid,uuid,uuid,uuid)", "photo.semantic_native_input_transfer_saved_document(uuid,uuid,uuid,uuid,uuid,integer,uuid,text)", "photo.semantic_native_input_defect_saved_config(uuid,uuid,uuid,uuid,uuid)", "photo.semantic_native_input_defect_saved_params(uuid,uuid,uuid,uuid,uuid,uuid,text)", "photo.semantic_native_input_saved_stage(uuid,uuid,uuid,uuid,uuid,uuid,text)", "photo.semantic_native_input_saved_stage_state(uuid,uuid,uuid,uuid,uuid,uuid,text,text)", "photo.semantic_native_input_saved_anchor_document(uuid,uuid,uuid,uuid,uuid,text,text)", "photo.semantic_native_input_stage_presence(uuid,uuid,uuid,uuid,uuid,text,text)", "photo.v27_native_initial_detector_presence(uuid,uuid,uuid,uuid,uuid,text)", "photo.v27_native_manual_selection_presence(uuid,uuid,uuid,uuid,uuid,text)", "photo.v27_native_manual_child_terminal(uuid,uuid,uuid,uuid,uuid,text)", "photo.read_v27_native_manual_selection(uuid,uuid,uuid,uuid,uuid,text)", "photo.semantic_proposal_transfer_qualification_candidates(uuid,uuid,uuid,uuid,jsonb)", "photo.semantic_native_input_saved_protection(uuid,uuid,uuid,uuid,uuid,uuid)", "photo.semantic_native_input_saved_intent(uuid,uuid,uuid,uuid,uuid,uuid)"} {
					if err := inspector.QueryRow(ctx, "SELECT has_function_privilege($1,$2,'EXECUTE')", role, signature).Scan(&allowed); err != nil || allowed != (role == "photo_api") {
						t.Fatal("native input saved reader API ACL", role, err)
					}
				}
			}
			for _, role := range []string{"photo_api", "photo_scheduler", "photo_worker", "photo_publisher", "photo_portal", "photo_auth", "photo_gateway", "photo_maintenance"} {
				var allowed bool
				if err := inspector.QueryRow(ctx, "SELECT has_function_privilege($1,$2,'EXECUTE')", role, "photo.semantic_native_defect_preparation_registration_state(uuid,uuid,uuid,uuid,text,text,jsonb,jsonb,jsonb,jsonb,jsonb,integer,text)").Scan(&allowed); err != nil || allowed {
					t.Fatal("native defect raw registration helper must remain private", role, err)
				}
			}
			for _, role := range []string{"photo_api", "photo_scheduler", "photo_worker", "photo_publisher", "photo_portal", "photo_auth", "photo_gateway", "photo_maintenance"} {
				var allowed bool
				if err := inspector.QueryRow(ctx, "SELECT has_function_privilege($1,$2,'EXECUTE')", role, "photo.semantic_native_defect_result_ready(uuid,uuid,integer,bigint,jsonb)").Scan(&allowed); err != nil || allowed {
					t.Fatal("native defect result helper must remain private", role, err)
				}
			}
			skinBinding, err := marshalNativeSkinRegistrationBinding(ctx, skinGraph, coordinatedSkin.Graph.Built)
			if err != nil {
				t.Fatal(err)
			}
			if err := database.InTransaction(ctx, scheduler, func(tx pgx.Tx) error {
				if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", f.Tenant.String()); err != nil {
					return err
				}
				for _, mutation := range []string{"extra", "task", "count", "operation"} {
					var packet map[string]any
					if err := json.Unmarshal(skinBinding, &packet); err != nil {
						return err
					}
					op := "mask.face_skin"
					switch mutation {
					case "extra":
						packet["unexpected"] = true
					case "task":
						packet["task_id"] = uuid.New().String()
					case "count":
						packet["transport"].(map[string]any)["count"] = 4
					case "operation":
						op = "face.parse"
					}
					raw, err := json.Marshal(packet)
					if err != nil {
						return err
					}
					var state string
					if err := tx.QueryRow(ctx, "SELECT photo.semantic_skin_registration_binding_state($1,$2,$3,$4::jsonb)", f.Tenant, skinGraph.shell.Run, op, string(raw)).Scan(&state); err != nil {
						return err
					}
					if state != "conflict" {
						return fmt.Errorf("skin registration accepted malformed %s", mutation)
					}
				}
				return nil
			}); err != nil {
				t.Fatal("skin registration malformed packets", err)
			}
			if err := inspector.QueryRow(ctx, "SELECT count(*) FROM photo.tasks WHERE run_id=$1 AND state='blocked' AND attempt=0", skinGraph.shell.Run).Scan(&skinBlocked); err != nil || skinBlocked != 1 {
				t.Fatal("skin coordinator activated task", err)
			}
			mutatedSkin := skinGraph
			mutatedSkin.shell.Bytes++
			if verifyNativeSkinPlanSeal(ctx, mutatedSkin) == nil {
				t.Fatal("skin graph seal accepted altered quota")
			}
			foreignSkin := skinTask
			foreignSkin.session = uuid.New()
			if _, err := reader.compileNativeSkinDraftPlan(ctx, who, graph, foreignSkin, 64<<20); err == nil {
				t.Fatal("skin graph accepted foreign preparation session")
			}
			if _, err := reader.compileNativeSkinDraftPlan(ctx, who, graph, skinTask, 1); err == nil {
				t.Fatal("skin graph accepted insufficient output reserve")
			}
			changedSizes := skinTask
			changedSizes.labels.object.Bytes++
			if _, err := reader.compileNativeSkinDraftPlan(ctx, who, graph, changedSizes, 64<<20); err == nil {
				t.Fatal("skin graph accepted changed label size with old CPU profile")
			}
			changedParamsSize := skinTask
			changedParamsSize.paramsObject.object.Bytes--
			if _, err := reader.compileNativeSkinDraftPlan(ctx, who, graph, changedParamsSize, 64<<20); err == nil {
				t.Fatal("skin graph accepted changed Params size with old CPU profile")
			}
			// Enable only the disposable fixture's CPU operation, never production.
			if _, err := inspector.Exec(ctx, "UPDATE photo.operation_registry SET v2_enabled=true WHERE operation='mask.face_skin' AND version='1'"); err != nil {
				t.Fatal(err)
			}
			var savedSkinCursor int64
			if err := inspector.QueryRow(ctx, "SELECT last_cursor FROM photo.tenant_event_cursors WHERE tenant_id=$1", f.Tenant).Scan(&savedSkinCursor); err != nil {
				t.Fatal(err)
			}
			if _, err := inspector.Exec(ctx, "UPDATE photo.tenant_event_cursors SET last_cursor=9007199254740991 WHERE tenant_id=$1", f.Tenant); err != nil {
				t.Fatal(err)
			}
			failedSkinTicket, failedSkinErr := reader.resumeNativeSkinPreparation(ctx, scheduler, who, skinGraph)
			if _, err := inspector.Exec(ctx, "UPDATE photo.tenant_event_cursors SET last_cursor=$2 WHERE tenant_id=$1", f.Tenant, savedSkinCursor); err != nil {
				t.Fatal(err)
			}
			var skinCursorErr *pgconn.PgError
			if failedSkinTicket != (v2input.JobTicket{}) || !errors.As(failedSkinErr, &skinCursorErr) || skinCursorErr.Code != "23514" {
				t.Fatal("skin seal cursor failure exposed ticket", failedSkinErr)
			}
			if err := inspector.QueryRow(ctx, "SELECT count(*) FROM photo.runs r JOIN photo.tasks t ON t.run_id=r.id AND t.tenant_id=r.tenant_id WHERE r.id=$1 AND r.status='draft' AND NOT r.graph_sealed AND t.state='blocked' AND t.attempt=0", skinGraph.shell.Run).Scan(&skinBlocked); err != nil || skinBlocked != 1 {
				t.Fatal("failed skin seal did not roll back graph/task", err)
			}
			for i := 0; i < 2; i++ {
				ticket, err := reader.resumeNativeSkinPreparation(ctx, scheduler, who, skinGraph)
				if err != nil || ticket.RunID != skinGraph.shell.Run || ticket.WorkflowID != skinGraph.shell.WorkflowID || ticket.State != "queued" {
					t.Fatal("skin first seal/receipt recovery", err)
				}
			}
			skinBootstrap := bootstrap
			skinBootstrap.Transport = v2input.ScopedPublisher{Documents: transportDocs, Tenant: f.Tenant, Run: skinGraph.shell.Run}
			for replay := 0; replay < 2; replay++ {
				ensuredSkin, ensureErr := skinBootstrap.ensureSkin(ctx, who, graph, color, budget)
				if ensureErr != nil || ensuredSkin.plan.seal != skinGraph.seal || ensuredSkin.ticket.RunID != skinGraph.shell.Run || ensuredSkin.ticket.WorkflowID != skinGraph.shell.WorkflowID || ensuredSkin.ticket.State != "queued" {
					t.Fatal("original admitted skin ensure replay", replay, ensureErr)
				}
			}
			var skinEvents, skinOutbox, skinReceipts int
			if err := inspector.QueryRow(ctx, "SELECT count(*) FROM photo.run_events WHERE run_id=$1 AND type='v2.run.queued'", skinGraph.shell.Run).Scan(&skinEvents); err != nil {
				t.Fatal(err)
			}
			if err := inspector.QueryRow(ctx, "SELECT count(*) FROM photo.outbox WHERE payload->>'run_id'=$1 AND routing_key='v2.run.queued'", skinGraph.shell.Run.String()).Scan(&skinOutbox); err != nil {
				t.Fatal(err)
			}
			if err := inspector.QueryRow(ctx, "SELECT count(*) FROM photo.idempotency_records WHERE resource_id=$1 AND route='V2.Admit' AND response_status=202", skinGraph.shell.Run).Scan(&skinReceipts); err != nil {
				t.Fatal(err)
			}
			if skinEvents != 1 || skinOutbox != 1 || skinReceipts != 1 {
				t.Fatal("skin seal replay duplicated event/outbox/receipt")
			}
			// Synthetic no_faces output, with actual immutable NPY/PNG/MaskRef
			// bytes. No inference or scientific quality acceptance is implied.
			if _, err := inspector.Exec(ctx, "UPDATE photo.runs SET status='running' WHERE id=$1", skinGraph.shell.Run); err != nil {
				t.Fatal(err)
			}
			if _, err := inspector.Exec(ctx, "UPDATE photo.tasks SET attempt=1,fencing_token=1 WHERE id=$1", skinDraft.ID); err != nil {
				t.Fatal(err)
			}
			skinWorker := uuid.New()
			if _, err := inspector.Exec(ctx, "INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES($1,$2,1,1,$3,clock_timestamp(),clock_timestamp(),'cpu','{\"backend\":\"cpu\"}')", f.Tenant, skinDraft.ID, skinWorker); err != nil {
				t.Fatal(err)
			}
			canvas := verified.labels.Canvas
			header := fmt.Sprintf("{'descr': '<f4', 'fortran_order': False, 'shape': (%d, %d), }", canvas.Height, canvas.Width)
			header += strings.Repeat(" ", (64-(10+len(header)+1)%64)%64) + "\n"
			npy := []byte{0x93, 'N', 'U', 'M', 'P', 'Y', 1, 0, 0, 0}
			binary.LittleEndian.PutUint16(npy[8:], uint16(len(header)))
			npy = append(npy, []byte(header)...)
			npy = append(npy, make([]byte, canvas.Width*canvas.Height*4)...)
			var png bytes.Buffer
			png.Write([]byte{137, 80, 78, 71, 13, 10, 26, 10})
			chunk := func(kind string, body []byte) {
				binary.Write(&png, binary.BigEndian, uint32(len(body)))
				png.WriteString(kind)
				png.Write(body)
				binary.Write(&png, binary.BigEndian, crc32.ChecksumIEEE(append([]byte(kind), body...)))
			}
			ihdr := make([]byte, 13)
			binary.BigEndian.PutUint32(ihdr[:4], uint32(canvas.Width))
			binary.BigEndian.PutUint32(ihdr[4:8], uint32(canvas.Height))
			ihdr[8] = 16
			chunk("IHDR", ihdr)
			var compressed bytes.Buffer
			encoder := zlib.NewWriter(&compressed)
			if _, err := encoder.Write(make([]byte, canvas.Height*(1+canvas.Width*2))); err != nil {
				t.Fatal(err)
			}
			if err := encoder.Close(); err != nil {
				t.Fatal(err)
			}
			chunk("IDAT", compressed.Bytes())
			chunk("IEND", nil)
			var skinOutputs []v2input.Ref
			for i, role := range []string{"skin_mask", "skin_mask_png", "skin_mask_ref"} {
				media := []string{"application/octet-stream", "image/png", "application/json"}[i]
				body := npy
				if i == 1 {
					body = png.Bytes()
				}
				if i == 2 {
					body, err = canonical.Marshal(review.ProtectionMask{Artifact: review.ArtifactRef{ID: skinOutputs[0].ArtifactID, SHA256: skinOutputs[0].SHA256}, Canvas: canvas, TransformChain: verified.labels.TransformChain, Selector: "skin", Confidence: 0, ModelLockID: &verified.labels.ModelLockID})
					if err != nil {
						t.Fatal(err)
					}
				}
				hash := sha256.Sum256(body)
				digest := fmt.Sprintf("%x", hash)
				id := uuid.New()
				object, err := objects.PutImmutable(ctx, "native-skin-result/"+id.String(), bytes.NewReader(body), int64(len(body)), digest, media)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := inspector.Exec(ctx, "INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES($1,$2,$3,$4,$5,$6,$7,$8,clock_timestamp(),$9,$10,1,1)", id, f.Tenant, f.Objects.Bucket, object.Key, digest, len(body), media, object.VersionID, role, skinDraft.ID); err != nil {
					t.Fatal(err)
				}
				if _, err := inspector.Exec(ctx, "INSERT INTO photo.output_uploads(tenant_id,task_id,attempt,fence,worker_id,certificate_sha256,idempotency_key,artifact_id,role,bucket,object_key,sha256,bytes,media_type,state,object_version,completed_at) VALUES($1,$2,1,1,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'ready',$13,clock_timestamp())", f.Tenant, skinDraft.ID, skinWorker, strings.Repeat("a", 64), uuid.New(), id, role, f.Objects.Bucket, object.Key, digest, len(body), media, object.VersionID); err != nil {
					t.Fatal(err)
				}
				skinOutputs = append(skinOutputs, v2input.Ref{ArtifactID: id, SHA256: digest})
			}
			rawSkin, err := canonical.Marshal(skinOutputs)
			if err != nil {
				t.Fatal(err)
			}
			var skinResultID uuid.UUID
			if err := inspector.QueryRow(ctx, "INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES($1,$2,1,1,$3::jsonb,'{}',$4) RETURNING id", f.Tenant, skinDraft.ID, string(rawSkin), strings.Repeat("7", 64)).Scan(&skinResultID); err != nil {
				t.Fatal(err)
			}
			if _, err := inspector.Exec(ctx, "UPDATE photo.tasks SET state='succeeded',canonical_result_id=$1 WHERE id=$2", skinResultID, skinDraft.ID); err != nil {
				t.Fatal(err)
			}
			completedSkin, err := reader.readCompletedNativeSkin(ctx, who, skinGraph, color)
			if err != nil || completedSkin.catalog.ResultID != skinResultID || completedSkin.skinPixels != 0 || completedSkin.mask.Confidence != 0 {
				t.Fatal("physical completed skin content", err)
			}
			savedPublished, err := reader.readNativeInputSavedPublishedIntent(ctx, who, request, 16<<20)
			if err != nil || !reflect.DeepEqual(savedPublished, published) {
				t.Fatal("original intent/inventory loading without publication", err)
			}
			originalCatalog, err := (SQLNativeParentArtifactCatalogFactory{API: reader.DB, Bucket: reader.Bucket}).ForReference(ctx, who, request.TenantID, reader.ProjectID, request.Intent.Reference.AssetVersionID)
			if err != nil {
				t.Fatal("original reference catalog factory", err)
			}
			if denied, err := (SQLNativeParentArtifactCatalogFactory{API: scheduler, Bucket: reader.Bucket}).ForReference(ctx, who, request.TenantID, reader.ProjectID, request.Intent.Reference.AssetVersionID); err == nil || denied != nil {
				t.Fatal("scheduler role accepted as source catalog")
			}
			restoredProducers, err := reader.restoreNativeInputFaceProducers(ctx, originalCatalog, who, request, key, published, snapshot, snapshotBody, color)
			if err != nil || restoredProducers.detector.seal != detector.seal || restoredProducers.landmarks.seal != retainedLandmarks.seal || restoredProducers.parser.seal != graph.seal || restoredProducers.skin.seal != skinGraph.seal {
				t.Fatal("original native producer reconstruction without publication", err)
			}
			for _, operation := range []string{"face.landmarks", "face.parse", "mask.face_skin"} {
				presence, err := reader.readNativeStagePresence(ctx, who, detector, operation)
				if err != nil || presence != nativeStagePresent {
					t.Fatal("original child presence", operation, err)
				}
			}
			initialBootstrap := bootstrap
			initialBootstrap.Catalog = originalCatalog
			initialTicket, err := nativeTicketWithRetry(ctx, scheduler.Begin, func(tx pgx.Tx) (v2input.JobTicket, error) {
				return reader.sealAndEnableInitialDetectorInTransaction(ctx, tx, who, detector, detectorTransport)
			})
			if err != nil || initialTicket.RunID != detectorRun || initialTicket.State != "queued" {
				t.Fatal("atomic initial detector scheduling", err)
			}
			var initialTenant, claimedDetector, initialParent, initialActor, initialSession, initialToken uuid.UUID
			var initialEpoch int64
			if err := scheduler.QueryRow(ctx, "SELECT tenant_id,detector_run_id,parent_run_id,actor_id,session_id,auth_epoch,lease_token FROM photo.claim_v27_native_initial_pipeline($1,30)", reader.ProjectID).Scan(&initialTenant, &claimedDetector, &initialParent, &initialActor, &initialSession, &initialEpoch, &initialToken); err != nil || initialTenant != f.Tenant || claimedDetector != detectorRun || initialParent != request.RunID || initialActor != who.UserID || initialSession != who.SessionID || initialToken == uuid.Nil {
				t.Fatal("original initial pipeline lease", err)
			}
			var bypass bool
			if err := scheduler.QueryRow(ctx, "SELECT has_function_privilege(current_user,'photo.record_v27_native_initial_pipeline(uuid,uuid,uuid,text)','EXECUTE')").Scan(&bypass); err != nil || bypass {
				t.Fatal("scheduler can bypass original manual handoff", err)
			}
			var recorded bool
			if err := scheduler.QueryRow(ctx, "SELECT photo.record_v27_native_initial_pipeline_handoff($1,$2,$3,'completed')", f.Tenant, detectorRun, uuid.New()).Scan(&recorded); err != nil || recorded {
				t.Fatal("stale initial lease recorded completion", err)
			}
			if err := scheduler.QueryRow(ctx, "SELECT photo.record_v27_native_initial_pipeline_handoff($1,$2,$3,'completed')", f.Tenant, detectorRun, initialToken).Scan(&recorded); err != nil || recorded {
				t.Fatal("unfinished original pipeline recorded completion", err)
			}
			if err := scheduler.QueryRow(ctx, "SELECT photo.record_v27_native_initial_pipeline_handoff($1,$2,$3,'failed')", f.Tenant, detectorRun, initialToken).Scan(&recorded); err != nil || recorded {
				t.Fatal("healthy original pipeline recorded failure", err)
			}
			presence, err := reader.readNativeManualSelectionPresence(ctx, who, detector)
			if err != nil || presence != nativeStageAbsent {
				t.Fatal("authenticated absent manual selection", err)
			}
			foreignWho := who
			foreignWho.SessionID = uuid.New()
			if _, err := reader.readNativeManualSelectionPresence(ctx, foreignWho, detector); err == nil {
				t.Fatal("foreign session received selection absence authority")
			}
			if terminal, err := reader.readNativeManualChildTerminal(ctx, who, detector); err == nil || terminal {
				t.Fatal("absent manual selection granted terminal authority", err)
			}
			if terminal, err := reader.readNativeManualChildTerminal(ctx, foreignWho, detector); err == nil || terminal {
				t.Fatal("foreign session granted terminal authority", err)
			}
			// Check codec identity without inventing terminal child evidence.
			// Terminal late-selection refusal requires a separate failed-child fixture.
			latePixels := []v2input.Ref{detector.request.Intent.Reference.Source}
			lateDiameter := [2]float64{.002, .012}
			lateBody, lateSHA, err := compileNativeManualSelection(ctx, who, detector, latePixels, lateDiameter)
			if err != nil {
				t.Fatal("compile scoped late selection", err)
			}
			decodedLate, err := decodeNativeManualSelection(ctx, lateBody, lateSHA, who, detector)
			if err != nil || decodedLate.Parent != request.RunID || decodedLate.Diameter != lateDiameter {
				t.Fatal("original checkpoint codec round trip", err)
			}
			presence, err = reader.readNativeManualSelectionPresence(ctx, who, detector)
			if err != nil || presence != nativeStageAbsent {
				t.Fatal("codec inspection changed selection state", err)
			}
			initialCapacity := TransferPreparation{Color: color, RAM: budget.RAM, VRAM: budget.VRAM, Scratch: budget.Scratch, Output: budget.Output, MetadataBytes: 16 << 20}
			initialProcessor := SQLNativeInitialPipelineRecovery{Preparer: TransferDraftPreparer{Reader: reader, Scheduler: scheduler, Documents: documents, TransportDocuments: transportDocs, Configuration: initialCapacity, ReservedBytes: budget.Reserved}, CatalogForReference: (SQLNativeParentArtifactCatalogFactory{API: api, Bucket: reader.Bucket}).ForReference, Timeout: time.Minute}
			processedInitial, lockedInitial, err := initialProcessor.Recover(ctx, who, f.Tenant, request.RunID)
			if err != nil || !lockedInitial || processedInitial.phase != nativePreparationWaiting || processedInitial.operation != "mask.person" {
				t.Fatal("production initial pipeline locked recovery", err, "locked", lockedInitial, "phase", processedInitial.phase, "operation", processedInitial.operation)
			}
			if outcome, err := nativeInitialPipelineRecoveryOutcome(request.RunID, processedInitial, nil); err != nil || outcome != "waiting" {
				t.Fatal("production initial pipeline pending person classification", err)
			}
			selectedDetector, err := reader.selectInitialDetectorConfiguration(ctx, who, f.Tenant, initialCapacity, budget.Reserved)
			if err != nil || selectedDetector.Report != f.FaceQualifications[1].Report {
				t.Fatal("automatic original detector qualification selection", err)
			}
			selectedInitial, err := reader.selectInitialFacePipelineConfiguration(ctx, who, f.Tenant, initialCapacity, budget.Reserved, transportDocs)
			if err != nil || selectedInitial.Landmarks.Report != f.FaceQualifications[0].Report || selectedInitial.Parser.Report != f.ParseQualification.Report {
				t.Fatal("automatic original face qualification selection", err)
			}
			qualifiedInitial, err := initialBootstrap.advanceQualifiedInitialFacePipeline(ctx, who, anchor, initialCapacity, budget.Reserved, transportDocs)
			if err != nil || qualifiedInitial.phase != nativePreparationSucceeded || qualifiedInitial.producers.landmarks.seal != retainedLandmarks.seal || qualifiedInitial.producers.parser.seal != graph.seal || qualifiedInitial.producers.skin.seal != skinGraph.seal {
				t.Fatal("automatic qualified initial pipeline exact replay", err)
			}
			initial, err := initialBootstrap.advanceInitialFacePipeline(ctx, who, anchor, nativeFacePipelineStartConfiguration{
				Landmarks: nativeFacePipelineStageConfiguration{Report: f.FaceQualifications[0].Report, Budget: budget},
				Parser:    nativeFacePipelineStageConfiguration{Report: f.ParseQualification.Report, Budget: budget},
				Skin:      budget, TransportDocuments: transportDocs,
			})
			if err != nil || initial.phase != nativePreparationSucceeded || initial.operation != "" || initial.producers.detector.seal != detector.seal || initial.producers.landmarks.seal != retainedLandmarks.seal || initial.producers.parser.seal != graph.seal || initial.producers.skin.seal != skinGraph.seal {
				t.Fatal("initial pipeline exact saved stage replay", err)
			}
			advanced, err := reader.advanceSavedNativeFacePipeline(ctx, scheduler, originalCatalog, who, anchor)
			if err != nil || advanced.phase != nativePreparationSucceeded || advanced.operation != "" || advanced.producers.detector.seal != detector.seal || advanced.producers.landmarks.seal != retainedLandmarks.seal || advanced.producers.parser.seal != graph.seal || advanced.producers.skin.seal != skinGraph.seal {
				t.Fatal("complete original face pipeline advancement", err)
			}
			recoveredRequest, recoveredKey, err := reader.restoreNativeInputSavedRequest(ctx, who, request, published.Ref, snapshot)
			if err != nil || recoveredKey != key || !reflect.DeepEqual(recoveredRequest, request) {
				t.Fatal("saved original request identity recovery", err)
			}
			recoveredAnchor, err := reader.loadNativePreparationAnchor(ctx, who, request.TenantID, request.RunID, color, 16<<20)
			if err != nil || recoveredAnchor.Key != key || !reflect.DeepEqual(recoveredAnchor.Request, request) || !reflect.DeepEqual(recoveredAnchor.Published, published) || recoveredAnchor.Snapshot != snapshot || !bytes.Equal(recoveredAnchor.SnapshotBody, snapshotBody) || recoveredAnchor.Color != color {
				t.Fatal("original preparation anchor recovery without in-memory request", err)
			}
			if denied, err := reader.loadNativePreparationAnchor(ctx, who, request.TenantID, request.RunID, color, 1); err == nil || denied.Request.RunID != uuid.Nil {
				t.Fatal("anchor recovery exceeded metadata budget")
			}
			restoredTransports, err := recoverNativeInputFaceProducerTransports(ctx, scheduler, who, restoredProducers)
			if err != nil || !reflect.DeepEqual(restoredTransports.detector, detectorTransport) || !reflect.DeepEqual(restoredTransports.landmarks, retainedLandmarkTransport) || !reflect.DeepEqual(restoredTransports.parser, physicalTransport.Graph.Built) || !reflect.DeepEqual(restoredTransports.skin, coordinatedSkin.Graph.Built) {
				t.Fatal("original native producer transport reconstruction", err)
			}
			observedSkin, err := reader.readNativeInputSavedStageState(ctx, who, request, published.Ref, snapshot, "mask.face_skin")
			if err != nil || observedSkin.task != skinDraft.ID || observedSkin.taskState != "succeeded" || !observedSkin.sealed {
				t.Fatal("original skin state observation", err)
			}
			if phase, err := nativeInputStagePreparationPhase(observedSkin); err != nil || phase != nativePreparationSucceeded {
				t.Fatal("original skin completion observation", phase, err)
			}
			if err := tenant.WithTenant(ctx, reader.DB, who, request.TenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
				var count int
				if err := tx.QueryRow(ctx, "SELECT count(*) FROM photo.semantic_native_input_saved_stage_state($1,$2,$3,$4,$5,$6,$7,$8)", request.TenantID, reader.ProjectID, who.UserID, who.SessionID, request.RunID, skinGraph.shell.Run, "mask.face_skin", strings.Repeat("0", 64)).Scan(&count); err != nil {
					return err
				}
				if count != 0 {
					return fmt.Errorf("state observer accepted a different config hash")
				}
				return nil
			}); err != nil {
				t.Fatal("state observation exact config binding", err)
			}
			nativeDefect, err := reader.prepareNativeDefectFaceParams(ctx, who, retainedLandmarks, skinGraph, color, [2]float64{.002, .012}, nil, nil, nil)
			if err != nil || nativeDefect.params.params == "" || nativeDefect.landmarks.catalog.ResultID == uuid.Nil || nativeDefect.skin.catalog.ResultID != skinResultID {
				t.Fatal("native no_faces defect Params preparation", err)
			}
			publishedNativeDefect, err := reader.publishNativeDefectVersionParams(ctx, who, nativeDefect, color, documents)
			if err != nil || publishedNativeDefect.Ref.SHA256 != nativeDefect.params.digest {
				t.Fatal("native defect Params publication", err)
			}
			replayNativeDefect, err := reader.publishNativeDefectVersionParams(ctx, who, nativeDefect, color, documents)
			if err != nil || replayNativeDefect.Ref != publishedNativeDefect.Ref {
				t.Fatal("native defect Params replay", err)
			}
			nativeParamsObject, err := reader.defectVersionParamsObject(ctx, who, f.Tenant, request.Intent.Reference, uuid.NewSHA1(request.RunID, []byte("semantic.defect.params:reference")), publishedNativeDefect.Ref)
			if err != nil {
				t.Fatal("native defect Params object", err)
			}
			inventory, sizes, err := compileNativeDefectInventory(ctx, nativeDefect, defectEvidenceObject{ref: publishedNativeDefect.Ref, object: nativeParamsObject}, nil)
			if err != nil || len(inventory) != len(sizes) || len(inventory) < 3 {
				t.Fatal("native defect exact inventory", err)
			}

			dependencies, err := reader.prepareNativeDefectDependencies(ctx, who, nativeDefect, color, retainedLandmarkTransport, coordinatedSkin.Graph.Built, physicalTransport.Graph.Built, detectorTransport)
			if err != nil {
				t.Fatal("native defect completed dependencies", err)
			}
			if err := database.InTransaction(ctx, scheduler, func(tx pgx.Tx) error {
				return reader.lockNativeDefectDependenciesInTransaction(ctx, tx, who, dependencies)
			}); err != nil {
				t.Fatal("combined native FAN/skin dependency lock", err)
			}

			packet, err := reader.marshalNativeSkinCompletedEvidence(ctx, who, skinGraph, color, completedSkin, coordinatedSkin.Graph.Built, physicalTransport.Graph.Built, detectorTransport)
			if err != nil {
				t.Fatal("completed skin evidence", err)
			}
			if err := database.InTransaction(ctx, scheduler, func(tx pgx.Tx) error {
				return reader.lockNativeCompletedSkinInTransaction(ctx, tx, who, skinGraph, packet)
			}); err != nil {
				t.Fatal("completed skin SQL lock", err)
			}
			for _, mutation := range []string{"extra", "result", "params", "file-version", "file-bytes", "outputs", "negative-count", "foreign-session"} {
				var altered map[string]any
				if err := json.Unmarshal(packet, &altered); err != nil {
					t.Fatal(err)
				}
				switch mutation {
				case "extra":
					altered["unexpected"] = true
				case "result":
					altered["result_id"] = uuid.New().String()
				case "params":
					altered["params"].(map[string]any)["artifact_id"] = uuid.New().String()
				case "file-version":
					altered["files"].([]any)[0].(map[string]any)["version"] = "foreign"
				case "file-bytes":
					altered["files"].([]any)[0].(map[string]any)["bytes"] = 1
				case "outputs":
					altered["outputs"] = altered["outputs"].([]any)[:2]
				case "negative-count":
					altered["skin_pixels"] = -1
				case "foreign-session":
					altered["admission"].(map[string]any)["parser"].(map[string]any)["face"].(map[string]any)["session_id"] = uuid.New().String()
				}
				changed, err := json.Marshal(altered)
				if err != nil {
					t.Fatal(err)
				}
				if err := database.InTransaction(ctx, scheduler, func(tx pgx.Tx) error {
					return reader.lockNativeCompletedSkinInTransaction(ctx, tx, who, skinGraph, changed)
				}); !errors.Is(err, v2input.ErrConflict) {
					t.Fatal("malformed completed skin evidence accepted", mutation, err)
				}
			}
			skinLock, err := scheduler.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err := reader.lockNativeCompletedSkinInTransaction(ctx, skinLock, who, skinGraph, packet); err != nil {
				skinLock.Rollback(ctx)
				t.Fatal(err)
			}
			for _, change := range []struct {
				query string
				id    uuid.UUID
			}{
				{"UPDATE photo.tasks SET cancel_requested=true WHERE id=$1", skinDraft.ID},
				{"UPDATE photo.output_uploads SET completed_at=NULL WHERE artifact_id=$1", skinOutputs[0].ArtifactID},
				{"UPDATE photo.artifacts SET object_version='foreign' WHERE id=$1", skinOutputs[1].ArtifactID},
				{"UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", f.Session},
			} {
				writer, err := inspector.Begin(ctx)
				if err != nil {
					skinLock.Rollback(ctx)
					t.Fatal(err)
				}
				if _, err := writer.Exec(ctx, "SET LOCAL lock_timeout='250ms'"); err != nil {
					writer.Rollback(ctx)
					skinLock.Rollback(ctx)
					t.Fatal(err)
				}
				_, writeErr := writer.Exec(ctx, change.query, change.id)
				writer.Rollback(ctx)
				var blocked *pgconn.PgError
				if !errors.As(writeErr, &blocked) || blocked.Code != "55P03" {
					skinLock.Rollback(ctx)
					t.Fatal("completed skin dependency failed to block writer", writeErr)
				}
			}
			if err := skinLock.Rollback(ctx); err != nil {
				t.Fatal(err)
			}

			for _, ref := range skinOutputs {
				if _, err := inspector.Exec(ctx, "UPDATE photo.output_uploads SET completed_at=NULL WHERE artifact_id=$1", ref.ArtifactID); err != nil {
					t.Fatal(err)
				}
				denied, deniedErr := reader.readCompletedNativeSkin(ctx, who, skinGraph, color)
				if _, err := inspector.Exec(ctx, "UPDATE photo.output_uploads SET completed_at=clock_timestamp() WHERE artifact_id=$1", ref.ArtifactID); err != nil {
					t.Fatal(err)
				}
				if deniedErr == nil || denied.catalog.ResultID != uuid.Nil || denied.body != "" {
					t.Fatal("incomplete skin output exposed content", deniedErr)
				}
			}

		}
		if operation == "face.landmarks" {
			verified, err := reader.readCompletedNativeLandmarks(ctx, who, graph, color)
			if err != nil || verified.catalog.ResultID != faceResultID || len(verified.faces) != 0 || verified.body == "" || verified.paramsBody == "" || verified.detector.catalog != completed.catalog {
				t.Fatal("byte-verified completed native FAN", err)
			}
			retainedLandmarks = graph
			retainedLandmarkTransport = physicalTransport.Graph.Built
			packet, err := reader.marshalNativeLandmarkCompletedEvidence(ctx, who, graph, color, verified, physicalTransport.Graph.Built, detectorTransport)
			if err != nil {
				t.Fatal("completed FAN evidence", err)
			}
			if err := database.InTransaction(ctx, scheduler, func(tx pgx.Tx) error {
				return reader.lockNativeCompletedLandmarksInTransaction(ctx, tx, who, graph, packet)
			}); err != nil {
				t.Fatal("completed FAN dependency lock", err)
			}

		}
		for _, ref := range faceOutputs {
			if _, err := inspector.Exec(ctx, "UPDATE photo.output_uploads SET completed_at=NULL WHERE artifact_id=$1", ref.ArtifactID); err != nil {
				t.Fatal(err)
			}
			denied, lookupErr := reader.nativeFaceResultCatalog(ctx, who, graph)
			var rejectedContent completedNativeLandmarks
			var contentErr error
			if operation == "face.landmarks" {
				rejectedContent, contentErr = reader.readCompletedNativeLandmarks(ctx, who, graph, color)
			}
			if operation == "face.parse" {
				rejectedSkin, skinErr := reader.publishNativeSkinStageParams(ctx, who, graph, color, documents)
				if skinErr == nil || rejectedSkin.document != uuid.Nil || rejectedSkin.ref.ArtifactID != uuid.Nil || rejectedSkin.parser.catalog.ResultID != uuid.Nil {
					t.Fatal("incomplete parser upload exposed skin Params evidence", skinErr)
				}
			}

			if _, err := inspector.Exec(ctx, "UPDATE photo.output_uploads SET completed_at=clock_timestamp() WHERE artifact_id=$1", ref.ArtifactID); err != nil {
				t.Fatal(err)
			}
			if operation == "face.landmarks" && (contentErr == nil || rejectedContent.catalog.ResultID != uuid.Nil || rejectedContent.body != "" || rejectedContent.faces != nil) {
				t.Fatal("incomplete FAN upload exposed content proof", contentErr)
			}
			if !errors.Is(lookupErr, v2input.ErrConflict) || denied.ResultID != uuid.Nil || denied.Outputs != nil {
				t.Fatal("incomplete face upload exposed completed result", operation, lookupErr)
			}
		}

	}
	t.Log("V27_NATIVE_DOWNSTREAM_PARAMS_PUBLICATION_OK")

	if err := database.InTransaction(ctx, scheduler, func(tx pgx.Tx) error {
		return reader.lockCompletedDetectorInTransaction(ctx, tx, who, detector, completed)
	}); err != nil {
		t.Fatal("physical retained SFD result lock", err)
	}
	changed := completed
	changed.objects[0].object.VersionID = "foreign-version"
	if err := database.InTransaction(ctx, scheduler, func(tx pgx.Tx) error {
		return reader.lockCompletedDetectorInTransaction(ctx, tx, who, detector, changed)
	}); err == nil {
		t.Fatal("substituted detector object pin accepted")
	}

	lockTx, err := scheduler.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lockTx.Rollback(context.Background())
	if err := reader.lockCompletedDetectorInTransaction(ctx, lockTx, who, detector, completed); err != nil {
		t.Fatal("retained detector lock", err)
	}
	for _, change := range []struct {
		query string
		id    uuid.UUID
	}{
		{"UPDATE photo.tasks SET cancel_requested=true WHERE id=$1", detectorTask.ID},
		{"UPDATE photo.output_uploads SET completed_at=NULL WHERE artifact_id=$1", outputID},
		{"UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", f.Session},
	} {
		writer, err := inspector.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Exec(ctx, "SET LOCAL lock_timeout='250ms'"); err != nil {
			writer.Rollback(ctx)
			t.Fatal(err)
		}
		_, writeErr := writer.Exec(ctx, change.query, change.id)
		writer.Rollback(ctx)
		var postgres *pgconn.PgError
		if !errors.As(writeErr, &postgres) || postgres.Code != "55P03" {
			t.Fatal("retained detector authority update was not blocked")
		}
	}
	if err := lockTx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	t.Log("V27_NATIVE_DETECTOR_CONCURRENT_WRITERS_BLOCKED_OK")
	t.Log("V27_NATIVE_DETECTOR_TRANSACTION_LOCK_OK")
	t.Log("V27_NATIVE_DETECTOR_CONTENT_HANDOFF_OK")
	parserLockTx, err := scheduler.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer parserLockTx.Rollback(context.Background())
	if err := reader.lockNativeSkinDependencyInTransaction(ctx, parserLockTx, who, retainedSkin); err != nil {
		t.Fatal("hold parser dependency lock", err)
	}
	for _, change := range []struct {
		query string
		id    uuid.UUID
	}{
		{"UPDATE photo.tasks SET cancel_requested=true WHERE id=$1", retainedSkin.parser.shell.Tasks[0].ID},
		{"UPDATE photo.output_uploads SET completed_at=NULL WHERE artifact_id=$1", retainedSkin.prepared.labels.ref.ArtifactID},
		{"UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", f.Session},
	} {
		writer, err := inspector.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Exec(ctx, "SET LOCAL lock_timeout='250ms'"); err != nil {
			writer.Rollback(ctx)
			t.Fatal(err)
		}
		_, writeErr := writer.Exec(ctx, change.query, change.id)
		writer.Rollback(ctx)
		var postgres *pgconn.PgError
		if !errors.As(writeErr, &postgres) || postgres.Code != "55P03" {
			t.Fatal("retained parser authority update was not blocked", writeErr)
		}
	}
	if err := parserLockTx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	t.Log("V27_NATIVE_SKIN_DEPENDENCY_CONCURRENT_WRITERS_BLOCKED_OK")
	if _, err := inspector.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", f.Session); err != nil {
		t.Fatal("session revocation", err)
	}
	if retainedSkin.seal == "" {
		t.Fatal("missing retained skin plan for revocation checks")
	}
	deniedSkinTicket, deniedSkinErr := reader.resumeNativeSkinPreparation(ctx, scheduler, who, retainedSkin)
	if deniedSkinErr == nil || deniedSkinTicket != (v2input.JobTicket{}) {
		t.Fatal("revoked skin receipt recovery exposed ticket", deniedSkinErr)
	}
	if err := database.InTransaction(ctx, scheduler, func(tx pgx.Tx) error { return reader.lockNativeSkinDependencyInTransaction(ctx, tx, who, retainedSkin) }); err == nil {
		t.Fatal("revoked skin dependency accepted")
	}
	if err := database.InTransaction(ctx, scheduler, func(tx pgx.Tx) error {
		return reader.registerNativeSkinInputsInTransaction(ctx, tx, who, retainedSkin, color, retainedSkinTransport)
	}); err == nil {
		t.Fatal("revoked skin registration replay accepted")
	}
	revokedSkin, skinErr := reader.persistNativeSkinPreparationInputs(ctx, scheduler, v2input.ScopedPublisher{Documents: transportDocs, Tenant: f.Tenant, Run: retainedSkin.shell.Run}, who, retainedSkin, color)
	if skinErr == nil || revokedSkin.Graph.ID != uuid.Nil || revokedSkin.Graph.Built.Manifest.ArtifactID != uuid.Nil {
		t.Fatal("revoked skin coordinator exposed transport", skinErr)
	}
	refused, err := reader.resumeDetectorPreparation(ctx, scheduler, who, detector)
	if err == nil || refused != (v2input.JobTicket{}) {
		t.Fatal("revoked detector replay accepted")
	}
	if denied, err := restartedReader.loadPersonPreparationAnchor(ctx, who, f.Tenant, request.RunID, color, 16<<20); err == nil || denied.Request.RunID != uuid.Nil {
		t.Fatal("revoked person anchor exposed request", err)
	}
	t.Log("V27_PERSON_SAVED_ANCHOR_REVOKED_OK")
	if worked, err := createPersonPreparer.prepareMissingCreatePerson(ctx, who, request, key, published); err == nil || worked {
		t.Fatal("revoked public person preparation returned success", err)
	}
	deniedPerson, deniedPersonErr := restartedReader.restorePersonDraftPlan(ctx, restartedCatalog, who, anchor)
	if deniedPersonErr == nil || deniedPerson.seal != "" {
		t.Fatal("revoked saved person reconstruction exposed plan", deniedPersonErr)
	}
	t.Log("V27_PERSON_SAVED_PLAN_REVOKED_OK")
	refused, err = reader.resumePersonPreparation(ctx, scheduler, who, person)
	if err == nil || refused != (v2input.JobTicket{}) {
		t.Fatal("revoked person replay accepted")
	}
	for i, graph := range downstreamPlans {
		revokedRecovery, err := reader.resumeNativeFacePreparation(ctx, scheduler, who, graph, completed)
		if err == nil || revokedRecovery != (v2input.JobTicket{}) {
			t.Fatal("revoked face recovery exposed ticket")
		}

		revokedTicket, err := reader.sealRegisteredNativeFacePreparation(ctx, scheduler, who, graph, completed, downstreamTransports[i], downstreamDetectorTransports[i])
		if err == nil || revokedTicket != (v2input.JobTicket{}) {
			t.Fatal("revoked face receipt replay accepted")
		}

		if err := database.InTransaction(ctx, scheduler, func(tx pgx.Tx) error {
			return reader.registerNativeFaceInputsInTransaction(ctx, tx, who, graph, color, completed, downstreamTransports[i])
		}); err == nil {
			t.Fatal("revoked downstream registration replay accepted")
		}
		refusedTask, err := reader.persistNativeFacePreparationInputs(ctx, scheduler, v2input.ScopedPublisher{Documents: transportDocs, Tenant: f.Tenant, Run: graph.shell.Run}, who, graph, color, completed)
		if err == nil || refusedTask.Graph.ID != uuid.Nil {
			t.Fatal("revoked downstream coordinator exposed usable transport")
		}
	}
	t.Log("V27_NATIVE_BOOTSTRAP_AND_REPLAY_OK")
}

// Fails the first person transport publication after its shell transaction.
// Other stages retain the actual PostgreSQL/object-store publisher.
type nativePersonFailTransport struct {
	Run      uuid.UUID
	Delegate v2input.DocumentPublisher
	Calls    int
}

func (p *nativePersonFailTransport) Publish(ctx context.Context, scope artifactjson.Scope, body []byte) (artifactjson.Ref, error) {
	if scope.Run == p.Run {
		p.Calls++
		return artifactjson.Ref{}, fmt.Errorf("injected person transport interruption")
	}
	return p.Delegate.Publish(ctx, scope, body)
}
