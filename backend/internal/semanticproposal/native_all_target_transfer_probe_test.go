package semanticproposal

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"reflect"
	"strings"
	"testing"
)

func prepareNativeAllTargetTransferProbeReference(t *testing.T, ctx context.Context, p TransferDraftPreparer, who auth.Identity, original detectorDraftPlan, advanced nativeInitialDefectAdvance) (TransferPreparation, nativeInputTransferReferenceDocuments) {
	t.Helper()
	sourcePerson := advanced.protection.published.preparation.plan.prepared.dependencies.preparation.person
	if sourcePerson == nil {
		t.Fatal("missing original person")
	}
	c, err := p.Reader.prepareNativeTransferRuntimeConfiguration(ctx, who, original.request.TenantID, p.Configuration)
	if err != nil {
		t.Fatal("native transfer reference runtime configuration", err)
	}
	selected, err := selectQualificationPolicies(sourcePerson.policy, []string{"mask.person"})
	if err != nil {
		t.Fatal("native transfer reference person policy", err)
	}
	c.PersonProtection = &TransferPersonProtection{Report: sourcePerson.report, Expected: selected["mask.person"]}
	c.SourceProtection = advanced.protection.verified.Root
	_, source, err := p.Reader.readQualifiedSourcePreparationProof(ctx, who, original.request.TenantID, original.request.Intent.Reference, c.Color, false)
	if err != nil {
		t.Fatal("native transfer reference qualified source", err)
	}
	person, err := p.Reader.prepareNativeInputReferencePersonMode(ctx, who, original.request, source, advanced.protection, c.Color, sourcePerson.report, true)
	if err != nil {
		t.Fatal("native transfer reference person coverage", err)
	}
	reference, err := p.Reader.readNativeInputTransferReferenceDocuments(ctx, who, original.request, person, c.Color, c.MetadataBytes)
	if err != nil {
		t.Fatal("native transfer reference documents", err)
	}
	return c, reference
}

func probeNativeAllTargetTransfer(t *testing.T, ctx context.Context, p TransferDraftPreparer, who auth.Identity, original detectorDraftPlan, manifest nativeTargetPreparationManifest, c TransferPreparation, reference nativeInputTransferReferenceDocuments) {
	t.Helper()
	p.Configuration = c
	selected, selectErr := selectQualificationPolicies(reference.person.policy, []string{"mask.person"})
	if selectErr != nil {
		t.Fatal(selectErr)
	}
	if _, selectErr = p.Reader.personCandidate(ctx, who, original.request.TenantID, original.request.Intent.Reference, selected["mask.person"]); !errors.Is(selectErr, v2input.ErrConflict) {
		t.Fatal("shared-source generic discovery must remain ambiguous", selectErr)
	}
	factory := (SQLNativeParentArtifactCatalogFactory{API: p.Reader.DB, Bucket: p.Reader.Bucket}).ForReference
	var targets []nativeTransferTargetProof
	locked, err := withNativeManualParentLock(ctx, p.Scheduler, original.request.TenantID, original.request.RunID, func(work context.Context) error {
		var e error
		targets, e = p.restoreNativeTransferTargets(work, who, original, manifest, reference, factory)
		return e
	})
	if err != nil || !locked {
		t.Fatal("native target transfer restore", err)
	}
	c.TargetProtection = make([]v2input.Ref, len(targets))
	for i, target := range targets {
		c.TargetProtection[i] = target.protection.Root
	}
	inputs, documents, err := p.Reader.prepareNativeAllTargetTransferInputs(ctx, who, original.request, original.published, c, reference, targets)
	if err != nil {
		t.Fatal("native target transfer inputs", err)
	}
	params, err := PublishTransferParams(ctx, p.Documents, who, original.request, original.requestKey, original.published, c.Qualification.ModelLockSHA, c.Qualification.Calibration)
	if err != nil {
		t.Fatal(err)
	}
	compile := func(in []nativeAllTargetTransferTaskInput, docs []v2input.SemanticPinnedDocuments, refs []v2input.Ref) (nativeAllTargetTransferWorkflowPlan, error) {
		return p.Reader.compileNativeAllTargetTransferWorkflowPlan(ctx, who, original.request, original.published, original.anchor.snapshotRef, original.anchor.snapshotBody, refs, in, docs, c)
	}
	plan, err := compile(inputs, documents, params)
	if err != nil {
		t.Fatal("native target transfer compile", err)
	}
	again, err := compile(inputs, documents, params)
	if err != nil || plan.core.configSHA != again.core.configSHA || !reflect.DeepEqual(plan.core.tasks, again.core.tasks) {
		t.Fatal("native target deterministic compile", err)
	}
	if err := p.Reader.validateNativeAllTargetTransferPlanScope(ctx, who, original.request, plan, c); err != nil {
		t.Fatal("native target plan scope", err)
	}
	savedConfig, err := decodeNativeAllTargetTransferConfig(plan.core.config, plan.core.configSHA)
	if err != nil || savedConfig.QueueClass != "batch" || savedConfig.Native.ManifestSHA != manifest.sha || len(savedConfig.Native.Targets) != len(targets) || savedConfig.Native.Reference != original.request.Intent.Reference || savedConfig.Native.Capacity.RAM != c.RAM {
		t.Fatal("native target persisted config", err)
	}
	oldCapacity := ownTransferPreparation(c)
	oldCapacity.legacyAllTargetQueueOmitted = true
	oldPlan, err := p.Reader.compileNativeAllTargetTransferWorkflowPlan(ctx, who, original.request, original.published, original.anchor.snapshotRef, original.anchor.snapshotBody, params, inputs, documents, oldCapacity)
	if err != nil { t.Fatal("historical all-target compile", err) }
	oldConfig, err := decodeNativeAllTargetTransferConfig(oldPlan.core.config, oldPlan.core.configSHA)
	if err != nil || oldConfig.QueueClass != "" { t.Fatal("historical all-target queue changed", err) }
	oldRestored, err := p.Reader.restoreNativeAllTargetTransferCapacity(ctx, who, original.request, oldPlan.core.config, oldPlan.core.configSHA, reference, c.Color)
	if err != nil || !oldRestored.legacyAllTargetQueueOmitted { t.Fatal("historical all-target capacity queue lost", err) }
	oldReplay, err := p.Reader.compileNativeAllTargetTransferWorkflowPlan(ctx, who, original.request, original.published, original.anchor.snapshotRef, original.anchor.snapshotBody, params, inputs, documents, oldRestored)
	if err != nil || oldReplay.core.configSHA != oldPlan.core.configSHA || string(oldReplay.core.config) != string(oldPlan.core.config) || !reflect.DeepEqual(oldReplay.core.tasks, oldPlan.core.tasks) { t.Fatal("historical all-target replay changed", err) }
	if err := p.Reader.validateNativeAllTargetTransferPlanScope(ctx, who, original.request, oldReplay, oldRestored); err != nil { t.Fatal("historical all-target scope", err) }
	recoveredCapacity, err := p.Reader.restoreNativeAllTargetTransferCapacity(ctx, who, original.request, plan.core.config, plan.core.configSHA, reference, c.Color)
	if err != nil || !reflect.DeepEqual(recoveredCapacity, c) {
		t.Fatal("saved all-native capacity changed", err)
	}
	replay, err := p.Reader.compileNativeAllTargetTransferWorkflowPlan(ctx, who, original.request, original.published, original.anchor.snapshotRef, original.anchor.snapshotBody, params, inputs, documents, recoveredCapacity)
	if err != nil || replay.core.configSHA != plan.core.configSHA || !reflect.DeepEqual(replay.core.tasks, plan.core.tasks) {
		t.Fatal("saved capacity recompile", err)
	}
	if _, err := decodeNativeInputTransferSavedConfig(plan.core.config, plan.core.configSHA, p.Reader.ProjectID, who, original.request, c); err == nil {
		t.Fatal("legacy saved decoder accepted all-native marker")
	}
	for _, kind := range []string{"schema", "ordinal", "namespace", "version", "producer", "reuse", "capacity", "input_digest", "unknown", "queue_interactive", "queue_training", "queue_empty", "queue_null"} {
		var changed map[string]any
		if err := json.Unmarshal(plan.core.config, &changed); err != nil {
			t.Fatal(err)
		}
		marker := changed["v27_native_targets_transfer"].(map[string]any)
		entry := marker["targets"].([]any)[0].(map[string]any)
		switch kind {
		case "queue_interactive":
			changed["queue_class"] = "interactive"
		case "queue_training":
			changed["queue_class"] = "training"
		case "queue_empty":
			changed["queue_class"] = ""
		case "queue_null":
			changed["queue_class"] = nil
		case "schema":
			marker["schema_version"] = 2
		case "ordinal":
			entry["ordinal"] = 1
		case "namespace":
			entry["namespace"] = uuid.New().String()
		case "version":
			entry["protection"].(map[string]any)["version"] = "null"
		case "producer":
			entry["producer"] = nil
		case "reuse":
			entry["reuse_reference"] = true
		case "capacity":
			marker["capacity"].(map[string]any)["ram_limit_bytes"] = -1
		case "input_digest":
			entry["input_digest"] = "changed"
		case "unknown":
			marker["unknown_field"] = true
		}
		raw, err := canonical.Marshal(changed)
		if err != nil {
			t.Fatal(err)
		}
		sha, err := canonical.Hash(json.RawMessage(raw))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := decodeNativeAllTargetTransferConfig(raw, sha); err == nil {
			t.Fatal("altered persisted config accepted", kind)
		}
	}
	altered := plan
	var changed map[string]any
	if err := json.Unmarshal(plan.core.config, &changed); err != nil {
		t.Fatal(err)
	}
	changed["v27_native_targets_transfer"].(map[string]any)["manifest_sha"] = original.request.RequestSHA
	altered.core.config, err = canonical.Marshal(changed)
	if err != nil {
		t.Fatal(err)
	}
	altered.core.configSHA, err = canonical.Hash(altered.core.config)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Reader.validateNativeAllTargetTransferPlanScope(ctx, who, original.request, altered, c); err == nil {
		t.Fatal("reshashed persisted marker substitution accepted")
	}
	if _, err := p.Reader.restoreNativeAllTargetTransferCapacity(ctx, who, original.request, altered.core.config, altered.core.configSHA, reference, c.Color); err == nil {
		t.Fatal("reshashed foreign manifest accepted for saved capacity")
	}

	if _, err := p.Reader.decodeNativeAllTargetTransferSaved(ctx, who, original.request, plan.core.config, plan.core.configSHA); err != nil {
		t.Fatal("scoped all-native saved decode", err)
	}
	for i := 0; i < 7; i++ {
		changed := original.request
		identity := who
		reader := p.Reader
		switch i {
		case 0:
			changed.ActorID = uuid.New()
		case 1:
			reader.ProjectID = uuid.New()
		case 2:
			changed.Intent.TargetSnapshotID = uuid.New()
		case 3:
			identity.SessionID = uuid.New()
		case 4:
			changed.RunID = uuid.New()
		case 5:
			changed.RequestSHA = strings.Repeat("f", 64)
		case 6:
			changed.Intent.Reference.Source.ArtifactID = uuid.New()
		}
		if _, err := reader.decodeNativeAllTargetTransferSaved(ctx, identity, changed, plan.core.config, plan.core.configSHA); err == nil {
			t.Fatal("foreign saved metadata scope accepted", i)
		}
	}
	t.Log("V27_NATIVE_ALL_TARGET_TRANSFER_SAVED_SCOPE_OK")
	t.Log("V27_NATIVE_ALL_TARGET_TRANSFER_PERSISTED_CONFIG_OK")
	memory := &proposalMemoryTransport{}
	transported, err := PublishWorkflowTasks(ctx, plan.core, memory)
	if err != nil || len(transported) != len(targets) {
		t.Fatal("native target transport compile", err)
	}
	for i, target := range targets {
		pin, err := plan.core.frozen[i].ProtectionPin()
		if err != nil || pin.TargetProtection != target.protection.Root {
			t.Fatal("native target frozen pin", err)
		}
		entries, err := plan.core.frozen[i].InputEntries(ctx)
		if err != nil {
			t.Fatal(err)
		}
		required := []v2input.Entry{{Ref: target.protection.Root, Contract: "MaskRef"}, {Ref: target.protection.Pixels, Contract: "NpyArray"}, {Ref: original.request.Intent.ReferenceMask, Contract: "MaskRef"}, {Ref: target.binding.Source, Contract: "WorkingImage"}}
		for _, wanted := range required {
			found := false
			for _, entry := range entries {
				if entry == wanted {
					found = true
				}
			}
			if !found {
				t.Fatal("native target frozen closure missing", wanted)
			}
		}
	}
	for _, kind := range []string{"reference_mode", "source_mode", "ordinal", "binding", "seal", "root", "profile", "qualification", "document", "params", "version"} {
		in := append([]nativeAllTargetTransferTaskInput(nil), inputs...)
		docs := append([]v2input.SemanticPinnedDocuments(nil), documents...)
		refs := append([]v2input.Ref(nil), params...)
		switch kind {
		case "reference_mode":
			in[0].reference.person.originalPinned = false
		case "source_mode":
			in[0].target.source.unitRGB = !in[0].target.source.unitRGB
			in[0].target.seal, err = nativeTransferTargetDigest(in[0].target)
			if err != nil {
				t.Fatal(err)
			}
		case "ordinal":
			in[0].target.ordinal++
		case "binding":
			in[0].target.binding.AssetVersionID = uuid.New()
		case "seal":
			in[0].target.seal = "changed"
		case "root":
			in[0].target.protection.Root.ArtifactID = uuid.New()
		case "profile":
			in[0].profile = `{}`
		case "qualification":
			in[0].qualified.expected = "changed"
		case "document":
			docs[0].TargetProtection = json.RawMessage(`{}`)
		case "params":
			refs[0].SHA256 = "changed"
		case "version":
			in[0].target.registered.object.object.VersionID = "changed"
		}
		if _, err := compile(in, docs, refs); err == nil {
			t.Fatal("substituted native target transfer accepted", kind)
		}
	}
	if err := validateNativeTransferPlanScope(who, original.request, plan.core); err == nil {
		t.Fatal("generic native gate accepted all-native plan")
	}
	if err := p.Reader.validateNativeInputTransferPlanScope(ctx, who, original.request, nativeInputTransferWorkflowPlan{core: plan.core, reference: reference}, c); err == nil {
		t.Fatal("old reference gate accepted native targets")
	}
	reuse := nativeTransferTargetProof{ordinal: 0, binding: original.request.Intent.Reference, source: reference.person.source.source, image: string(reference.reference), protection: reference.person.source.protection.verified, reuseReference: true}
	reuse.seal, err = nativeTransferTargetDigest(reuse)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Reader.verifyNativeTransferTarget(ctx, who, original.request, reference, reuse); err != nil {
		t.Fatal("explicit reference reuse proof", err)
	}
	originalPerson := reference.person.source.protection.published.preparation.plan.prepared.dependencies.preparation.person
	if originalPerson == nil {
		t.Fatal("missing exact original person")
	}
	locator, err := nativeDefectPersonRecoveryFromProof(ctx, *originalPerson)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := p.Reader.restoreNativeDefectPersonInput(ctx, who, original.request, locator)
	if err != nil || !reflect.DeepEqual(recovered, *originalPerson) {
		t.Fatal("shared-source exact original person recovery", err)
	}
	changedLocator := locator
	changedLocator.AuthoritySHA = "0" + locator.AuthoritySHA[1:]
	if changedLocator.AuthoritySHA == locator.AuthoritySHA {
		changedLocator.AuthoritySHA = "1" + locator.AuthoritySHA[1:]
	}
	if _, err := p.Reader.restoreNativeDefectPersonInput(ctx, who, original.request, changedLocator); err == nil {
		t.Fatal("changed original recovery locator accepted")
	}
	owner, err := pgxpool.New(ctx, os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", who.SessionID); err != nil {
		t.Fatal(err)
	}
	_, _, refusal := p.Reader.prepareNativeAllTargetTransferInputs(ctx, who, original.request, original.published, c, reference, targets)
	if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=NULL WHERE id=$1", who.SessionID); err != nil {
		t.Fatal(err)
	}
	if refusal == nil {
		t.Fatal("revoked original-pinned transfer preparation accepted")
	}
	checked, err := p.Reader.readNativeInputTransferReferenceDocuments(ctx, who, original.request, reference.person, c.Color, c.MetadataBytes)
	if err != nil || !sameNativeInputTransferDocuments(ctx, checked, reference) {
		t.Fatal("restored exact original reference", err)
	}
	if existing, err := p.Reader.hasNativeInputSavedParent(ctx, who, original.request); err != nil || existing {
		t.Fatal("compiler activated parent", err)
	}
	probeNativeOriginalReferenceConsumer(t, ctx, p, who, original, plan, c)
	if os.Getenv("PHOTO_TEST_V27_NATIVE_ALL_TARGET_ADMIT") == "1" {
		probeNativeAllTargetTransferAdmission(t, ctx, p, who, original, plan, c)
	}
	t.Log("V27_NATIVE_ALL_TARGET_TRANSFER_RESTORE_FREEZE_REPLAY_SUBSTITUTION_OK")
}

// Fixture preparation for the transfer compiler scenario. Exhaustive queued /
// completed / revoked prerequisite advancement replay remains in its own probe.
func probeNativeTargetDefectsReadyForTransfer(t *testing.T, ctx context.Context, p TransferDraftPreparer, who auth.Identity, original detectorDraftPlan, manifest nativeTargetPreparationManifest, ordinal int) {
	t.Helper()
	factory := (SQLNativeParentArtifactCatalogFactory{API: p.Reader.DB, Bucket: p.Reader.Bucket}).ForReference
	advance := func(want string) {
		t.Helper()
		ok, err := withNativeManualParentLock(ctx, p.Scheduler, original.request.TenantID, original.request.RunID, func(work context.Context) error {
			out, err := p.advanceNativeTargetDefects(work, who, original, manifest, factory)
			if err != nil {
				return err
			}
			if out.phase != nativePreparationWaiting || out.operation != want {
				return v2input.ErrConflict
			}
			return nil
		})
		if err != nil || !ok {
			t.Fatal("native transfer fixture target defect advance", want, err)
		}
	}
	advance("defects.propose")
	a, err := compileNativeTargetAnchor(ctx, who, original.request, original.requestKey, original.published, original.anchor.snapshotRef, original.anchor.snapshotBody, manifest, ordinal, "defects.propose")
	if err != nil {
		t.Fatal(err)
	}
	saved, err := p.Reader.readNativeTargetDefectSaved(ctx, who, a)
	if err != nil || !saved.Sealed || saved.RunState != "queued" {
		t.Fatal("native transfer fixture queued defect", err)
	}
	probeNativeWireTaskOutputs(t, ctx, saved.Task)
	advance("semantic.transfer")
	saved, err = p.Reader.readNativeTargetDefectSaved(ctx, who, a)
	if err != nil || saved.RunState != "completed" || saved.TaskState != "succeeded" {
		t.Fatal("native transfer fixture completed defect", err)
	}
	t.Log("V27_TARGET_DEFECT_AUTOMATIC_ADVANCE_READY_FOR_TRANSFER_OK")
}
