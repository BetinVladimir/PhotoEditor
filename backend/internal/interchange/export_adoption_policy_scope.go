package interchange

import (
	"bytes"
	"context"
	"encoding/json"

	"photoeditor/backend/internal/v2input"
)

// schedulerExportPolicyBindings accepts only the same server-frozen plan that
// belongs to the live claim. It never synthesizes an HTTP session identity.
func schedulerExportPolicyBindings(ctx context.Context, intent CreateIntent,
	frozen FrozenExportDocuments, catalog SQLSchedulerExportInputCatalog) ([]v2input.SessionVersionBinding, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if catalog.Claims.DB == nil || catalog.Claim.validate() != nil ||
		len(catalog.Bucket) < 3 || len(catalog.Bucket) > 63 {
		return nil, v2input.ErrInvalid
	}
	actual, checked, err := catalog.Claims.FrozenExportAdoption(ctx, catalog.Claim)
	if err != nil {
		return nil, err
	}
	if actual.Identity != intent.Identity || actual.RequestSHA != intent.RequestSHA ||
		!bytes.Equal(actual.RequestJCS, intent.RequestJCS) ||
		!bytes.Equal(checked.Body(), frozen.Body()) {
		return nil, v2input.ErrInvalid
	}
	var payload exportFreezePayload
	if err := json.Unmarshal(checked.Body(), &payload); err != nil {
		return nil, err
	}
	var snapshot v2input.SessionInputSnapshot
	if err := decodeImportDocument([]byte(payload.SnapshotJCS), &snapshot); err != nil {
		return nil, err
	}
	if len(snapshot.Items) == 0 || len(snapshot.Items) > 30000 {
		return nil, v2input.ErrInvalid
	}
	bindings := make([]v2input.SessionVersionBinding, 0, len(snapshot.Items))
	for _, item := range snapshot.Items {
		bindings = append(bindings, item.Binding)
	}
	return bindings, ctx.Err()
}
