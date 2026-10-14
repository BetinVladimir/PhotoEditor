package v2draft

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
	"testing"
)

func TestCompileManualRasterVectorsOriginalBytes(t *testing.T) {
	read := func(path string) []byte {
		t.Helper()
		b, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		return b
	}
	marshal := func(v any) []byte {
		t.Helper()
		b, e := canonical.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		return b
	}
	ref := func(b []byte) v2input.Ref {
		s := sha256.Sum256(b)
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(s[:])}
	}
	var sources []map[string]any
	if e := json.Unmarshal(read("../review/testdata/working-images-python.json"), &sources); e != nil {
		t.Fatal(e)
	}
	sourceRaw := marshal(sources[0])
	sourceRef := ref(sourceRaw)
	var root map[string]any
	if e := json.Unmarshal(read("../review/testdata/face-manual-root-python.json"), &root); e != nil {
		t.Fatal(e)
	}
	root["image"] = sourceRef
	target := root["target"].(map[string]any)
	vectors := map[string]any{"version": "manual_mask_v1", "source_sha256": target["canvas"].(map[string]any)["source_sha256"], "canvas": target["canvas"], "shapes": []any{}}
	for _, external := range []bool{false, true} {
		params := map[string]any{"image": sourceRef, "object_id": target["object_id"], "vectors": nil, "inline_vectors": nil}
		var vectorsRaw []byte
		var expected v2input.Ref
		contract := "Params"
		if external {
			vectorsRaw = marshal(vectors)
			expected = ref(vectorsRaw)
			params["vectors"] = expected
			contract = "ManualMask"
		} else {
			params["inline_vectors"] = vectors
		}
		paramsRaw := marshal(params)
		paramsRef := ref(paramsRaw)
		if !external {
			expected = paramsRef
		}
		root["vectors"] = expected
		rootRaw := marshal(root)
		rootRef := ref(rootRaw)
		source, e := review.ParseWorkingImage(sourceRaw)
		if e != nil {
			t.Fatal("source", e)
		}
		parsed, e := review.ParseManualRasterDocument(rootRaw, review.ArtifactRef{ID: rootRef.ArtifactID, SHA256: rootRef.SHA256})
		if e != nil {
			t.Fatal("root", e)
		}
		_, e = review.VerifyManualMaskParams(paramsRaw, review.ArtifactRef{ID: paramsRef.ArtifactID, SHA256: paramsRef.SHA256}, rootRaw, review.ArtifactRef{ID: rootRef.ArtifactID, SHA256: rootRef.SHA256}, parsed.Target, source.Source(review.ArtifactRef{ID: sourceRef.ArtifactID, SHA256: sourceRef.SHA256}))
		if e != nil {
			t.Fatal("params", e, string(paramsRaw))
		}
		check := func(src, par, vec []byte, wantOK bool) {
			t.Helper()
			got, e := CompileManualRasterVectors(sourceRef, src, rootRef, rootRaw, paramsRef, par, vec)
			if wantOK {
				if e != nil || got.Ref != expected || got.Contract != contract {
					t.Fatalf("context=%+v error=%v", got, e)
				}
			} else if e == nil || got != (ManualRasterVectors{}) {
				t.Fatal("unbound original bytes accepted", got, e)
			}
		}
		check(sourceRaw, paramsRaw, vectorsRaw, true)
		contextValue, e := CompileManualRasterVectors(sourceRef, sourceRaw, rootRef, rootRaw, paramsRef, paramsRaw, vectorsRaw)
		if e != nil {
			t.Fatal(e)
		}
		fixture := &imageClosureCatalog{closureFixture: closureFixture{objects: map[uuid.UUID]objectstore.Object{}, bodies: map[string][]byte{}}}
		add := func(r v2input.Ref, b []byte, media string) {
			key := r.ArtifactID.String()
			fixture.objects[r.ArtifactID] = objectstore.Object{Key: key, SHA256: r.SHA256, Bytes: int64(len(b)), VersionID: "v1", ContentType: media}
			fixture.bodies[key] = b
		}
		add(sourceRef, sourceRaw, "application/json")
		add(rootRef, rootRaw, "application/json")
		add(paramsRef, paramsRaw, "application/json")
		if external {
			add(expected, vectorsRaw, "application/json")
		}
		add(v2input.Ref{ArtifactID: source.Pixels.ID, SHA256: source.Pixels.SHA256}, []byte("pixels"), "application/octet-stream")
		childRefs, e := manualRasterRootRefs(rootRaw, "mask", rootRef, &contextValue)
		if e != nil {
			t.Fatal(e)
		}
		for _, child := range childRefs[2:] {
			media := "application/octet-stream"
			if child.Contract == "Artifact" {
				media = "image/png"
			}
			add(child.Ref, []byte("mask"), media)
		}
		canvasHash, e := canonical.Hash(source.Canvas)
		if e != nil {
			t.Fatal(e)
		}
		binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: sourceRef, CanvasHash: canvasHash}
		roots := []LayerRef{{Path: "source", Contract: "WorkingImage", Ref: sourceRef, CanvasHash: canvasHash}, {Path: "params", Contract: "Params", Ref: paramsRef}, {Path: "mask", Contract: "RasterizedMask", Ref: rootRef, ManualVectors: &contextValue}}
		var inventory []v2input.Entry
		var documents []DocumentWitness
		var occurrences []OccurrenceWitness
		resolver := ClosureResolver{Catalog: fixture, Objects: fixture}
		if _, e = resolver.resolveRootsWithEvidence(context.Background(), auth.Identity{}, uuid.New(), roots, binding, &inventory, &documents, &occurrences); e != nil {
			t.Fatal("manual closure", e)
		}
		unique, e := uniqueTaskInputs(inventory)
		if e != nil || len(unique) != len(fixture.objects) {
			t.Fatal("incomplete original input inventory", unique, e)
		}
		targetRaw := marshal(parsed.Target)
		guardRaw := marshal(parsed.Protected)
		targetDoc := ref(targetRaw)
		guardDoc := ref(guardRaw)
		add(targetDoc, targetRaw, "application/json")
		add(guardDoc, guardRaw, "application/json")
		evidence, e := resolver.ResolveManualRasterTaskInputEvidence(context.Background(), auth.Identity{}, uuid.New(), binding, sourceRaw, rootRef, rootRaw, paramsRef, paramsRaw, vectorsRaw, targetDoc, guardDoc)
		if e != nil || len(evidence.Entries) != len(unique)+2 || len(evidence.Documents) == 0 || len(evidence.Occurrences) == 0 {
			t.Fatal("task evidence", e)
		}
		donorBinding := binding
		donorBinding.AssetVersionID = uuid.New()
		donorBinding.Source = ref(sourceRaw)
		donorParams := v2portrait.OpenEyesParams{SchemaVersion: 1, Base: binding, Donor: donorBinding, Strength: 1, SourceFaceID: uuid.New(), DonorFaceID: uuid.New(), EyesMask: targetDoc, Protected: guardDoc, Confirmation: ref([]byte("{}")), SourceFaces: ref([]byte("{}")), DonorFaces: ref([]byte("{}"))}
		image, e := review.ParseWorkingImage(sourceRaw)
		if e != nil {
			t.Fatal(e)
		}
		donorCapacity, e := v2portrait.DonorPortraitResources(image.Canvas.Width, image.Canvas.Height, image.Canvas.Width, image.Canvas.Height, 1, 1)
		if e != nil {
			t.Fatal(e)
		}
		donorBody, _, e := donorParams.Compile()
		if e != nil {
			t.Fatal(e)
		}
		donorPlan, e := v2portrait.CompileDonorTaskPlan(context.Background(), binding, donorBinding, sourceRaw, sourceRaw, donorBody, 1, 1, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", donorCapacity)
		if e != nil {
			t.Fatal(e)
		}
		physical := make([]DonorImageObject, len(donorPlan.PhysicalImageOccurrences))
		for i, occurrence := range donorPlan.PhysicalImageOccurrences {
			physical[i].Occurrence = occurrence
		}
		loadedDonor := LoadedDonorTaskPlan{Plan: donorPlan, Images: DonorImageEvidence{SourceRaw: sourceRaw, DonorRaw: sourceRaw, Physical: physical}, patchWidth: 1, patchHeight: 1, runtimeSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", capacity: donorCapacity}
		donorEvidence, e := resolver.ResolveLoadedDonorManualMaskEvidence(context.Background(), auth.Identity{}, uuid.New(), loadedDonor, rootRef, rootRaw, paramsRef, paramsRaw, vectorsRaw)
		if e != nil || len(donorEvidence.Entries) != len(evidence.Entries) {
			t.Fatal("donor manual closure", e)
		}
		checkPreparedDonorInputs(t, fixture.closureFixture, loadedDonor, donorParams, DonorManualProducer{RootRef: rootRef, RootRaw: rootRaw, ParamsRef: paramsRef, ParamsRaw: paramsRaw, VectorsRaw: vectorsRaw})
		aliased, e := resolver.ResolveLoadedDonorManualMaskEvidence(context.Background(), auth.Identity{}, uuid.New(), loadedDonor, donorParams.Confirmation, rootRaw, paramsRef, paramsRaw, vectorsRaw)
		if e == nil || aliased.Entries != nil {
			t.Fatal("aliased donor manual root")
		}
		for _, clothing := range []bool{false, true} {
			var localRaw []byte
			if clothing {
				localRaw, _, e = (v2portrait.ClothingParams{SchemaVersion: 1, Base: binding, Mask: targetDoc, Protected: guardDoc, Strength: 1, SaturationFactor: 1, WrinkleStrength: .5}).Compile()
			} else {
				localRaw, _, e = (v2portrait.HairParams{SchemaVersion: 1, Base: binding, Mask: targetDoc, Protected: guardDoc, Strength: 1, SaturationFactor: 1, Detail: .5}).Compile()
			}
			if e != nil {
				t.Fatal(e)
			}
			localRef := ref(localRaw)
			add(localRef, localRaw, "application/json")
			full, e := resolver.ResolveLocalPortraitTaskInputEvidence(context.Background(), auth.Identity{}, uuid.New(), binding, localRef, localRaw, sourceRaw, rootRef, rootRaw, paramsRef, paramsRaw, vectorsRaw, targetDoc, guardDoc)
			if e != nil || len(full.Entries) != len(unique)+3 {
				t.Fatal("local operation inventory", len(full.Entries), e)
			}
			wrong, e := resolver.ResolveLocalPortraitTaskInputEvidence(context.Background(), auth.Identity{}, uuid.New(), binding, localRef, localRaw, sourceRaw, rootRef, rootRaw, paramsRef, paramsRaw, vectorsRaw, guardDoc, targetDoc)
			if e == nil || len(wrong.Entries) != 0 {
				t.Fatal("local mask binding changed", e)
			}
			tenant := uuid.New()
			scope := artifactjson.Scope{Tenant: tenant, Run: uuid.New(), Owner: uuid.New(), Purpose: "task_params", AssemblyLease: uuid.New()}
			calls := 0
			revokeAfterPublish := false
			var lastPublished v2input.Ref
			publisher := localTaskPublisherFunc(func(ctx context.Context, actual artifactjson.Scope, body []byte) (artifactjson.Ref, error) {
				calls++
				if actual != scope {
					t.Fatal("publication scope changed")
				}
				pin := ref(body)
				add(pin, body, "application/json")
				lastPublished = pin
				if revokeAfterPublish {
					fixture.denyPath = "TaskInputs.local_params"
				}
				return artifactjson.Ref{ID: pin.ArtifactID, SHA256: pin.SHA256}, nil
			})
			operation := "portrait.hair"
			if clothing {
				operation = "portrait.clothing"
			}
			cost, _ := v2portrait.LocalPortraitResources(source.Canvas.Width, source.Canvas.Height)
			prepared, e := resolver.PrepareLocalPortraitTask(context.Background(), auth.Identity{}, tenant, publisher, scope, operation, binding, sourceRaw, localRaw, sourceRef.SHA256, cost, rootRef, rootRaw, paramsRef, paramsRaw, vectorsRaw, targetDoc, guardDoc)
			if e != nil || calls != 1 || len(prepared.Evidence.Entries) != len(unique)+3 || prepared.Published.ParamsRef.ArtifactID == uuid.Nil {
				t.Fatal("prepared local task", e)
			}
			workflow := uuid.New()
			frozen, e := FreezeLocalPortraitTask(context.Background(), workflow, 0, prepared, binding, sourceRaw, sourceRef.SHA256, cost)
			if e != nil {
				t.Fatal("freeze", e)
			}
			plan, e := frozen.DocumentPlan()
			if e != nil {
				t.Fatal("saved local plan", e)
			}
			planRaw, _ := canonical.Marshal(plan)
			restored, e := RestoreLocalPortraitTask(context.Background(), workflow, 0, planRaw, sourceRaw, sourceRef.SHA256, cost)
			if e != nil {
				t.Fatal("restore local plan", e)
			}
			restoredDraft, _ := restored.DraftTask()
			proof := LocalPortraitRecoveryProof{Root: rootRef, RootRaw: rootRaw, OriginalParams: paramsRef, OriginalRaw: paramsRaw, VectorsRaw: vectorsRaw}
			ownerWho := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
			readSource, err := resolver.ReadLocalPortraitSource(context.Background(), ownerWho, tenant, binding)
			if err != nil || string(readSource) != string(sourceRaw) {
				t.Fatal("scoped source read", err)
			}
			fixture.denyPath = "TaskInputs.source"
			if raw, err := resolver.ReadLocalPortraitSource(context.Background(), ownerWho, tenant, binding); err == nil || raw != nil {
				t.Fatal("denied source read")
			}
			fixture.denyPath = ""
			ownerRun, ownerProject, ownerSnapshot := uuid.New(), uuid.New(), uuid.New()
			ownerRaw, ownerSHA, e := resolver.CompileLocalPortraitOwnerRequest(context.Background(), ownerWho, tenant, ownerProject, ownerSnapshot, ownerRun, 0, operation, binding, sourceRaw, localRaw, sourceRef.SHA256, cost, proof)
			if e != nil {
				t.Fatal("owner request", e)
			}
			var owner LocalPortraitOwnerRequest
			if json.Unmarshal(ownerRaw, &owner) != nil {
				t.Fatal("owner request decode")
			}
			ownerWorkflow, _ := v2input.WorkflowID(ownerRun)
			computedOwnerSHA, _ := canonical.Hash(owner)
			ownerPublishes := 0
			ownerParams, e := PublishLocalPortraitOwnerParams(context.Background(), localTaskPublisherFunc(func(_ context.Context, s artifactjson.Scope, body []byte) (artifactjson.Ref, error) {
				ownerPublishes++
				if !s.LocalPortrait || s.Tenant != tenant || s.Run != ownerRun || s.Owner != owner.Intent.TaskID || s.Purpose != "task_params" || s.AssemblyLease != uuid.Nil {
					t.Fatal("owner publication scope changed")
				}
				pin := ref(body)
				return artifactjson.Ref{ID: pin.ArtifactID, SHA256: pin.SHA256}, nil
			}), ownerRaw, sourceRaw, sourceRef.SHA256, cost)
			if e != nil || ownerPublishes != 1 || ownerParams.ParamsRef.SHA256 != owner.Intent.ParamsSHA {
				t.Fatal("owner params publication", e)
			}
			fullReserve := cost.OutputBytes + 65536 + 2097152
			for _, failure := range []string{"", "check", "begin", "task_params", "stage", "v2_input_chunk_0", "v2_input_manifest", "admit"} {
				var steps []string
				owners := &localPortraitCoordinatorStore{steps: &steps, fail: failure}
				coordinator := LocalPortraitCoordinator{Resolver: resolver, Owners: owners, Documents: localTaskPublisherFunc(func(_ context.Context, scope artifactjson.Scope, body []byte) (artifactjson.Ref, error) {
					steps = append(steps, scope.Purpose)
					if scope.Tenant != tenant || scope.Run != ownerRun || scope.Owner != owner.Intent.TaskID || scope.AssemblyLease != uuid.Nil || scope.LocalPortrait != (scope.Purpose == "task_params") {
						t.Fatal("coordinator publication scope")
					}
					if failure == scope.Purpose {
						return artifactjson.Ref{}, v2input.ErrExpired
					}
					pin := ref(body)
					add(pin, body, "application/json")
					return artifactjson.Ref{ID: pin.ArtifactID, SHA256: pin.SHA256}, nil
				})}
				materialCalls := 0
				service := LocalPortraitRecoveryService{Coordinator: coordinator, RuntimeSHA: sourceRef.SHA256, Capacity: cost,
					Materials: localPortraitMaterialFunc(func(_ context.Context, _ auth.Identity, _ uuid.UUID, raw []byte) (LocalPortraitMaterial, error) {
						materialCalls++
						if string(raw) != string(ownerRaw) {
							t.Fatal("submission material scope")
						}
						for i := range raw {
							raw[i] = 'x'
						}
						return LocalPortraitMaterial{Source: sourceRaw, Proof: proof}, nil
					})}
				result, err := service.Submit(context.Background(), ownerWho, tenant, ownerRaw, sourceRef.SHA256, fullReserve)
				if materialCalls != 1 {
					t.Fatal("submission did not load current material")
				}
				if failure == "" {
					previousSteps := steps
					for _, boundary := range []string{"factory-error", "nil-material", "cancel-factory", "material-error", "cancel-material"} {
						steps = nil
						ctx, cancel := context.WithCancel(context.Background())
						calls := 0
						loader := localPortraitMaterialFunc(func(context.Context, auth.Identity, uuid.UUID, []byte) (LocalPortraitMaterial, error) {
							calls++
							if boundary == "material-error" {
								return LocalPortraitMaterial{}, v2input.ErrExpired
							}
							cancel()
							return LocalPortraitMaterial{Source: sourceRaw, Proof: proof}, nil
						})
						copyService := service
						copyService.Factory = localPortraitFactoryFunc(func(_ context.Context, request LocalPortraitOwnerRequest) (LocalPortraitMaterialLoader, LocalPortraitCoordinator, error) {
							if request.Tenant != tenant || request.Run != ownerRun {
								t.Fatal("factory request scope")
							}
							if boundary == "factory-error" {
								return nil, LocalPortraitCoordinator{}, v2input.ErrExpired
							}
							if boundary == "nil-material" {
								return nil, coordinator, nil
							}
							if boundary == "cancel-factory" {
								cancel()
							}
							return loader, coordinator, nil
						})
						got, e := copyService.Submit(ctx, ownerWho, tenant, ownerRaw, sourceRef.SHA256, fullReserve)
						cancel()
						wantCalls := 0
						if boundary == "material-error" || boundary == "cancel-material" {
							wantCalls = 1
						}
						if e == nil || got != (LocalPortraitAdmission{}) || calls != wantCalls || len(steps) != 0 {
							t.Fatal("submission crossed failed boundary", boundary, calls, steps, e)
						}
					}
					steps = previousSteps
				}
				all := []string{"check", "begin", "task_params", "stage", "v2_input_chunk_0", "v2_input_manifest", "admit"}
				count := len(all)
				if failure != "" {
					for i, step := range all {
						if step == failure {
							count = i + 1
							break
						}
					}
				}
				if len(steps) != count {
					t.Fatal("coordinator continued after failure", failure, steps, err, fixture.lookups[len(fixture.lookups)-1])
				}
				for i, step := range steps {
					if step != all[i] {
						t.Fatal("coordinator order", steps)
					}
				}
				if failure == "" {
					if err != nil || result.Run != ownerRun || result.Workflow != ownerWorkflow || result.Task != owner.Intent.TaskID {
						t.Fatal("coordinator admission", err)
					}
				} else if err == nil || result != (LocalPortraitAdmission{}) {
					t.Fatal("failed coordinator returned admission", failure)
				}
				steps = nil
				if result, err := service.Submit(context.Background(), auth.Identity{UserID: uuid.New(), SessionID: ownerWho.SessionID}, tenant, ownerRaw, sourceRef.SHA256, fullReserve); err == nil || result != (LocalPortraitAdmission{}) || len(steps) != 0 || materialCalls != 1 {
					t.Fatal("changed actor reached coordinator mutation")
				}
			}
			var recoverySteps []string
			recoveryStore := &localPortraitCoordinatorStore{steps: &recoverySteps}
			recoveryCoordinator := LocalPortraitCoordinator{Resolver: resolver, Owners: recoveryStore, Documents: localTaskPublisherFunc(func(_ context.Context, scope artifactjson.Scope, body []byte) (artifactjson.Ref, error) {
				recoverySteps = append(recoverySteps, scope.Purpose)
				pin := ref(body)
				add(pin, body, "application/json")
				return artifactjson.Ref{ID: pin.ArtifactID, SHA256: pin.SHA256}, nil
			})}
			initial, err := recoveryCoordinator.Submit(context.Background(), ownerWho, tenant, ownerRaw, sourceRaw, proof, sourceRef.SHA256, cost, sourceRef.SHA256, fullReserve)
			if err != nil {
				t.Fatal("recovery setup", err)
			}
			savedRecovery := append([]byte(nil), recoveryStore.saved...)
			recoverySteps = nil
			recovered, err := recoveryCoordinator.Recover(context.Background(), ownerWho, tenant, ownerRaw, savedRecovery, sourceRaw, proof, sourceRef.SHA256, cost, sourceRef.SHA256, fullReserve)
			if err != nil || recovered != initial {
				t.Fatal("staged recovery", err)
			}
			wantRecovery := []string{"check", "begin", "stage", "v2_input_chunk_0", "v2_input_manifest", "admit"}
			if len(recoverySteps) != len(wantRecovery) {
				t.Fatal("recovery republished Params", recoverySteps)
			}
			for i, step := range recoverySteps {
				if step != wantRecovery[i] {
					t.Fatal("recovery order", recoverySteps)
				}
			}
			for _, badSaved := range [][]byte{nil, []byte("{}"), append(append([]byte(nil), savedRecovery...), 'x')} {
				recoverySteps = nil
				if result, err := recoveryCoordinator.Recover(context.Background(), ownerWho, tenant, ownerRaw, badSaved, sourceRaw, proof, sourceRef.SHA256, cost, sourceRef.SHA256, fullReserve); err == nil || result != (LocalPortraitAdmission{}) || len(recoverySteps) != 0 {
					t.Fatal("bad recovery mutated owner")
				}
			}
			fixture.denyPath = "TaskInputs.local_params"
			recoverySteps = nil
			if result, err := recoveryCoordinator.Recover(context.Background(), ownerWho, tenant, ownerRaw, savedRecovery, sourceRaw, proof, sourceRef.SHA256, cost, sourceRef.SHA256, fullReserve); err == nil || result != (LocalPortraitAdmission{}) || len(recoverySteps) != 0 {
				t.Fatal("revoked Params recovery mutated owner")
			}
			fixture.denyPath = ""
			stagedFrozen, err := FreezeLocalPortraitTask(context.Background(), ownerWorkflow, 0, prepared, binding, sourceRaw, sourceRef.SHA256, cost)
			if err != nil {
				t.Fatal(err)
			}
			events := []string{}
			stager := localPortraitStagerFunc(func(_ context.Context, tid, rid uuid.UUID, ordinal int, _ FrozenLocalPortraitTask) error {
				if tid != tenant || rid != ownerRun || ordinal != 0 {
					t.Fatal("stage scope changed")
				}
				events = append(events, "stage")
				return nil
			})
			transport := localTaskPublisherFunc(func(_ context.Context, s artifactjson.Scope, body []byte) (artifactjson.Ref, error) {
				if len(events) == 0 || events[0] != "stage" || s.Tenant != tenant || s.Run != ownerRun || s.Owner != owner.Intent.TaskID || s.LocalPortrait || s.AssemblyLease != uuid.Nil {
					t.Fatal("transport precedes stage or wrong scope")
				}
				events = append(events, s.Purpose)
				pin := ref(body)
				return artifactjson.Ref{ID: pin.ArtifactID, SHA256: pin.SHA256}, nil
			})
			stagedGraph, err := PublishStagedLocalPortraitInputs(context.Background(), stager, transport, tenant, ownerRun, 0, stagedFrozen)
			if err != nil || stagedGraph.ID != owner.Intent.TaskID || len(events) != 3 || events[1] != "v2_input_chunk_0" || events[2] != "v2_input_manifest" {
				t.Fatal("staged transport", err, events)
			}
			events = nil
			failedGraph, err := PublishStagedLocalPortraitInputs(context.Background(), localPortraitStagerFunc(func(context.Context, uuid.UUID, uuid.UUID, int, FrozenLocalPortraitTask) error {
				return v2input.ErrExpired
			}), transport, tenant, ownerRun, 0, stagedFrozen)
			if err == nil || failedGraph.ID != uuid.Nil || len(events) != 0 {
				t.Fatal("failed staging published inputs")
			}
			config, e := localPortraitOwnerConfig(ownerRaw, sourceRef.SHA256, fullReserve)
			if e != nil {
				t.Fatal("owner config", e)
			}

   queuedConfig,err:=localPortraitOwnerConfigForQueue(ownerRaw,sourceRef.SHA256,fullReserve,"interactive")
   if err!=nil{t.Fatal(err)}
   queuedHash:=sha256.Sum256(queuedConfig)
   queuedOwner,err:=decodeLocalPortraitSavedOwner(ownerWho,tenant,ownerRun,ownerRaw,savedRecovery,sourceRef.SHA256,fullReserve,hex.EncodeToString(queuedHash[:]))
   if err!=nil||string(queuedOwner.Request)!=string(ownerRaw)||string(queuedOwner.TaskPlan)!=string(savedRecovery){t.Fatal("queued owner recovery changed",err)}
   if string(queuedConfig)==string(config){t.Fatal("queue absent from config hash input")}
   for _,foreign:=range []string{"batch","training","capture"}{
    if _,err:=localPortraitOwnerConfigForQueue(ownerRaw,sourceRef.SHA256,fullReserve,foreign);err==nil{t.Fatal("foreign local queue accepted")}
   }
			configSum := sha256.Sum256(config)
			configDigest := hex.EncodeToString(configSum[:])
			loaded, err := decodeLocalPortraitSavedOwner(ownerWho, tenant, ownerRun, ownerRaw, savedRecovery, sourceRef.SHA256, fullReserve, configDigest)
			if err != nil || string(loaded.Request) != string(ownerRaw) || string(loaded.TaskPlan) != string(savedRecovery) || loaded.ReservedBytes != fullReserve {
				t.Fatal("saved owner load", err)
			}
			for _, staged := range []bool{false, true} {
				recoverySteps = nil
				loader := localPortraitLoaderFunc(func(_ context.Context, identity auth.Identity, tid, rid uuid.UUID) (LocalPortraitSavedOwner, error) {
					if identity.UserID != ownerWho.UserID || tid != tenant || rid != ownerRun {
						t.Fatal("resume loader scope")
					}
					result := loaded
					if !staged {
						result.TaskPlan = nil
					}
					return result, nil
				})
				result, err := recoveryCoordinator.Resume(context.Background(), loader, ownerWho, tenant, ownerRun, sourceRaw, proof, sourceRef.SHA256, cost)
				if err != nil || result != initial {
					t.Fatal("resume loaded owner", staged, err)
				}
				paramsCount := 0
				for _, step := range recoverySteps {
					if step == "task_params" {
						paramsCount++
					}
				}
				if staged && paramsCount != 0 || !staged && paramsCount != 1 {
					t.Fatal("resume selected wrong phase", staged, recoverySteps)
				}
			}
			for _, missing := range []bool{false, true} {
				recoverySteps = nil
				loader := localPortraitLoaderFunc(func(context.Context, auth.Identity, uuid.UUID, uuid.UUID) (LocalPortraitSavedOwner, error) {
					if missing {
						return LocalPortraitSavedOwner{}, v2input.ErrExpired
					}
					return loaded, nil
				})
				if result, err := recoveryCoordinator.Resume(context.Background(), loader, ownerWho, tenant, uuid.New(), sourceRaw, proof, sourceRef.SHA256, cost); err == nil || result != (LocalPortraitAdmission{}) || len(recoverySteps) != 0 {
					t.Fatal("missing/foreign owner resumed")
				}
			}
			for _, failMaterial := range []bool{false, true} {
				recoverySteps = nil
				materialCalls := 0
				service := LocalPortraitRecoveryService{Coordinator: recoveryCoordinator, RuntimeSHA: sourceRef.SHA256, Capacity: cost,
					Owners: localPortraitLoaderFunc(func(context.Context, auth.Identity, uuid.UUID, uuid.UUID) (LocalPortraitSavedOwner, error) {
						return loaded, nil
					}),
					Materials: localPortraitMaterialFunc(func(_ context.Context, _ auth.Identity, _ uuid.UUID, raw []byte) (LocalPortraitMaterial, error) {
						materialCalls++
						if string(raw) != string(ownerRaw) {
							t.Fatal("service material request changed")
						}
						for i := range raw {
							raw[i] = 'x'
						}
						if failMaterial {
							return LocalPortraitMaterial{}, v2input.ErrExpired
						}
						return LocalPortraitMaterial{Source: sourceRaw, Proof: proof}, nil
					})}
				result, err := service.Resume(context.Background(), ownerWho, tenant, ownerRun)
				if materialCalls != 1 {
					t.Fatal("service material not loaded")
				}
				if failMaterial {
					if err == nil || result != (LocalPortraitAdmission{}) || len(recoverySteps) != 0 {
						t.Fatal("material denial reached mutation")
					}
				} else if err != nil || result != initial {
					t.Fatal("recovery service", err)
				}
			}
			unstaged, err := decodeLocalPortraitSavedOwner(ownerWho, tenant, ownerRun, ownerRaw, []byte("null"), sourceRef.SHA256, fullReserve, configDigest)
			if err != nil || len(unstaged.TaskPlan) != 0 {
				t.Fatal("unstaged owner load", err)
			}
			if _, err := decodeLocalPortraitSavedOwner(ownerWho, tenant, ownerRun, ownerRaw, savedRecovery, sourceRef.SHA256, fullReserve, sourceRef.SHA256); err == nil {
				t.Fatal("changed config digest loaded")
			}
			if _, err := decodeLocalPortraitSavedOwner(ownerWho, uuid.New(), ownerRun, ownerRaw, savedRecovery, sourceRef.SHA256, fullReserve, configDigest); err == nil {
				t.Fatal("cross tenant owner loaded")
			}
			if _, err := decodeLocalPortraitSavedOwner(ownerWho, tenant, ownerRun, ownerRaw, []byte("{}"), sourceRef.SHA256, fullReserve, configDigest); err == nil {
				t.Fatal("malformed staged owner loaded")
			}
			var configObject struct {
				Project  uuid.UUID                 `json:"project_id"`
				Snapshot uuid.UUID                 `json:"input_snapshot_id"`
				Owner    LocalPortraitOwnerRequest `json:"v28_local_portrait"`
			}
			if json.Unmarshal(config, &configObject) != nil || configObject.Project != ownerProject || configObject.Snapshot != ownerSnapshot || configObject.Owner.Run != ownerRun {
				t.Fatal("owner config pins changed")
			}
			if _, err := localPortraitOwnerConfig(ownerRaw, sourceRef.SHA256, fullReserve-1); err == nil {
				t.Fatal("owner transport quota omitted")
			}
			if ownerSHA != computedOwnerSHA || owner.Workflow != ownerWorkflow || owner.Actor != ownerWho.UserID || owner.Session != ownerWho.SessionID || owner.ManualRoot != rootRef || owner.OriginalParams != paramsRef || (owner.Vectors != nil) != (len(vectorsRaw) > 0) {
				t.Fatal("owner pins changed")
			}
			if raw, sha, err := resolver.CompileLocalPortraitOwnerRequest(context.Background(), auth.Identity{}, tenant, ownerProject, ownerSnapshot, ownerRun, 0, operation, binding, sourceRaw, localRaw, sourceRef.SHA256, cost, proof); err == nil || raw != nil || sha != "" {
				t.Fatal("anonymous owner request accepted")
			}
			intent, e := resolver.CompileLocalPortraitIntent(context.Background(), auth.Identity{}, tenant, workflow, 0, operation, binding, sourceRaw, localRaw, sourceRef.SHA256, cost, proof)
			if e != nil || intent.TaskID != restoredDraft.ID || intent.ParamsSHA != prepared.Published.ParamsRef.SHA256 || intent.ReservedBytes != cost.OutputBytes || calls != 1 {
				t.Fatal("unpublished intent", e)
			}
			for _, entry := range intent.ManualEntries {
				if entry.Ref.ArtifactID == prepared.Published.ParamsRef.ArtifactID {
					t.Fatal("intent includes unpublished local Params")
				}
			}
			fixture.denyPath = "TaskInputs.guard"
			linked, err := FreezePublishedLocalPortraitIntent(context.Background(), workflow, 0, intent, prepared, sourceRaw, sourceRef.SHA256, cost)
			if err != nil {
				t.Fatal("published intent freeze", err)
			}
			linkedDraft, _ := linked.DraftTask()
			if linkedDraft.InputDigest != restoredDraft.InputDigest {
				t.Fatal("intent freeze changed digest")
			}
			badIntent := intent
			badIntent.ManualEntries = nil
			if _, err = FreezePublishedLocalPortraitIntent(context.Background(), workflow, 0, badIntent, prepared, sourceRaw, sourceRef.SHA256, cost); err == nil {
				t.Fatal("changed saved manual dependencies accepted")
			}
			deniedIntent, err := resolver.CompileLocalPortraitIntent(context.Background(), auth.Identity{}, tenant, workflow, 0, operation, binding, sourceRaw, localRaw, sourceRef.SHA256, cost, proof)
			if err == nil || deniedIntent.TaskID != uuid.Nil {
				t.Fatal("denied prepublication intent returned")
			}
			fixture.denyPath = ""
			verified, e := resolver.RestoreVerifiedLocalPortraitTask(context.Background(), auth.Identity{}, tenant, workflow, 0, planRaw, sourceRaw, sourceRef.SHA256, cost, proof)
			if e != nil {
				t.Fatal("restore current scope", e)
			}
			verifiedDraft, _ := verified.DraftTask()
			if verifiedDraft.InputDigest != restoredDraft.InputDigest {
				t.Fatal("restored scope digest changed")
			}
			for _, path := range []string{"TaskInputs.guard", "TaskInputs.local_params", "TaskInputs.manual_raster.protected.artifact"} {
				fixture.denyPath = path
				denied, err := resolver.RestoreVerifiedLocalPortraitTask(context.Background(), auth.Identity{}, tenant, workflow, 0, planRaw, sourceRaw, sourceRef.SHA256, cost, proof)
				if err == nil {
					t.Fatal("saved inventory bypassed scope", path)
				}
				if _, err = denied.DraftTask(); err == nil {
					t.Fatal("denied restore returned task")
				}
			}
			fixture.denyPath = ""
			changedPrepared := prepared
			changedPrepared.Evidence.Entries = append(append([]v2input.Entry(nil), prepared.Evidence.Entries...), v2input.Entry{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: sourceRef.SHA256}, Contract: "Artifact"})
			changedFrozen, err := FreezeLocalPortraitTask(context.Background(), workflow, 0, changedPrepared, binding, sourceRaw, sourceRef.SHA256, cost)
			if err != nil {
				t.Fatal(err)
			}
			changedPlan, _ := changedFrozen.DocumentPlan()
			changedRaw, _ := canonical.Marshal(changedPlan)
			if _, err = resolver.RestoreVerifiedLocalPortraitTask(context.Background(), auth.Identity{}, tenant, workflow, 0, changedRaw, sourceRaw, sourceRef.SHA256, cost, proof); err == nil {
				t.Fatal("saved extra input accepted as current closure")
			}
			plan.Task.InputDigest = sourceRef.SHA256
			tamperedPlan, _ := canonical.Marshal(plan)
			if _, e = RestoreLocalPortraitTask(context.Background(), workflow, 0, tamperedPlan, sourceRaw, sourceRef.SHA256, cost); e == nil {
				t.Fatal("changed saved digest restored")
			}
			plan.Entries[0].Contract = "tampered"
			ownedPlan, _ := frozen.DocumentPlan()
			if ownedPlan.Entries[0].Contract == "tampered" {
				t.Fatal("saved plan aliases compiler inventory")
			}
			draft, e := frozen.DraftTask()
			expectedID, _ := v2input.TaskID(workflow, operation, 0)
			if e != nil || draft.ID != expectedID || restoredDraft.InputDigest != draft.InputDigest || draft.ResourceClass != "cpu" || draft.ParamsArtifactID != prepared.Published.ParamsRef.ArtifactID {
				t.Fatal("draft", e)
			}
			draft.ExecutionProfile[0] = '!'
			again, _ := frozen.DraftTask()
			if !json.Valid(again.ExecutionProfile) {
				t.Fatal("profile aliases caller")
			}
			badPrepared := prepared
			badPrepared.Evidence.Entries = nil
			if _, e = FreezeLocalPortraitTask(context.Background(), workflow, 0, badPrepared, binding, sourceRaw, sourceRef.SHA256, cost); e == nil {
				t.Fatal("missing inputs accepted")
			}
			badPrepared = prepared
			badPrepared.Published.Plan.ParamsSHA = rootRef.SHA256
			if _, e = FreezeLocalPortraitTask(context.Background(), workflow, 0, badPrepared, binding, sourceRaw, sourceRef.SHA256, cost); e == nil {
				t.Fatal("changed params pin accepted")
			}
			graph, e := frozen.PublishInputClosure(context.Background(), localClosurePublisherFunc(func(_ context.Context, task uuid.UUID, ordinal int, contract string, body []byte) (v2input.Ref, error) {
				if task != expectedID {
					t.Fatal("transport task changed")
				}
				return ref(body), nil
			}))
			if e != nil || graph.ID != expectedID || graph.Built.Count != len(prepared.Evidence.Entries) {
				t.Fatal("input transport", e)
			}
			for _, role := range []string{"InputClosureChunk", "InputClosureManifest"} {
				aliased, err := frozen.PublishInputClosure(context.Background(), localClosurePublisherFunc(func(_ context.Context, _ uuid.UUID, _ int, contract string, body []byte) (v2input.Ref, error) {
					pin := ref(body)
					if contract == role {
						pin.ArtifactID = prepared.Published.ParamsRef.ArtifactID
					}
					return pin, nil
				}))
				if err == nil || aliased.ID != uuid.Nil {
					t.Fatal("transport aliases input", role)
				}
			}
			failed, e := frozen.PublishInputClosure(context.Background(), localClosurePublisherFunc(func(context.Context, uuid.UUID, int, string, []byte) (v2input.Ref, error) {
				return v2input.Ref{}, v2input.ErrInvalid
			}))
			if e == nil || failed.ID != uuid.Nil || failed.Open != nil {
				t.Fatal("failed transport returned graph")
			}
			lateContext, stop := context.WithCancel(context.Background())
			defer stop()
			failed, e = frozen.PublishInputClosure(lateContext, localClosurePublisherFunc(func(_ context.Context, _ uuid.UUID, _ int, _ string, body []byte) (v2input.Ref, error) {
				stop()
				return ref(body), nil
			}))
			if e != context.Canceled || failed.ID != uuid.Nil || failed.Open != nil {
				t.Fatal("late cancellation returned graph", e)
			}
			prepared.Evidence.Entries[0].Contract = "tampered"
			first, _ := graph.Open(context.Background())
			second, _ := graph.Open(context.Background())
			for {
				a, ok, err := first.Next(context.Background())
				b, ok2, err2 := second.Next(context.Background())
				if err != nil || err2 != nil || ok != ok2 || a != b || a.Contract == "tampered" {
					t.Fatal("mutable retry inventory")
				}
				if !ok {
					break
				}
			}
			cancelled, cancel := context.WithCancel(context.Background())
			cancel()
			if _, e = frozen.PublishInputClosure(cancelled, nil); e != context.Canceled {
				t.Fatal("cancelled transport", e)
			}
			fixture.denyPath = "TaskInputs.guard"
			denied, e := resolver.PrepareLocalPortraitTask(context.Background(), auth.Identity{}, tenant, publisher, scope, operation, binding, sourceRaw, localRaw, sourceRef.SHA256, cost, rootRef, rootRaw, paramsRef, paramsRaw, vectorsRaw, targetDoc, guardDoc)
			if e == nil || calls != 1 || denied.Published.Plan.Params != nil || len(denied.Evidence.Entries) != 0 {
				t.Fatal("preflight denial published Params", e)
			}
			fixture.denyPath = ""
			revokeAfterPublish = true
			late, e := resolver.PrepareLocalPortraitTask(context.Background(), auth.Identity{}, tenant, publisher, scope, operation, binding, sourceRaw, localRaw, sourceRef.SHA256, cost, rootRef, rootRaw, paramsRef, paramsRaw, vectorsRaw, targetDoc, guardDoc)
			if e == nil || calls != 2 || late.Published.Plan.Params != nil || len(late.Evidence.Entries) != 0 {
				t.Fatal("post-publication denial escaped", e)
			}
			fixture.denyPath = ""
			delete(fixture.objects, lastPublished.ArtifactID)
			delete(fixture.objects, prepared.Published.ParamsRef.ArtifactID)
			delete(fixture.objects, localRef.ArtifactID)
		}
		for _, path := range []string{"TaskInputs.target", "TaskInputs.guard"} {
			fixture.denyPath = path
			denied, e := resolver.ResolveManualRasterTaskInputEvidence(context.Background(), auth.Identity{}, uuid.New(), binding, sourceRaw, rootRef, rootRaw, paramsRef, paramsRaw, vectorsRaw, targetDoc, guardDoc)
			if e == nil || len(denied.Entries) != 0 || len(denied.Documents) != 0 || len(denied.Occurrences) != 0 {
				t.Fatal("denied standalone mask evidence escaped", path, e)
			}
		}
		fixture.denyPath = ""
		fixture.bodies[targetDoc.ArtifactID.String()] = append(append([]byte{}, targetRaw...), ' ')
		corrupted, e := resolver.ResolveManualRasterTaskInputEvidence(context.Background(), auth.Identity{}, uuid.New(), binding, sourceRaw, rootRef, rootRaw, paramsRef, paramsRaw, vectorsRaw, targetDoc, guardDoc)
		if e == nil || len(corrupted.Entries) != 0 || len(corrupted.Documents) != 0 {
			t.Fatal("changed standalone bytes accepted", e)
		}
		fixture.bodies[targetDoc.ArtifactID.String()] = targetRaw
		fixture.denyPath = "TaskInputs.manual_raster.protected.artifact"
		evidence, e = resolver.ResolveManualRasterTaskInputEvidence(context.Background(), auth.Identity{}, uuid.New(), binding, sourceRaw, rootRef, rootRaw, paramsRef, paramsRaw, vectorsRaw, targetDoc, guardDoc)
		if e == nil || len(evidence.Entries) != 0 || len(evidence.Documents) != 0 || len(evidence.Occurrences) != 0 {
			t.Fatal("partial denied evidence escaped", e)
		}
		fixture.denyPath = "mask.protected.artifact"
		if _, e = resolver.resolveRoots(context.Background(), auth.Identity{}, uuid.New(), roots, binding); e == nil {
			t.Fatal("denied guard occurrence accepted")
		}
		if external {
			hash, e := canonical.Hash(target["canvas"])
			if e != nil {
				t.Fatal(e)
			}
			node := LayerRef{Ref: expected, Contract: "ManualMask", CanvasHash: hash, Path: "Task.mask.vectors"}
			children, e := DocumentOccurrenceChildren(vectorsRaw, node)
			if e != nil || len(children) != 0 {
				t.Fatal("valid vector occurrence", children, e)
			}
			node.CanvasHash = sourceRef.SHA256
			if _, e = DocumentOccurrenceChildren(vectorsRaw, node); e == nil {
				t.Fatal("foreign canvas occurrence accepted")
			}
		}
		check(append(append([]byte{}, sourceRaw...), ' '), paramsRaw, vectorsRaw, false)
		check(sourceRaw, append(append([]byte{}, paramsRaw...), ' '), vectorsRaw, false)
		if external {
			check(sourceRaw, paramsRaw, nil, false)
			check(sourceRaw, paramsRaw, append(append([]byte{}, vectorsRaw...), ' '), false)
		} else {
			check(sourceRaw, paramsRaw, []byte("{}"), false)
		}
	}
}

type localClosurePublisherFunc func(context.Context, uuid.UUID, int, string, []byte) (v2input.Ref, error)

func (f localClosurePublisherFunc) Publish(ctx context.Context, task uuid.UUID, ordinal int, contract string, body []byte) (v2input.Ref, error) {
	return f(ctx, task, ordinal, contract, body)
}

type localTaskPublisherFunc func(context.Context, artifactjson.Scope, []byte) (artifactjson.Ref, error)

func (f localTaskPublisherFunc) Publish(ctx context.Context, scope artifactjson.Scope, body []byte) (artifactjson.Ref, error) {
	return f(ctx, scope, body)
}

type localPortraitStagerFunc func(context.Context, uuid.UUID, uuid.UUID, int, FrozenLocalPortraitTask) error

func (f localPortraitStagerFunc) Stage(ctx context.Context, tenant, run uuid.UUID, ordinal int, task FrozenLocalPortraitTask) error {
	return f(ctx, tenant, run, ordinal, task)
}

// The store mock models transaction boundaries only; it is not SQL acceptance.
type localPortraitCoordinatorStore struct {
	steps *[]string
	fail  string
	saved []byte
}

func (s *localPortraitCoordinatorStore) step(name string) error {
	*s.steps = append(*s.steps, name)
	if s.fail == name {
		return v2input.ErrExpired
	}
	return nil
}
func (s *localPortraitCoordinatorStore) Check(_ context.Context, raw []byte) error {
	// Simulate a collaborator mutating its buffer; coordinator intent must survive.
	for i := range raw {
		raw[i] = 'x'
	}
	return s.step("check")
}
func (s *localPortraitCoordinatorStore) Begin(_ context.Context, raw []byte, _ string, _ int64) error {
	var owner LocalPortraitOwnerRequest
	if json.Unmarshal(raw, &owner) != nil {
		return v2input.ErrInvalid
	}
	for i := range raw {
		raw[i] = 'x'
	}
	return s.step("begin")
}
func (s *localPortraitCoordinatorStore) Stage(_ context.Context, _, _ uuid.UUID, _ int, task FrozenLocalPortraitTask) error {
	plan, err := task.DocumentPlan()
	if err != nil {
		return err
	}
	s.saved, err = canonical.Marshal(plan)
	if err != nil {
		return err
	}
	return s.step("stage")
}
func (s *localPortraitCoordinatorStore) Admit(_ context.Context, _, _ uuid.UUID, _ int, frozen FrozenLocalPortraitTask, graph v2input.GraphTask) error {
	draft, err := frozen.DraftTask()
	if err != nil || graph.ID != draft.ID || graph.Built.Count != len(frozen.entries) {
		return v2input.ErrInvalid
	}
	return s.step("admit")
}

type localPortraitLoaderFunc func(context.Context, auth.Identity, uuid.UUID, uuid.UUID) (LocalPortraitSavedOwner, error)

func (f localPortraitLoaderFunc) Load(ctx context.Context, who auth.Identity, tenant, run uuid.UUID) (LocalPortraitSavedOwner, error) {
	return f(ctx, who, tenant, run)
}

type localPortraitMaterialFunc func(context.Context, auth.Identity, uuid.UUID, []byte) (LocalPortraitMaterial, error)

func (f localPortraitMaterialFunc) Load(ctx context.Context, who auth.Identity, tenant uuid.UUID, raw []byte) (LocalPortraitMaterial, error) {
	return f(ctx, who, tenant, raw)
}

type localPortraitFactoryFunc func(context.Context, LocalPortraitOwnerRequest) (LocalPortraitMaterialLoader, LocalPortraitCoordinator, error)

func (f localPortraitFactoryFunc) Build(ctx context.Context, request LocalPortraitOwnerRequest) (LocalPortraitMaterialLoader, LocalPortraitCoordinator, error) {
	return f(ctx, request)
}
