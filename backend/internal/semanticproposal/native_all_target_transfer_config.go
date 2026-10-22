package semanticproposal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2qualification"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Persisted locators only. SQL admission and recovery must independently repeat
// producer/result/object authority; these fields never confer a capability.
type nativeAllTargetTransferProducer struct {
	Run    uuid.UUID   `json:"run_id"`
	Task   uuid.UUID   `json:"task_id"`
	Result uuid.UUID   `json:"result_id"`
	Params v2input.Ref `json:"params"`
}
type nativeAllTargetTransferTargetLocator struct {
	InputDigest    string                           `json:"input_digest"`
	Ordinal        int                              `json:"ordinal"`
	Binding        v2input.SessionVersionBinding    `json:"source"`
	Namespace      uuid.UUID                        `json:"namespace"`
	ReuseReference bool                             `json:"reuse_reference"`
	Producer       *nativeAllTargetTransferProducer `json:"producer"`
	Protection     nativeSkinCompletedFile          `json:"protection"`
	Pixels         v2input.Ref                      `json:"pixels"`
}
type nativeAllTargetTransferCapacity struct {
	Costs    v2input.SemanticQualifiedCosts `json:"qualified_costs"`
	RAM      int64                          `json:"ram_limit_bytes"`
	VRAM     int64                          `json:"vram_limit_bytes"`
	Scratch  int64                          `json:"scratch_limit_bytes"`
	Output   int64                          `json:"output_limit_bytes"`
	Metadata int64                          `json:"metadata_limit_bytes"`
	Report   v2input.Ref                    `json:"qualification_report"`
	Expected json.RawMessage                `json:"expected_policy"`
}
type nativeAllTargetTransferMarker struct {
	Intent              v2input.Ref                            `json:"intent"`
	Reference           v2input.SessionVersionBinding          `json:"reference"`
	ReferencePixels     v2input.Ref                            `json:"reference_pixels"`
	Schema              int                                    `json:"schema_version"`
	Parent              uuid.UUID                              `json:"parent_run_id"`
	RequestSHA          string                                 `json:"request_sha"`
	ManifestSHA         string                                 `json:"manifest_sha"`
	ReferenceProducer   nativeAllTargetTransferProducer        `json:"reference_producer"`
	ReferenceProtection nativeSkinCompletedFile                `json:"reference_protection"`
	OriginalPerson      nativeDefectPersonRecovery             `json:"original_person"`
	Capacity            nativeAllTargetTransferCapacity        `json:"capacity"`
	Targets             []nativeAllTargetTransferTargetLocator `json:"targets"`
	SceneEvidence       []json.RawMessage                      `json:"scene_evidence,omitempty"`
}
type nativeAllTargetTransferConfig struct {
	v2input.SemanticDraftConfig
	Native nativeAllTargetTransferMarker `json:"v27_native_targets_transfer"`
}

func nativeTransferProtectionLocator(bucket string, pin defectEvidenceObject) nativeSkinCompletedFile {
	o := pin.object
	return nativeSkinCompletedFile{Role: "protection_root", Ref: pin.ref, Bucket: bucket, Key: o.Key, Version: o.VersionID, Bytes: o.Bytes, Media: o.ContentType}
}

// Pure construction after physical preparation. Retains exact original manifest
// identities and saved capacity even when all target ordinals reuse reference.
func compileNativeAllTargetTransferConfig(ctx context.Context, who auth.Identity, request Request, core WorkflowPlan, reference nativeInputTransferReferenceDocuments, targets []nativeTransferTargetProof, c TransferPreparation) (json.RawMessage, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	original := reference.person.source.protection.published.preparation.plan.prepared.dependencies.preparation.landmarksPlan.detector
	manifest, err := compileNativeTargetPreparationManifest(ctx, who, request, original.requestKey, PublishedIntent{Ref: core.intent, Inventory: core.inventory}, core.snapshotRef, core.snapshotBody)
	if err != nil {
		return nil, "", err
	}
	var entries nativeTargetPreparationManifestBody
	if json.Unmarshal(manifest.body, &entries) != nil || len(entries.Entries) != len(targets) || core.transferQualification == nil {
		return nil, "", v2input.ErrInvalid
	}
	var config nativeAllTargetTransferConfig
	if json.Unmarshal(core.config, &config.SemanticDraftConfig) != nil {
		return nil, "", v2input.ErrInvalid
	}
	baseBody, err := canonical.Marshal(config.SemanticDraftConfig)
	if err != nil {
		return nil, "", err
	}
	normalizedBase, err := canonical.Bytes(core.config)
	if err != nil || !bytes.Equal(baseBody, normalizedBase) {
		return nil, "", v2input.ErrInvalid
	}
	// Select saved serialization only after verifying the complete base config.
	if c.legacyAllTargetQueueOmitted {
		config.QueueClass = ""
	}
	registered := reference.person.source.protection
	prep := registered.published.preparation
	person := prep.plan.prepared.dependencies.preparation.person
	if !reference.person.originalPinned || person == nil || len(prep.plan.shell.Tasks) != 1 {
		return nil, "", v2input.ErrInvalid
	}
	personLocator, err := nativeDefectPersonRecoveryFromProof(ctx, *person)
	if err != nil {
		return nil, "", err
	}
	var retained int64
	if err := chargeMetadata(&retained, c.MetadataBytes, len(core.config), len(reference.reference), len(reference.selected), len(reference.protection), len(person.body), len(person.evidence), len(person.qualification), len(targets)*1024); err != nil {
		return nil, "", err
	}
	for _, t := range targets {
		if err := chargeMetadata(&retained, c.MetadataBytes, len(t.completed), len(t.image), len(t.source.pins), len(t.protection.Body), len(t.registered.object.object.Key), len(t.registered.object.object.VersionID)); err != nil {
			return nil, "", err
		}
	}
	q := core.transferQualification
	marker := nativeAllTargetTransferMarker{Intent: core.intent, Reference: request.Intent.Reference, ReferencePixels: registered.verified.Pixels, Schema: 1, Parent: request.RunID, RequestSHA: request.RequestSHA, ManifestSHA: manifest.sha, ReferenceProducer: nativeAllTargetTransferProducer{Run: prep.plan.shell.Run, Task: prep.plan.shell.Tasks[0].ID, Result: prep.result.catalog.ResultID, Params: prep.result.catalog.Params}, ReferenceProtection: nativeTransferProtectionLocator(person.bucket, registered.objects[0]), OriginalPerson: personLocator, Capacity: nativeAllTargetTransferCapacity{Costs: c.Costs, RAM: c.RAM, VRAM: c.VRAM, Scratch: c.Scratch, Output: c.Output, Metadata: c.MetadataBytes, Report: q.objects[1].ref, Expected: json.RawMessage(q.expected)}, Targets: make([]nativeAllTargetTransferTargetLocator, len(targets))}
	for i, t := range targets {
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
		e := entries.Entries[i]
		if t.ordinal != i || t.binding != e.Binding || t.reuseReference != e.ReuseReference {
			return nil, "", v2input.ErrConflict
		}
		locator := nativeAllTargetTransferTargetLocator{InputDigest: core.tasks[i].InputDigest, Ordinal: i, Binding: e.Binding, Namespace: e.Namespace, ReuseReference: e.ReuseReference, Pixels: t.protection.Pixels}
		if t.reuseReference {
			locator.Protection = marker.ReferenceProtection
		} else {
			if t.plan == nil || t.plan.anchor.manifest.sha != manifest.sha || len(t.plan.shell.Tasks) != 1 {
				return nil, "", v2input.ErrConflict
			}
			completed := t.registered.published.preparation.completed
			locator.Producer = &nativeAllTargetTransferProducer{Run: t.plan.shell.Run, Task: t.plan.shell.Tasks[0].ID, Result: completed.catalog.ResultID, Params: completed.catalog.Params}
			locator.Protection = nativeTransferProtectionLocator(t.source.bucket, t.registered.object)
		}
		marker.Targets[i] = locator
	}
	config.Native = marker
	if len(c.sceneEvidence) > 0 {
		config.Native.SceneEvidence = ownTransferPreparation(c).sceneEvidence
	}
	if err := validateNativeAllTargetTransferConfigLocators(config); err != nil {
		return nil, "", err
	}
	raw, err := canonical.Marshal(config)
	if err != nil {
		return nil, "", err
	}
	if len(raw) > 8<<20 {
		return nil, "", v2input.ErrInvalid
	}
	if err := chargeMetadata(&retained, c.MetadataBytes, len(raw)); err != nil {
		return nil, "", err
	}
	sha, err := canonical.Hash(json.RawMessage(raw))
	if err != nil {
		return nil, "", err
	}
	return raw, sha, ctx.Err()
}

// Strict detached decoding; semantic matching and live SQL authority are separate.
func decodeNativeAllTargetTransferConfig(raw json.RawMessage, sha string) (nativeAllTargetTransferConfig, error) {
	var zero nativeAllTargetTransferConfig
	if len(raw) < 2 || len(raw) > 8<<20 {
		return zero, v2input.ErrInvalid
	}
	actual, err := canonical.Hash(raw)
	if err != nil || actual != sha {
		return zero, v2input.ErrConflict
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(normalized))
	decoder.DisallowUnknownFields()
	var config nativeAllTargetTransferConfig
	if decoder.Decode(&config) != nil || decoder.Decode(new(any)) != io.EOF {
		return zero, v2input.ErrInvalid
	}
	encoded, err := canonical.Marshal(config)
	if err != nil || !bytes.Equal(encoded, normalized) || config.Native.Schema != 1 || int64(len(raw)) > config.Native.Capacity.Metadata || len(config.Native.Targets) < 1 || len(config.Native.Targets) > 5000 || len(config.Native.Targets) != len(config.Semantic.Nodes) {
		return zero, v2input.ErrInvalid
	}
	if err := validateNativeAllTargetTransferConfigLocators(config); err != nil {
		return zero, err
	}
	return config, nil
}

func validateNativeAllTargetTransferConfigLocators(config nativeAllTargetTransferConfig) error {
	if config.QueueClass != "" && config.QueueClass != "batch" {
		return v2input.ErrInvalid
	}
	m := config.Native
	if m.Schema != 1 || len(m.Targets) < 1 || len(m.Targets) > 5000 || len(m.Targets) != len(config.Semantic.Nodes) {
		return v2input.ErrInvalid
	}
	validRef := func(ref v2input.Ref) bool {
		_, err := domain.ParseDigest(ref.SHA256)
		return ref.ArtifactID != uuid.Nil && err == nil
	}
	validBinding := func(b v2input.SessionVersionBinding) bool {
		_, err := domain.ParseDigest(b.CanvasHash)
		return b.AssetVersionID != uuid.Nil && validRef(b.Source) && err == nil
	}
	validProducer := func(p nativeAllTargetTransferProducer) bool {
		return p.Run != uuid.Nil && p.Task != uuid.Nil && p.Result != uuid.Nil && validRef(p.Params)
	}
	validProtection := func(p nativeSkinCompletedFile) bool {
		return p.Role == "protection_root" && validRef(p.Ref) && p.Bucket != "" && len(p.Bucket) <= 255 && p.Key != "" && len(p.Key) <= 1024 && p.Version != "" && p.Version != "null" && len(p.Version) <= 8192 && p.Bytes > 1 && p.Bytes <= 8<<20 && p.Media == "application/json"
	}
	if config.ProjectID == uuid.Nil || config.SnapshotID == uuid.Nil || config.Semantic.AuthSessionID == uuid.Nil || config.Semantic.Snapshot == nil || !validRef(*config.Semantic.Snapshot) || config.Semantic.Qualification == nil || m.Parent == uuid.Nil || !validRef(m.Intent) || !validBinding(m.Reference) || !validRef(m.ReferencePixels) || !validProducer(m.ReferenceProducer) || !validProtection(m.ReferenceProtection) {
		return v2input.ErrInvalid
	}
	for _, d := range []string{m.RequestSHA, m.ManifestSHA, config.Semantic.SnapshotDigest} {
		if _, err := domain.ParseDigest(d); err != nil {
			return v2input.ErrInvalid
		}
	}
	raw, err := canonical.Marshal(m.OriginalPerson)
	if err != nil {
		return err
	}
	if _, err := decodeNativeDefectPersonRecovery(raw); err != nil {
		return err
	}
	if m.OriginalPerson.Files[0].Bucket != m.ReferenceProtection.Bucket {
		return v2input.ErrConflict
	}
	capacity := m.Capacity
	for _, limit := range []int64{capacity.RAM, capacity.Scratch, capacity.Output, capacity.Metadata} {
		if limit < 1 || limit > 9007199254740991 {
			return v2input.ErrInvalid
		}
	}
	if capacity.Metadata > capacity.RAM/8 || capacity.VRAM < 1 || capacity.VRAM > 9007199254740991 || !validRef(capacity.Report) {
		return v2input.ErrInvalid
	}
	var expected v2qualification.Expected
	if json.Unmarshal(capacity.Expected, &expected) != nil || expected.Operation != "semantic.transfer" || expected.Version != "1" || (expected.Backend != "cuda" && expected.Backend != "mps") || capacity.Report != config.Semantic.Qualification.Report || verifyQualificationConfiguration(*config.Semantic.Qualification, expected) != nil {
		return v2input.ErrInvalid
	}
	if m.SceneEvidence != nil {
		if len(m.SceneEvidence) != len(m.Targets) {
			return v2input.ErrInvalid
		}
		required := false
		for _, check := range expected.RequiredChecks {
			if check == semanticSceneCheck {
				required = true
			}
		}
		if !required {
			return v2input.ErrInvalid
		}
		pin := *config.Semantic.Qualification
		for i, raw := range m.SceneEvidence {
			scene, err := v2input.DecodeSemanticSceneEvidence(raw)
			if err != nil || scene.Reference.Binding != m.Reference || scene.Target.Binding != m.Targets[i].Binding || scene.Intent != m.Intent || scene.Calibration != pin.Calibration || scene.ModelLockSHA != pin.ModelLockSHA || scene.RuntimeLockSHA != pin.RuntimeLockSHA || scene.DeviceProfileSHA != pin.DeviceProfileSHA || scene.DatasetSHA != pin.DatasetSHA {
				return v2input.ErrInvalid
			}
			if _, ok := expected.EvidenceSHAs[scene.PolicySHA]; !ok {
				return v2input.ErrInvalid
			}
		}
	}
	expectedRaw, err := canonical.Marshal(expected)
	if err != nil {
		return err
	}
	normalizedExpected, err := canonical.Bytes(capacity.Expected)
	if err != nil || !bytes.Equal(expectedRaw, normalizedExpected) {
		return v2input.ErrInvalid
	}
	for _, cost := range []int64{capacity.Costs.RAMBytes, capacity.Costs.VRAMBytes, capacity.Costs.ScratchBytes} {
		if cost < 0 || cost > 9007199254740991 {
			return v2input.ErrInvalid
		}
	}
	matchProducer := func(parent uuid.UUID, p nativeAllTargetTransferProducer) bool {
		run, workflow, err := defectPreparationIDs(parent)
		if err != nil {
			return false
		}
		task, err := v2input.TaskID(workflow, "defects.propose", 0)
		return err == nil && p.Run == run && p.Task == task
	}
	if !matchProducer(m.Parent, m.ReferenceProducer) {
		return v2input.ErrConflict
	}
	workflow, err := v2input.WorkflowID(m.Parent)
	if err != nil {
		return err
	}
	seen := map[uuid.UUID]bool{}
	for i, t := range m.Targets {
		if _, err := domain.ParseDigest(t.InputDigest); err != nil {
			return v2input.ErrInvalid
		}
		if t.Ordinal != i || !validBinding(t.Binding) || !validProtection(t.Protection) || !validRef(t.Pixels) || t.Protection.Bucket != m.ReferenceProtection.Bucket {
			return v2input.ErrInvalid
		}
		digest, err := canonical.Hash(t.Binding)
		if err != nil {
			return err
		}
		namespace := uuid.NewSHA1(m.Parent, []byte(fmt.Sprintf("semantic.target.prepare:1:%s:%d:%s", config.Semantic.Snapshot.SHA256, i, digest)))
		if t.Namespace != namespace {
			return v2input.ErrConflict
		}
		node := config.Semantic.Nodes[i]
		task, err := v2input.TaskID(workflow, "semantic.transfer", i)
		if err != nil || node.TaskID != task || !validRef(v2input.Ref{ArtifactID: node.ParamsArtifactID, SHA256: node.ParamsSHA}) || seen[node.ParamsArtifactID] || node.SourceProtection != m.ReferenceProtection.Ref || node.TargetProtection != t.Protection.Ref {
			return v2input.ErrConflict
		}
		seen[node.ParamsArtifactID] = true
		if t.ReuseReference {
			if t.Producer != nil || t.Binding != m.Reference || t.Protection != m.ReferenceProtection || t.Pixels != m.ReferencePixels {
				return v2input.ErrConflict
			}
		} else if t.Binding == m.Reference || t.Producer == nil || !validProducer(*t.Producer) || !matchProducer(t.Namespace, *t.Producer) || t.Producer.Run == m.ReferenceProducer.Run || t.Producer.Result == m.ReferenceProducer.Result {
			return v2input.ErrConflict
		}
	}
	return nil
}
