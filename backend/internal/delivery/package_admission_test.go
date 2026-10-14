package delivery

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func packageAdmissionFixture(t *testing.T) (PreparePlan, auth.Identity, PackageAdmission) {
	t.Helper()
	snapshot, raw, policy, configs := freezeFixture(t)
	_, frozenRaw, sha, err := CompileFrozenInput(snapshot, raw, policy, configs)
	if err != nil {
		t.Fatal(err)
	}
	frozen := v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	workflow, project := uuid.New(), uuid.New()
	_, _, params, err := CompilePrepareProof(project, who.SessionID, frozen, frozenRaw, raw, configs)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := CompilePreparePlan(workflow, project, who.SessionID, v2input.Ref{ArtifactID: uuid.New(), SHA256: digest(params)}, frozen, frozenRaw, raw, configs)
	if err != nil {
		t.Fatal(err)
	}
	var owned FrozenInput
	// The compiler sorts policy UUID lists; derive the policy bytes from its result.
	if err := json.Unmarshal(frozenRaw, &owned); err != nil {
		t.Fatal(err)
	}
	policyRaw, err := canonical.Marshal(owned.Policy)
	if err != nil {
		t.Fatal(err)
	}
	return plan, who, PackageAdmission{Tenant: uuid.New(), Package: uuid.New(), Workflow: workflow, Name: "Доставка", Policy: v2input.Ref{ArtifactID: uuid.New(), SHA256: digest(policyRaw)}}
}

func TestPackageAdmissionBindsIdentityAndFrozenPolicy(t *testing.T) {
	plan, who, request := packageAdmissionFixture(t)
	tx := &proofTx{accepted: true}
	if err := plan.AdmitPackage(context.Background(), tx, who, request); err != nil {
		t.Fatal(err)
	}
	if tx.calls != 1 || len(tx.args) != 12 || tx.args[0] != request.Tenant || tx.args[1] != who.UserID || tx.args[2] != who.SessionID || tx.args[3] != request.Package || tx.args[4] != request.Workflow || tx.args[5] != plan.TaskID() || tx.args[11] != string(plan.ParamsJCS()) {
		t.Fatal("scope or frozen params changed")
	}
	tx.accepted = false
	if err := plan.AdmitPackage(context.Background(), tx, who, request); err == nil {
		t.Fatal("SQL refusal accepted")
	}
}

func TestPackageAdmissionInvalidInputsNeverReachDatabase(t *testing.T) {
	plan, who, request := packageAdmissionFixture(t)
	for _, change := range []func(*PackageAdmission){
		func(r *PackageAdmission) { r.Tenant = uuid.Nil },
		func(r *PackageAdmission) { r.Package = uuid.Nil },
		func(r *PackageAdmission) { r.Workflow = uuid.New() },
		func(r *PackageAdmission) { r.Policy.SHA256 = strings.Repeat("0", 64) },
		func(r *PackageAdmission) { r.Name = "" },
		func(r *PackageAdmission) { r.Name = strings.Repeat("я", 201) },
		func(r *PackageAdmission) { r.Name = string([]byte{255}) },
	} {
		changed := request
		change(&changed)
		tx := &proofTx{accepted: true}
		if err := plan.AdmitPackage(context.Background(), tx, who, changed); err == nil || tx.calls != 0 {
			t.Fatal("invalid package reached SQL")
		}
	}
	tx := &proofTx{accepted: true}
	foreign := who
	foreign.SessionID = uuid.New()
	if err := plan.AdmitPackage(context.Background(), tx, foreign, request); err == nil || tx.calls != 0 {
		t.Fatal("foreign session reached SQL")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := plan.AdmitPackage(ctx, tx, who, request); err != context.Canceled || tx.calls != 0 {
		t.Fatal("cancelled package reached SQL")
	}
}
