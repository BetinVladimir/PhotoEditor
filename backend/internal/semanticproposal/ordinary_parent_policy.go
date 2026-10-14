package semanticproposal

import (
	"photoeditor/backend/internal/v2qualification"
	"sort"
)

// Own exact public deployment pins before the prerequisite draft commits.
func ordinaryExpectation(e v2qualification.Expected) createExpectation {
	sets := make([][]string, 3)
	for i, m := range []map[string]struct{}{e.CalibrationSHAs, e.TestFixtureSHAs, e.EvidenceSHAs} {
		for sha := range m {
			sets[i] = append(sets[i], sha)
		}
		sort.Strings(sets[i])
	}
	return createExpectation{Operation: e.Operation, Version: e.Version, Backend: e.Backend, ModelLockSHA: e.ModelLockSHA, RuntimeLockSHA: e.RuntimeLockSHA, DeviceProfileSHA: e.DeviceProfileSHA, DatasetSHA: e.DatasetSHA, IssuerID: e.IssuerID, RequiredChecks: append([]string(nil), e.RequiredChecks...), CalibrationSHAs: sets[0], TestFixtureSHAs: sets[1], EvidenceSHAs: sets[2], LicenseCleared: e.LicenseCleared}
}
func ordinaryParentPolicy(p TransferDraftPreparer) *createProjectPolicy {
	c := ownTransferPreparation(p.Configuration)
	return &createProjectPolicy{ProjectID: p.Reader.ProjectID, Qualification: c.Qualification, Expected: ordinaryExpectation(c.Expected), Person: createPersonPolicy{Report: c.PersonProtection.Report, Expected: ordinaryExpectation(c.PersonProtection.Expected)}, Costs: createResourceCosts{RAM: c.Costs.RAMBytes, VRAM: c.Costs.VRAMBytes, Scratch: c.Costs.ScratchBytes}, RAM: c.RAM, VRAM: c.VRAM, Scratch: c.Scratch, Output: c.Output, Metadata: c.MetadataBytes, Reserved: p.ReservedBytes}
}
