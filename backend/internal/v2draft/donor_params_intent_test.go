package v2draft

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
	"strings"
	"testing"
)

func TestDonorParamsRecoveryIntentStrictBindings(t *testing.T) {
	ctx := context.Background()
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	workflow := uuid.New()
	sha := strings.Repeat("a", 64)
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: sha} }
	binding := func() v2input.SessionVersionBinding {
		return v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref(), CanvasHash: sha}
	}
	params := v2portrait.OpenEyesParams{SchemaVersion: 1, Base: binding(), Donor: binding(), Strength: .5, SourceFaceID: uuid.New(), DonorFaceID: uuid.New(), EyesMask: ref(), Protected: ref(), Confirmation: ref(), SourceFaces: ref(), DonorFaces: ref()}
	body, digest, err := params.Compile()
	if err != nil {
		t.Fatal(err)
	}
	pin := v2portrait.DonorQualificationPin{PolicyID: uuid.New(), QualificationID: uuid.New(), IssuerID: uuid.New(), PolicySHA: sha, Report: ref(), Calibration: ref(), ModelLockSHA: sha, RuntimeLockSHA: sha, DeviceProfileSHA: sha, DatasetSHA: sha, Backend: "cpu"}
	owner, _ := v2input.TaskID(workflow, "portrait.open_eyes", 3)
	intent := donorParamsIntent{Actor: who.UserID, Session: who.SessionID, NodeKey: "portrait.open_eyes:3", Owner: owner, Params: string(body), ParamsSHA: digest, Qualification: pin, ReservedBytes: 100, Objects: []v2portrait.DonorQualificationObject{
		{Contract: "Calibration", ArtifactID: pin.Calibration.ArtifactID, SHA256: sha, Bucket: "photos", Key: "calibration", Version: "v1", Bytes: 10},
		{Contract: "Report", ArtifactID: pin.Report.ArtifactID, SHA256: sha, Bucket: "photos", Key: "report", Version: "v2", Bytes: 2 << 20},
	}}
	raw, err := canonical.Marshal(intent)
	if err != nil {
		t.Fatal(err)
	}
	seed, err := ParseDonorParamsRecoveryIntent(ctx, who, workflow, raw)
	if err != nil || seed.Owner() != owner || seed.Ordinal() != 3 || seed.Params() != params || seed.QualificationPin() != pin {
		t.Fatal("valid seed", err)
	}
	objs := seed.QualificationObjects()
	objs[0].Version = "mutated"
	raw[0] = '!'
	if seed.QualificationObjects()[0].Version != "v1" {
		t.Fatal("borrowed objects")
	}
	for _, mutate := range []func(*donorParamsIntent){
		func(i *donorParamsIntent) { i.Actor = uuid.New() }, func(i *donorParamsIntent) { i.Session = uuid.New() },
		func(i *donorParamsIntent) { i.Owner = uuid.New() }, func(i *donorParamsIntent) { i.NodeKey = "portrait.open_eyes:03" },
		func(i *donorParamsIntent) { i.NodeKey = "portrait.open_eyes:5000" }, func(i *donorParamsIntent) { i.Params += " " },
		func(i *donorParamsIntent) { i.ParamsSHA = sha }, func(i *donorParamsIntent) { i.ReservedBytes = 0 },
		func(i *donorParamsIntent) { i.Objects[0].Version = "null" }, func(i *donorParamsIntent) { i.Objects[1].ArtifactID = uuid.New() },
		func(i *donorParamsIntent) { i.Objects[0].Contract = "Report" }, func(i *donorParamsIntent) { i.Qualification.Backend = "cuda" },
	} {
		changed := intent
		changed.Objects = append([]v2portrait.DonorQualificationObject(nil), intent.Objects...)
		mutate(&changed)
		b, _ := canonical.Marshal(changed)
		if got, err := ParseDonorParamsRecoveryIntent(ctx, who, workflow, b); err == nil || got.Owner() != uuid.Nil {
			t.Fatal("changed seed accepted")
		}
	}
	good, _ := canonical.Marshal(intent)
	if _, err := ParseDonorParamsRecoveryIntent(ctx, who, uuid.New(), good); err == nil {
		t.Fatal("wrong workflow accepted")
	}
	for _, b := range [][]byte{[]byte(`{"actor_id":"x","actor_id":"y"}`), append([]byte(" "), good...), []byte(`{}`)} {
		if _, err := ParseDonorParamsRecoveryIntent(ctx, who, workflow, b); err == nil {
			t.Fatal("invalid wire accepted")
		}
	}
}
