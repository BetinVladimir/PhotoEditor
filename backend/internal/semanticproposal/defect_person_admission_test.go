package semanticproposal

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

func TestNativeDefectPersonProofRequiresProtectedPixelInParams(t *testing.T) {
	m := manualPinTestProof()
	who := auth.Identity{UserID: m.actor, SessionID: m.session}
	reader := SQLTransferSourceReader{ProjectID: m.project, Bucket: "bucket"}
	protected := protectionPreparationProof{tenant: m.tenant, project: m.project, actor: m.actor, session: m.session, bucket: "bucket", binding: m.binding, root: m.objects.Root, pixels: m.objects.Pixel}
	proof := personProtectionProof{Binding: m.binding, Root: v2input.Ref{ArtifactID: uuid.New(), SHA256: m.objects.Root.ref.SHA256}, Child: v2input.Ref{ArtifactID: uuid.New(), SHA256: m.objects.Root.ref.SHA256}, Protection: protected.root.ref, Report: v2input.Ref{ArtifactID: uuid.New(), SHA256: m.objects.Root.ref.SHA256}, Count: 1, protectionEvidence: protected}
	body, _ := canonical.Marshal([]personEvidencePin{{Contract: "PersonParams", ArtifactID: uuid.New(), SHA256: proof.Root.SHA256, Bucket: "bucket", Key: "params", Version: "v1", Bytes: 2, Backend: "cpu"}, {Contract: "PersonReport", ArtifactID: uuid.New(), SHA256: proof.Root.SHA256, Bucket: "bucket", Key: "report", Version: "v1", Bytes: 2, Backend: "cpu"}})
	proof.Evidence = string(body)
	body, _ = canonical.Marshal(personEvidencePin{Contract: "QualificationReport", ArtifactID: proof.Report.ArtifactID, SHA256: proof.Report.SHA256, Bucket: "bucket", Key: "qualification", Version: "v1", Bytes: 2, Backend: "cpu"})
	proof.Qualification = string(body)
	body, _ = canonical.Marshal([]qualificationPolicyRow{{ID: uuid.New(), Operation: "mask.person", Version: "1", Model: proof.Root.SHA256, Runtime: proof.Root.SHA256, Device: proof.Root.SHA256, Dataset: proof.Root.SHA256, Backend: "cpu", Issuer: m.actor, Checks: []string{"quality"}, Fixtures: []string{proof.Root.SHA256}, Evidence: []string{proof.Root.SHA256}, License: true, Enabled: true, Admission: true}})
	policy := qualificationPolicySnapshot{tenant: m.tenant, project: m.project, actor: m.actor, session: m.session, body: string(body)}
	selected, err := selectQualificationPolicies(policy, []string{"mask.person"})
	if err != nil {
		t.Fatal(err)
	}
	body, _ = canonical.Marshal(selected["mask.person"])
	proof.Expected = string(body)
	raw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if err := json.Unmarshal(raw, &images); err != nil {
		t.Fatal(err)
	}
	image, err := review.ParseWorkingImage(images[0])
	if err != nil {
		t.Fatal(err)
	}
	source := image.Source(review.ArtifactRef{ID: m.binding.Source.ArtifactID, SHA256: m.binding.Source.SHA256})
	sourceBody, _ := canonical.Marshal(source)
	mask := review.ProtectionMask{Artifact: review.ArtifactRef{ID: protected.pixels.ref.ArtifactID, SHA256: protected.pixels.ref.SHA256}, Canvas: source.Canvas, TransformChain: source.TransformChain, Selector: "protected", Confidence: 1, Protected: true}
	plan := defectPreparationDraftPlan{request: Request{TenantID: m.tenant, ActorID: m.actor, Intent: v2input.SemanticEditIntent{Reference: m.binding}}, execution: defectPolicyExecutionPreparation{policy: policy}, protectionSource: string(sourceBody)}
	compile := func(permanent *review.ProtectionMask) {
		compiled, err := review.BuildDefectProposalTaskInputs(context.Background(), source, []review.DefectProposalFaceInput{}, [2]float64{.002, .012}, permanent, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		plan.params.params = v2input.Ref{ArtifactID: uuid.New(), SHA256: compiled.ParamsSHA256}
		plan.params.preparation.params.params = string(compiled.Params)
	}
	compile(&mask)
	if err := reader.validateDefectPersonAdmission(context.Background(), who, plan, proof); err != nil {
		t.Fatal(err)
	}
	authorityBody, err := reader.marshalDefectPersonAuthority(context.Background(), who, defectPersonAdmissionPreparation{plan: plan, person: proof})
	if err != nil {
		t.Fatal(err)
	}
	var authority defectPersonAuthority
	if json.Unmarshal(authorityBody, &authority) != nil || authority.SchemaVersion != 1 || authority.Root != proof.Root || authority.Child != proof.Child || authority.Protection != proof.Protection || authority.ProtectedPixels != protected.pixels.ref || authority.Count != proof.Count {
		t.Fatal("person authority changed covered identities")
	}
	var protectionPins []manualEvidencePin
	if json.Unmarshal(authority.ProtectionEvidence, &protectionPins) != nil || len(protectionPins) != 2 || protectionPins[0].Role != "protected_pixels" || protectionPins[1].Role != "protected_ref" {
		t.Fatal("person authority lost protection evidence")
	}
	compile(nil)
	if err := reader.validateDefectPersonAdmission(context.Background(), who, plan, proof); err != v2input.ErrInvalid {
		t.Fatal("missing protected person pixel accepted", err)
	}
	if body, err := reader.marshalDefectPersonAuthority(context.Background(), who, defectPersonAdmissionPreparation{plan: plan, person: proof}); err != v2input.ErrInvalid || body != nil {
		t.Fatal("missing covered pixels serialized", err)
	}
	if _, err := reader.prepareDefectPersonAdmission(context.Background(), who, plan, proof); err != v2input.ErrInvalid {
		t.Fatal("invalid Params reached catalog IO", err)
	}
}
