package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"reflect"
	"strings"
	"testing"
)

func TestParserBatchOwnedProofAndBarrier(t *testing.T) {
	m := manualPinTestProof()
	p := faceParsePreparationProof{evidence: facePreparationProof{tenant: m.tenant, project: m.project, actor: m.actor, session: m.session, binding: m.binding, bucket: "bucket"}, pixels: m.objects.Pixel}
	var policies [2]v2qualification.Expected
	for i, op := range [2]string{"face.parse", "face.detect"} {
		for j := range p.evidence.objects[i] {
			obj := m.objects.Root
			obj.ref.ArtifactID = uuid.New()
			p.evidence.objects[i][j] = obj
		}
		q := m.objects.Root
		q.ref.ArtifactID = uuid.New()
		p.evidence.qualification[i] = personEvidenceObject{contract: "QualificationReport", ref: q.ref, object: q.object, backend: "cpu"}
		policies[i] = v2qualification.Expected{Operation: op, Version: "1", Backend: "cpu"}
	}
	body, _ := canonical.Marshal(policies)
	p.evidence.expected = string(body)
	s := SQLTransferSourceReader{ProjectID: m.project, Bucket: "bucket"}
	who := auth.Identity{UserID: m.actor, SessionID: m.session}
	second := p
	second.evidence.objects[0][0].ref.ArtifactID = uuid.MustParse("00000000-0000-4000-8000-000000000001")
	p.evidence.objects[0][0].ref.ArtifactID = uuid.MustParse("ffffffff-ffff-4fff-8fff-ffffffffffff")
	tx := &manualLockTx{role: "photo_scheduler"}
	if err := s.lockFaceParsePreparationProofs(context.Background(), tx, who, m.tenant, m.binding, []faceParsePreparationProof{p, second, p}); err != nil {
		t.Fatal(err)
	}
	if len(tx.calls) != 21 || !strings.Contains(tx.calls[10], "person_inventory") || tx.args[1][7] != second.evidence.objects[0][0].ref.ArtifactID {
		t.Fatal("batch order", tx.calls)
	}
	for _, pair := range [][2]int{{1, 12}, {3, 14}, {4, 15}, {6, 17}, {8, 19}, {9, 20}} {
		if !reflect.DeepEqual(tx.args[pair[0]], tx.args[pair[1]]) {
			t.Fatal("pins changed")
		}
	}
	var pins []manualEvidencePin
	if json.Unmarshal([]byte(tx.args[1][14].(string)), &pins) != nil || len(pins) != 5 {
		t.Fatal("labels pin missing")
	}
	for _, fail := range []int{2, 4, 5, 6, 8, 10, 11} {
		tx := &manualLockTx{role: "photo_scheduler", failAt: fail}
		if err := s.lockFaceParsePreparationProofs(context.Background(), tx, who, m.tenant, m.binding, []faceParsePreparationProof{p}); err == nil || len(tx.calls) != fail {
			t.Fatal("failed phase continued", fail, err)
		}
	}
	changed := p
	changed.pixels.object.Key += "-changed"
	invalid := p
	invalid.evidence.expected = "{}"
	for _, batch := range [][]faceParsePreparationProof{nil, {p, changed}, {p, invalid}, make([]faceParsePreparationProof, 65)} {
		tx := &manualLockTx{role: "photo_scheduler"}
		if err := s.lockFaceParsePreparationProofs(context.Background(), tx, who, m.tenant, m.binding, batch); err == nil || len(tx.calls) != 0 {
			t.Fatal("invalid batch reached SQL", err)
		}
	}
	source := sourcePreparationProof{tenant: m.tenant, project: m.project, actor: m.actor, session: m.session, bucket: "bucket", binding: m.binding, iccSHA: m.binding.Source.SHA256, unitRGB: true}
	rootObject := m.objects.Root.object
	rootObject.SHA256 = m.binding.Source.SHA256
	sourcePins := []sourcePreparationPin{{v2draft.LayerRef{Ref: m.binding.Source, Contract: "WorkingImage", CanvasHash: m.binding.CanvasHash}, rootObject}, {v2draft.LayerRef{Ref: m.objects.Pixel.ref, Contract: "WorkingPixels", ParentRef: m.binding.Source}, m.objects.Pixel.object}}
	sourceBody, _ := canonical.Marshal(sourcePins)
	source.pins = string(sourceBody)
	color := TransferColorProfile{sha: source.iccSHA}
	tx = &manualLockTx{role: "photo_scheduler"}
	if err := s.lockFaceParseSourcePreparationProofs(context.Background(), tx, who, m.tenant, m.binding, color, source, []faceParsePreparationProof{p, p}); err != nil {
		t.Fatal(err)
	}
	if len(tx.calls) != 15 || !strings.Contains(tx.calls[1], "source_evidence") || !strings.Contains(tx.calls[7], "person_inventory") || !reflect.DeepEqual(tx.args[1], tx.args[14]) {
		t.Fatal("source/parser/barrier order", tx.calls)
	}
	for _, fail := range []int{2, 4, 6, 7, 8, 10, 12, 13, 15} {
		tx := &manualLockTx{role: "photo_scheduler", failAt: fail}
		if err := s.lockFaceParseSourcePreparationProofs(context.Background(), tx, who, m.tenant, m.binding, color, source, []faceParsePreparationProof{p}); err == nil || len(tx.calls) != fail {
			t.Fatal("combined failure continued", fail, err)
		}
	}
	tx = &manualLockTx{role: "photo_scheduler"}
	if err := s.lockFaceParseSourcePreparationProofs(context.Background(), tx, who, m.tenant, m.binding, color, source, []faceParsePreparationProof{p, invalid}); err == nil || len(tx.calls) != 0 {
		t.Fatal("invalid parser locked source", err)
	}

	tx = &manualLockTx{role: "photo_scheduler"}
	if err := s.lockFaceParseSourcePreparationInventory(context.Background(), tx, who, m.tenant, m.binding, color, source, []faceParsePreparationProof{p}); err != nil {
		t.Fatal(err)
	}
	if len(tx.calls) != 16 || !strings.Contains(tx.calls[1], "source_evidence") || !strings.Contains(tx.calls[7], "person_inventory") || !strings.Contains(tx.calls[13], "face_parse_inventory") || !reflect.DeepEqual(tx.args[1], tx.args[15]) {
		t.Fatal("source/parser inventory order", tx.calls)
	}
	tx = &manualLockTx{role: "photo_scheduler"}
	if err := s.lockFaceParseSourcePreparationInventory(context.Background(), tx, who, m.tenant, m.binding, color, source, []faceParsePreparationProof{p, p}); err == nil || len(tx.calls) != 0 {
		t.Fatal("duplicate parser inventory locked source", err)
	}

	faceProof := p.evidence
	facePolicies := policies
	facePolicies[0].Operation = "face.landmarks"
	facePolicyBody, _ := canonical.Marshal(facePolicies)
	faceProof.expected = string(facePolicyBody)
	tx = &manualLockTx{role: "photo_scheduler"}
	if err := s.lockFaceSourcePreparationInventory(context.Background(), tx, who, m.tenant, m.binding, color, source, []facePreparationProof{faceProof}); err != nil {
		t.Fatal(err)
	}
	if len(tx.calls) != 16 || !strings.Contains(tx.calls[1], "source_evidence") || !strings.Contains(tx.calls[7], "person_inventory") || !strings.Contains(tx.calls[13], "face_inventory") || !reflect.DeepEqual(tx.args[1], tx.args[15]) {
		t.Fatal("source/face inventory order", tx.calls)
	}
	tx = &manualLockTx{role: "photo_scheduler"}
	if err := s.lockFaceSourcePreparationInventory(context.Background(), tx, who, m.tenant, m.binding, color, source, []facePreparationProof{faceProof, faceProof}); err == nil || len(tx.calls) != 0 {
		t.Fatal("duplicate inventory locked source", err)
	}

	tx = &manualLockTx{role: "photo_scheduler"}
	if err := s.lockFaceAndParseSourceInventory(context.Background(), tx, who, m.tenant, m.binding, color, source, []facePreparationProof{faceProof}, []faceParsePreparationProof{p}); err != nil {
		t.Fatal(err)
	}
	if len(tx.calls) != 27 || !strings.Contains(tx.calls[12], "person_inventory") || !strings.Contains(tx.calls[25], "face_inventory") || !strings.Contains(tx.calls[26], "face_parse_inventory") {
		t.Fatal("combined family ordering", tx.calls)
	}
	for i := 0; i < 12; i++ {
		if !reflect.DeepEqual(tx.args[i], tx.args[i+13]) {
			t.Fatal("combined family pins changed", i)
		}
	}
	for fail := 1; fail <= 27; fail++ {
		tx := &manualLockTx{role: "photo_scheduler", failAt: fail}
		if err := s.lockFaceAndParseSourceInventory(context.Background(), tx, who, m.tenant, m.binding, color, source, []facePreparationProof{faceProof}, []faceParsePreparationProof{p}); err == nil || len(tx.calls) != fail {
			t.Fatal("combined family failure continued", fail, err)
		}
	}
	tx = &manualLockTx{role: "photo_scheduler"}
	if err := s.lockFaceAndParseSourceInventory(context.Background(), tx, who, m.tenant, m.binding, color, source, []facePreparationProof{faceProof}, []faceParsePreparationProof{invalid}); err == nil || len(tx.calls) != 0 {
		t.Fatal("invalid later family reached SQL", err)
	}

	tx = &manualLockTx{role: "photo_scheduler"}
	if err := s.lockFaceParseManualSourceInventory(context.Background(), tx, who, m.tenant, m.binding, color, source, []facePreparationProof{faceProof}, []faceParsePreparationProof{p}, []manualPreparationProof{m, m}); err != nil {
		t.Fatal(err)
	}
	if len(tx.calls) != 29 || !strings.Contains(tx.calls[12], "manual_evidence") || !strings.Contains(tx.calls[13], "person_inventory") || !reflect.DeepEqual(tx.args[12], tx.args[26]) {
		t.Fatal("manual before common barrier", tx.calls)
	}
	invalidManual := m
	invalidManual.binding.AssetVersionID = uuid.New()
	tx = &manualLockTx{role: "photo_scheduler"}
	if err := s.lockFaceParseManualSourceInventory(context.Background(), tx, who, m.tenant, m.binding, color, source, []facePreparationProof{faceProof}, []faceParsePreparationProof{p}, []manualPreparationProof{invalidManual}); err == nil || len(tx.calls) != 0 {
		t.Fatal("invalid manual reached SQL", err)
	}

	protected := protectionPreparationProof{tenant: m.tenant, project: m.project, actor: m.actor, session: m.session, bucket: "bucket", binding: m.binding, root: m.objects.Root, pixels: m.objects.Pixel}
	first := personProtectionProof{Binding: m.binding, Root: v2input.Ref{ArtifactID: uuid.MustParse("ffffffff-ffff-4fff-8fff-ffffffffffff"), SHA256: m.objects.Root.ref.SHA256}, Child: m.objects.Pixel.ref, Protection: protected.root.ref, Count: 1, protectionEvidence: protected}
	first.Child.ArtifactID = uuid.New()
	first.Report = v2input.Ref{ArtifactID: uuid.New(), SHA256: m.objects.Root.ref.SHA256}
	expected, _ := canonical.Marshal(v2qualification.Expected{Operation: "mask.person", Version: "1", Backend: "cpu"})
	first.Expected = string(expected)
	evidence := []personEvidencePin{{Contract: "PersonParams", ArtifactID: uuid.New(), SHA256: first.Root.SHA256, Bucket: "bucket", Key: "params", Version: "v1", Bytes: 2, Backend: "cpu"}, {Contract: "PersonReport", ArtifactID: uuid.New(), SHA256: first.Root.SHA256, Bucket: "bucket", Key: "report", Version: "v1", Bytes: 2, Backend: "cpu"}}
	personEvidenceBody, _ := canonical.Marshal(evidence)
	first.Evidence = string(personEvidenceBody)
	qualification, _ := canonical.Marshal(personEvidencePin{Contract: "QualificationReport", ArtifactID: first.Report.ArtifactID, SHA256: first.Report.SHA256, Bucket: "bucket", Key: "qualification", Version: "v1", Bytes: 2, Backend: "cpu"})
	first.Qualification = string(qualification)

	tx = &manualLockTx{role: "photo_scheduler"}
	if err := s.lockSelectedPersonSourceInventory(context.Background(), tx, who, m.tenant, m.binding, color, source, []facePreparationProof{faceProof}, []faceParsePreparationProof{p}, []manualPreparationProof{m}, []personProtectionProof{first, first}); err != nil {
		t.Fatal(err)
	}
	if len(tx.calls) != 38 || !strings.Contains(tx.calls[17], "person_inventory") || !strings.Contains(tx.calls[35], "face_inventory") || !strings.Contains(tx.calls[36], "face_parse_inventory") || !strings.Contains(tx.calls[37], "person_candidate_matches") {
		t.Fatal("person common barrier order", tx.calls)
	}
	for i := 0; i < 17; i++ {
		if !reflect.DeepEqual(tx.args[i], tx.args[i+18]) {
			t.Fatal("person common pins changed", i)
		}
	}
	for fail := 1; fail <= 38; fail++ {
		tx := &manualLockTx{role: "photo_scheduler", failAt: fail}
		if err := s.lockSelectedPersonSourceInventory(context.Background(), tx, who, m.tenant, m.binding, color, source, []facePreparationProof{faceProof}, []faceParsePreparationProof{p}, []manualPreparationProof{m}, []personProtectionProof{first}); err == nil || len(tx.calls) != fail {
			t.Fatal("person shared failure continued", fail, err)
		}
	}
	invalidPerson := first
	invalidPerson.Evidence = "[]"
	tx = &manualLockTx{role: "photo_scheduler"}
	if err := s.lockSelectedPersonSourceInventory(context.Background(), tx, who, m.tenant, m.binding, color, source, []facePreparationProof{faceProof}, []faceParsePreparationProof{p}, []manualPreparationProof{m}, []personProtectionProof{first, invalidPerson}); err == nil || len(tx.calls) != 0 {
		t.Fatal("invalid later person reached SQL", err)
	}

	conflictingPerson := first
	conflictingPerson.Count++
	tx = &manualLockTx{role: "photo_scheduler"}
	if err := s.lockSelectedPersonSourceInventory(context.Background(), tx, who, m.tenant, m.binding, color, source, []facePreparationProof{faceProof}, []faceParsePreparationProof{p}, []manualPreparationProof{m}, []personProtectionProof{first, conflictingPerson}); err != v2input.ErrConflict || len(tx.calls) != 0 {
		t.Fatal("conflicting same-root person reached SQL", err)
	}
	foreignPerson := first
	foreignPerson.Binding.AssetVersionID = uuid.New()
	tx = &manualLockTx{role: "photo_scheduler"}
	if err := s.lockSelectedPersonSourceInventory(context.Background(), tx, who, m.tenant, m.binding, color, source, []facePreparationProof{faceProof}, []faceParsePreparationProof{p}, []manualPreparationProof{m}, []personProtectionProof{foreignPerson}); err != v2input.ErrInvalid || len(tx.calls) != 0 {
		t.Fatal("foreign person binding reached SQL", err)
	}

	policyRows := []qualificationPolicyRow{}
	policyExpected := map[string]v2qualification.Expected{}
	for _, op := range []string{"face.landmarks", "face.detect", "face.parse", "mask.person"} {
		policyRows = append(policyRows, qualificationPolicyRow{ID: uuid.New(), Operation: op, Version: "1", Model: m.objects.Root.ref.SHA256, Runtime: m.objects.Root.ref.SHA256, Device: m.objects.Root.ref.SHA256, Dataset: m.objects.Root.ref.SHA256, Backend: "cpu", Issuer: m.actor, Checks: []string{"quality"}, Fixtures: []string{m.objects.Root.ref.SHA256}, Evidence: []string{m.objects.Root.ref.SHA256}, License: true, Enabled: true, Admission: true})
	}
	policyBody, _ := canonical.Marshal(policyRows)
	policy := qualificationPolicySnapshot{tenant: m.tenant, project: m.project, actor: m.actor, session: m.session, body: string(policyBody)}
	policyExpected, err := selectQualificationPolicies(policy, []string{"face.landmarks", "face.detect", "face.parse", "mask.person"})
	if err != nil {
		t.Fatal(err)
	}
	faceExpectedBody, _ := canonical.Marshal([2]v2qualification.Expected{policyExpected["face.landmarks"], policyExpected["face.detect"]})
	faceProof.expected = string(faceExpectedBody)
	parseExpectedBody, _ := canonical.Marshal([2]v2qualification.Expected{policyExpected["face.parse"], policyExpected["face.detect"]})
	p.evidence.expected = string(parseExpectedBody)
	personExpectedBody, _ := canonical.Marshal(policyExpected["mask.person"])
	first.Expected = string(personExpectedBody)
	tx = &manualLockTx{role: "photo_scheduler"}
	if err := s.lockPolicySelectedSourceInventory(context.Background(), tx, who, m.tenant, m.binding, color, source, []facePreparationProof{faceProof}, []faceParsePreparationProof{p}, []manualPreparationProof{m}, []personProtectionProof{first}, policy); err != nil {
		t.Fatal(err)
	}
	if len(tx.calls) != 40 || !strings.Contains(tx.calls[17], "qualification_policy_snapshot") || !strings.Contains(tx.calls[18], "person_inventory") || !reflect.DeepEqual(tx.args[17], tx.args[39]) {
		t.Fatal("global/tenant barrier order", tx.calls)
	}
	for i := 0; i < 17; i++ {
		if !reflect.DeepEqual(tx.args[i], tx.args[i+19]) {
			t.Fatal("policy shared evidence changed", i)
		}
	}
	for fail := 1; fail <= 40; fail++ {
		tx := &manualLockTx{role: "photo_scheduler", failAt: fail}
		if err := s.lockPolicySelectedSourceInventory(context.Background(), tx, who, m.tenant, m.binding, color, source, []facePreparationProof{faceProof}, []faceParsePreparationProof{p}, []manualPreparationProof{m}, []personProtectionProof{first}, policy); err == nil || len(tx.calls) != fail {
			t.Fatal("policy shared failure continued", fail, err)
		}
	}
	for _, body := range []string{"", "null", "{}", "[]", "[", "[" + strings.Repeat("{},", 64) + "{}]"} {
		changed := policy
		changed.body = body
		tx := &manualLockTx{role: "photo_scheduler"}
		if err := s.lockPolicySelectedSourceInventory(context.Background(), tx, who, m.tenant, m.binding, color, source, []facePreparationProof{faceProof}, []faceParsePreparationProof{p}, []manualPreparationProof{m}, []personProtectionProof{first}, changed); err != v2input.ErrInvalid || len(tx.calls) != 0 {
			t.Fatal("invalid policy snapshot reached SQL", body, err)
		}
	}
	foreignPolicy := policy
	foreignPolicy.session = uuid.New()
	tx = &manualLockTx{role: "photo_scheduler"}
	if err := s.lockPolicySelectedSourceInventory(context.Background(), tx, who, m.tenant, m.binding, color, source, []facePreparationProof{faceProof}, []faceParsePreparationProof{p}, []manualPreparationProof{m}, []personProtectionProof{first}, foreignPolicy); err != v2input.ErrInvalid || len(tx.calls) != 0 {
		t.Fatal("foreign policy snapshot reached SQL", err)
	}

	foreignExpected := policyExpected["mask.person"]
	foreignExpected.RuntimeLockSHA = strings.Repeat("b", 64)
	foreignExpectedBody, _ := canonical.Marshal(foreignExpected)
	foreignExpectedProof := first
	foreignExpectedProof.Expected = string(foreignExpectedBody)
	tx = &manualLockTx{role: "photo_scheduler"}
	if err := s.lockPolicySelectedSourceInventory(context.Background(), tx, who, m.tenant, m.binding, color, source, []facePreparationProof{faceProof}, []faceParsePreparationProof{p}, []manualPreparationProof{m}, []personProtectionProof{foreignExpectedProof}, policy); err != v2qualification.ErrUnqualified || len(tx.calls) != 0 {
		t.Fatal("foreign active policy Expected reached SQL", err)
	}

	allPolicyRows := append([]qualificationPolicyRow(nil), policyRows...)
	for _, op := range []string{"defects.propose", "semantic.transfer"} {
		row := policyRows[0]
		row.ID = uuid.New()
		row.Operation = op
		allPolicyRows = append(allPolicyRows, row)
	}
	allPolicyBody, _ := canonical.Marshal(allPolicyRows)
	allPolicy := policy
	allPolicy.body = string(allPolicyBody)
	executionPolicies, err := selectQualificationPolicies(allPolicy, []string{"defects.propose", "semantic.transfer"})
	if err != nil {
		t.Fatal(err)
	}
	tx = &manualLockTx{role: "photo_scheduler"}
	if err := s.lockProposalPolicySourceInventory(context.Background(), tx, who, m.tenant, m.binding, color, source, []facePreparationProof{faceProof}, []faceParsePreparationProof{p}, []manualPreparationProof{m}, []personProtectionProof{first}, allPolicy, executionPolicies["defects.propose"], executionPolicies["semantic.transfer"]); err != nil || len(tx.calls) != 40 {
		t.Fatal("proposal execution policies composition", err, tx.calls)
	}
	tx = &manualLockTx{role: "photo_scheduler"}
	if err := s.lockProposalPolicySourceInventory(context.Background(), tx, who, m.tenant, m.binding, color, source, []facePreparationProof{faceProof}, []faceParsePreparationProof{p}, []manualPreparationProof{m}, []personProtectionProof{first}, policy, executionPolicies["defects.propose"], executionPolicies["semantic.transfer"]); err != v2qualification.ErrUnqualified || len(tx.calls) != 0 {
		t.Fatal("missing execution policies reached SQL", err)
	}
	foreignTransfer := ownQualificationExpected(executionPolicies["semantic.transfer"])
	foreignTransfer.ModelLockSHA = strings.Repeat("b", 64)
	tx = &manualLockTx{role: "photo_scheduler"}
	if err := s.lockProposalPolicySourceInventory(context.Background(), tx, who, m.tenant, m.binding, color, source, []facePreparationProof{faceProof}, []faceParsePreparationProof{p}, []manualPreparationProof{m}, []personProtectionProof{first}, allPolicy, executionPolicies["defects.propose"], foreignTransfer); err != v2qualification.ErrUnqualified || len(tx.calls) != 0 {
		t.Fatal("foreign execution policy reached SQL", err)
	}

	// Synthetic proofs exercise sequencing only; original document verification is
	// covered separately by the physical retained-transfer fixture.
	for i := range allPolicyRows {
		if allPolicyRows[i].Operation == "semantic.transfer" {
			allPolicyRows[i].Backend = "cuda"
			allPolicyRows[i].Calibrations = []string{m.objects.Root.ref.SHA256}
		}
	}
	allPolicyBody, _ = canonical.Marshal(allPolicyRows)
	allPolicy.body = string(allPolicyBody)
	selectedTransfer, err := selectQualificationPolicies(allPolicy, []string{"semantic.transfer"})
	if err != nil {
		t.Fatal(err)
	}
	expectedTransfer := selectedTransfer["semantic.transfer"]
	pin := v2input.SemanticQualificationPin{PolicyID: uuid.New(), QualificationID: uuid.New(), Report: v2input.Ref{ArtifactID: uuid.New(), SHA256: m.objects.Root.ref.SHA256}, Calibration: v2input.Ref{ArtifactID: uuid.New(), SHA256: m.objects.Root.ref.SHA256}, ModelLockSHA: expectedTransfer.ModelLockSHA, RuntimeLockSHA: expectedTransfer.RuntimeLockSHA, DeviceProfileSHA: expectedTransfer.DeviceProfileSHA, DatasetSHA: expectedTransfer.DatasetSHA, IssuerID: expectedTransfer.IssuerID, Backend: "cuda"}
	pin.PolicySHA, err = v2input.SemanticQualificationPolicyHash(pin.PolicyID, expectedTransfer)
	if err != nil {
		t.Fatal(err)
	}
	expectedBody, _ := canonical.Marshal(expectedTransfer)
	proof := transferQualificationPreparationProof{tenant: m.tenant, project: m.project, actor: m.actor, session: m.session, bucket: s.Bucket, pin: pin, expected: string(expectedBody)}
	for i, contract := range []string{"Calibration", "Report"} {
		ref := pin.Calibration
		if i == 1 {
			ref = pin.Report
		}
		obj := m.objects.Root.object
		obj.SHA256 = ref.SHA256
		proof.objects[i] = qualificationObject{contract: contract, ref: ref, object: obj}
	}
	lock := func(tx *manualLockTx, value transferQualificationPreparationProof) error {
		return s.lockTransferQualifiedSourceInventory(context.Background(), tx, who, m.tenant, m.binding, color, source, []facePreparationProof{faceProof}, []faceParsePreparationProof{p}, []manualPreparationProof{m}, []personProtectionProof{first}, allPolicy, value, expectedTransfer)
	}
	tx = &manualLockTx{role: "photo_scheduler"}
	if err := lock(tx, proof); err != nil {
		t.Fatal(err)
	}
	if len(tx.calls) != 44 || !strings.Contains(tx.calls[1], "transfer_qualification_evidence") || !strings.Contains(tx.calls[19], "qualification_policy_snapshot") || !strings.Contains(tx.calls[20], "person_inventory") || !strings.Contains(tx.calls[22], "transfer_qualification_evidence") || !reflect.DeepEqual(tx.args[1], tx.args[22]) {
		t.Fatal("transfer lock barrier sequence", tx.calls)
	}
	for fail := 1; fail <= 44; fail++ {
		tx = &manualLockTx{role: "photo_scheduler", failAt: fail}
		if err := lock(tx, proof); err == nil || len(tx.calls) != fail {
			t.Fatal("transfer composition continued after failure", fail, err, len(tx.calls))
		}
	}
	invalidTransfer := proof
	invalidTransfer.objects[1].object.VersionID = "null"
	tx = &manualLockTx{role: "photo_scheduler"}
	if err := lock(tx, invalidTransfer); err != v2input.ErrInvalid || len(tx.calls) != 0 {
		t.Fatal("invalid transfer proof reached SQL", err)
	}

	selectedDefect, err := selectQualificationPolicies(allPolicy, []string{"defects.propose"})
	if err != nil {
		t.Fatal(err)
	}
	expectedDefect := selectedDefect["defects.propose"]
	defectBody, _ := canonical.Marshal(expectedDefect)
	defectReport := m.objects.Root
	defectReport.ref.ArtifactID = uuid.New()
	defectProof := defectQualificationPreparationProof{tenant: m.tenant, project: m.project, actor: m.actor, session: m.session, bucket: s.Bucket, expected: string(defectBody), report: personEvidenceObject{contract: "QualificationReport", ref: defectReport.ref, object: defectReport.object, backend: expectedDefect.Backend}}
	combined := func(tx *manualLockTx, d defectQualificationPreparationProof, tr transferQualificationPreparationProof) error {
		return s.lockExecutionQualifiedSourceInventory(context.Background(), tx, who, m.tenant, m.binding, color, source, []facePreparationProof{faceProof}, []faceParsePreparationProof{p}, []manualPreparationProof{m}, []personProtectionProof{first}, allPolicy, d, tr, expectedDefect, expectedTransfer)
	}
	tx = &manualLockTx{role: "photo_scheduler"}
	if err := combined(tx, defectProof, proof); err != nil {
		t.Fatal(err)
	}
	if len(tx.calls) != 48 || !strings.Contains(tx.calls[1], "defect_qualification") || !strings.Contains(tx.calls[3], "transfer_qualification_evidence") || !strings.Contains(tx.calls[21], "qualification_policy_snapshot") || !strings.Contains(tx.calls[22], "person_inventory") || !reflect.DeepEqual(tx.args[1], tx.args[24]) || !reflect.DeepEqual(tx.args[3], tx.args[26]) {
		t.Fatal("combined qualification barrier sequence", tx.calls)
	}
	for fail := 1; fail <= 48; fail++ {
		tx = &manualLockTx{role: "photo_scheduler", failAt: fail}
		if err := combined(tx, defectProof, proof); err == nil || len(tx.calls) != fail {
			t.Fatal("combined qualification continued after failure", fail, err, len(tx.calls))
		}
	}
	for _, change := range []func(*defectQualificationPreparationProof){
		func(p *defectQualificationPreparationProof) { p.session = uuid.New() },
		func(p *defectQualificationPreparationProof) { p.report.object.VersionID = "null" },
		func(p *defectQualificationPreparationProof) { p.report.object.Bytes = 8<<20 + 1 },
		func(p *defectQualificationPreparationProof) { p.expected = "{}" },
	} {
		changed := defectProof
		change(&changed)
		tx = &manualLockTx{role: "photo_scheduler"}
		if err := combined(tx, changed, proof); err == nil || len(tx.calls) != 0 {
			t.Fatal("invalid defect proof reached SQL", err)
		}
	}
	tx = &manualLockTx{role: "photo_scheduler"}
	if err := combined(tx, defectProof, invalidTransfer); err == nil || len(tx.calls) != 0 {
		t.Fatal("invalid late transfer proof locked defect", err)
	}

}
