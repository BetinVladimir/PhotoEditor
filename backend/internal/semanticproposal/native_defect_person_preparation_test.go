package semanticproposal

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// Content-only fixture, not a producer or SQL authority certificate.
func personDefectBindingFixture(t *testing.T) nativeDefectFacePreparation {
	t.Helper()
	raw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []json.RawMessage
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	image, err := review.ParseWorkingImage(fixtures[0])
	if err != nil {
		t.Fatal(err)
	}
	imageBody, _ := canonical.Marshal(image)
	imageSHA, _ := canonical.Hash(image)
	canvasSHA, _ := canonical.Hash(image.Canvas)
	model := strings.Repeat("a", 64)
	mask := review.MaskRef{Artifact: image.Pixels, Canvas: image.Canvas, TransformChain: image.TransformChain, Selector: "person", Confidence: .83, ModelLockID: &model}
	body, _ := canonical.Marshal(mask)
	rootSHA, _ := canonical.Hash(mask)
	policyBody, _ := canonical.Marshal([]qualificationPolicyRow{{ID: uuid.New(), Operation: "mask.person", Version: "1", Model: model, Runtime: model, Device: model, Dataset: model, Backend: "cuda", Issuer: uuid.New(), Checks: []string{"quality"}, Fixtures: []string{model}, Evidence: []string{model}, License: true, Enabled: true, Admission: true}})
	policy := qualificationPolicySnapshot{tenant: uuid.New(), project: uuid.New(), actor: uuid.New(), session: uuid.New(), body: string(policyBody)}
	p := nativePersonInputAuthority{tenant: policy.tenant, project: policy.project, actor: policy.actor, session: policy.session, bucket: "owned", body: string(body), policy: policy, root: v2input.Ref{ArtifactID: uuid.New(), SHA256: rootSHA}, pixels: v2input.Ref{ArtifactID: image.Pixels.ID, SHA256: image.Pixels.SHA256}, binding: v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: imageSHA}, CanvasHash: canvasSHA}}
	selected, err := selectQualificationPolicies(policy, []string{"mask.person"})
	if err != nil {
		t.Fatal(err)
	}
	expected, err := canonical.Marshal(selected["mask.person"])
	if err != nil {
		t.Fatal(err)
	}
	p.expected = string(expected)
	p.seal, err = nativePersonInputAuthorityDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	permanent, err := projectNativePersonProtectionInput(context.Background(), p, imageBody)
	if err != nil {
		t.Fatal(err)
	}
	params, _ := canonical.Marshal(map[string]any{"permanent": permanent, "protections": []review.ProtectionMask{}})
	prepared := nativeDefectFacePreparation{person: &p, params: PreparedDefectParams{params: string(params), roots: `[]`}}
	prepared.landmarksPlan.detector = detectorDraftPlan{request: Request{TenantID: policy.tenant, ActorID: policy.actor, Intent: v2input.SemanticEditIntent{Reference: p.binding}}, policy: policy, prepared: sourceDetectorPreparation{source: sourcePreparationProof{bucket: p.bucket}}}
	prepared.landmarks.detector.sourceBody = string(imageBody)
	return prepared
}

func TestNativeDefectPersonInputBindsParamsScopePolicyAndPins(t *testing.T) {
	ctx := context.Background()
	prepared := personDefectBindingFixture(t)
	if err := verifyNativeDefectPersonInput(ctx, prepared); err != nil {
		t.Fatal(err)
	}
	pins, err := nativeDefectPersonInputPins(ctx, prepared)
	if err != nil || len(pins) != 1 || pins[0].contract != "NpyArray" || pins[0].pin.ref != prepared.person.pixels || pins[0].pin.object != prepared.person.objects[1] {
		t.Fatal("original pixel pin changed", err)
	}
	for _, kind := range []string{"permanent", "protected", "additional", "manual", "tenant", "project", "actor", "session", "bucket", "policy", "expected", "pixels", "model", "source"} {
		changed := prepared
		person := *prepared.person
		changed.person = &person
		var params map[string]json.RawMessage
		if err := json.Unmarshal([]byte(changed.params.params), &params); err != nil {
			t.Fatal(err)
		}
		switch kind {
		case "permanent":
			params["permanent"] = json.RawMessage(`null`)
		case "protected":
			var mask review.ProtectionMask
			_ = json.Unmarshal(params["permanent"], &mask)
			mask.Protected = false
			params["permanent"], _ = canonical.Marshal(mask)
		case "additional":
			params["protections"] = json.RawMessage(`[{}]`)
		case "manual":
			changed.params.roots = `[{}]`
		case "tenant":
			person.tenant = uuid.New()
		case "project":
			person.project = uuid.New()
		case "actor":
			person.actor = uuid.New()
		case "session":
			person.session = uuid.New()
		case "bucket":
			person.bucket = "other"
		case "policy":
			person.policy.body = `[{}]`
		case "expected":
			person.expected = `{}`
		case "pixels":
			person.pixels.ArtifactID = uuid.New()
		case "model":
			var mask review.ProtectionMask
			_ = json.Unmarshal(params["permanent"], &mask)
			model := strings.Repeat("b", 64)
			mask.ModelLockID = &model
			params["permanent"], _ = canonical.Marshal(mask)
		case "source":
			changed.landmarks.detector.sourceBody = `{}`
		}
		// Recompute the private seal to test bindings beyond mere checksum drift.
		person.seal, _ = nativePersonInputAuthorityDigest(person)
		body, _ := canonical.Marshal(params)
		changed.params.params = string(body)
		if err := verifyNativeDefectPersonInput(ctx, changed); err == nil {
			t.Fatalf("%s substitution accepted", kind)
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := nativeDefectPersonInputPins(ctx, prepared); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := (SQLTransferSourceReader{}).prepareNativeDefectPersonFaceParams(ctx, auth.Identity{}, nativeFaceDraftPlan{}, nativeSkinDraftPlan{}, TransferColorProfile{}, [2]float64{}, nativePersonInputAuthority{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestNativeDefectPersonDependencySealPreservesLegacyAndBindsAuthority(t *testing.T) {
	legacy := nativeDefectDependencies{landmarks: "landmarks", skin: "skin"}
	want, _ := canonical.Hash(map[string]any{"landmarks_plan": "", "skin_plan": "", "params": "", "params_sha": "", "inputs": "", "manual_roots": "", "landmarks": "landmarks", "skin": "skin"})
	got, err := nativeDefectDependencyDigest(legacy)
	if err != nil || got != want {
		t.Fatal("legacy digest changed", err)
	}
	proof := legacy
	proof.preparation = personDefectBindingFixture(t)
	original, err := nativeDefectDependencyDigest(proof)
	if err != nil {
		t.Fatal(err)
	}
	changed := proof
	person := *proof.preparation.person
	person.objects[1].VersionID = "substituted"
	changed.preparation.person = &person
	if _, err := nativeDefectDependencyDigest(changed); !errors.Is(err, v2input.ErrConflict) {
		t.Fatal("changed authority seal accepted", err)
	}
	person.seal, _ = nativePersonInputAuthorityDigest(person)
	other, err := nativeDefectDependencyDigest(changed)
	if err != nil || other == original {
		t.Fatal("dependency digest ignores original pixel version", err)
	}
}

// Pure seal integrity only; this content fixture is never admitted as a plan.
func TestNativeDefectDraftSealRevalidatesRetainedPersonDependency(t *testing.T) {
	dependencies := nativeDefectDependencies{preparation: personDefectBindingFixture(t)}
	var err error
	dependencies.seal, err = nativeDefectDependencyDigest(dependencies)
	if err != nil {
		t.Fatal(err)
	}
	plan := nativeDefectDraftPlan{prepared: nativeDefectTaskPreparation{dependencies: dependencies}}
	plan.seal, err = nativeDefectDraftDigest(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyNativeDefectDraftSeal(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	person := *plan.prepared.dependencies.preparation.person
	person.objects[1].VersionID = "substituted"
	person.seal, _ = nativePersonInputAuthorityDigest(person)
	plan.prepared.dependencies.preparation.person = &person
	if err := verifyNativeDefectDraftSeal(context.Background(), plan); !errors.Is(err, v2input.ErrConflict) {
		t.Fatal("changed nested original person bypassed draft seal", err)
	}
}
