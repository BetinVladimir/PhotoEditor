package semanticproposal

import (
	"bytes"
	"encoding/json"
	"sort"
	"testing"

	"photoeditor/backend/internal/v2qualification"
)

func createExpectationForProbe(e v2qualification.Expected) createExpectation {
	sets := make([][]string, 3)
	for i, set := range []map[string]struct{}{e.CalibrationSHAs, e.TestFixtureSHAs, e.EvidenceSHAs} {
		for sha := range set {
			sets[i] = append(sets[i], sha)
		}
		sort.Strings(sets[i])
	}
	return createExpectation{Operation: e.Operation, Version: e.Version, Backend: e.Backend, ModelLockSHA: e.ModelLockSHA, RuntimeLockSHA: e.RuntimeLockSHA, DeviceProfileSHA: e.DeviceProfileSHA, DatasetSHA: e.DatasetSHA, IssuerID: e.IssuerID, RequiredChecks: e.RequiredChecks, CalibrationSHAs: sets[0], TestFixtureSHAs: sets[1], EvidenceSHAs: sets[2], LicenseCleared: e.LicenseCleared}
}
func createDeploymentForProbe(t *testing.T, p TransferDraftPreparer) CreateDeployment {
	t.Helper()
	c := p.Configuration
	project := createProjectPolicy{ProjectID: p.Reader.ProjectID, Qualification: c.Qualification, Expected: createExpectationForProbe(c.Expected), Person: createPersonPolicy{Report: c.PersonProtection.Report, Expected: createExpectationForProbe(c.PersonProtection.Expected)}, Costs: createResourceCosts{RAM: c.Costs.RAMBytes, VRAM: c.Costs.VRAMBytes, Scratch: c.Costs.ScratchBytes}, RAM: c.RAM, VRAM: c.VRAM, Scratch: c.Scratch, Output: c.Output, Metadata: c.MetadataBytes, Reserved: p.ReservedBytes}
	project.Scene=ordinaryParentPolicy(p).Scene
	raw, err := json.Marshal(struct {
		SchemaVersion int                   `json:"schema_version"`
		Projects      []createProjectPolicy `json:"projects"`
	}{1, []createProjectPolicy{project}})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PHOTO_TEST_V27_CREATE_DEPLOYMENT", string(raw))
	deployment, err := DecodeCreateDeployment(raw, c.Color)
	if err != nil {
		t.Fatal("valid create deployment", err)
	}
	for _, invalid := range [][]byte{
		bytes.Replace(raw, []byte(`"schema_version":1`), []byte(`"Schema_Version":1`), 1),
		bytes.Replace(raw, []byte(`"schema_version":1`), []byte(`"schema_version":1,"schema_version":1`), 1),
		bytes.Replace(raw, []byte(`"license_cleared":true`), []byte(`"license_cleared":false`), 1),
		bytes.Replace(raw, []byte(`"projects":[`), []byte(`"unknown":0,"projects":[`), 1),
		append(append([]byte(nil), raw...), []byte(` {}`)...),
	} {
		if got, err := DecodeCreateDeployment(invalid, c.Color); err == nil || len(got.projects) != 0 {
			t.Fatal("invalid deployment accepted")
		}
	}
	// Decoded policy must not retain input byte ownership.
	for i := range raw {
		raw[i] = 0
	}
	if len(deployment.projects) != 1 || deployment.projects[0].ProjectID != project.ProjectID {
		t.Fatal("configuration aliases input bytes")
	}
	return deployment
}
