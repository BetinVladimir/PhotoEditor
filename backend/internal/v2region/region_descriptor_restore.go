package v2region

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

// RestoreRegionDescriptor rebuilds preparation evidence from a persisted node
// and freshly authorized source/recipe metadata. The caller must establish
// sealed admission, node/config integrity and live access before invoking it.
// No object locator from node JSON is trusted without matching SQL metadata.
func RestoreRegionDescriptor(ctx context.Context, objects RegionObjectReader, nodeJSON []byte, tenant, asset, project, task uuid.UUID, bucket, targetICC string, source, recipe RegionSourceMetadata) (RegionDescriptor, error) {
	fail := func() (RegionDescriptor, error) { return RegionDescriptor{}, ErrRegionInvalid }
	if err := ctx.Err(); err != nil {
		return RegionDescriptor{}, err
	}
	if tenant == uuid.Nil || asset == uuid.Nil || project == uuid.Nil || task == uuid.Nil || bucket == "" || !validRegionSHA(targetICC) || len(nodeJSON) < 2 || len(nodeJSON) > 2*1048576 {
		return fail()
	}
	var node regionNodeDocument
	normalized, err := canonical.Bytes(nodeJSON)
	if err != nil || !bytes.Equal(normalized, nodeJSON) || json.Unmarshal(nodeJSON, &node) != nil {
		return fail()
	}
	rebuilt, err := canonical.Marshal(node)
	if err != nil || !bytes.Equal(rebuilt, nodeJSON) || node.TaskID != task || node.Params.ArtifactID == uuid.Nil || !validRegionSHA(node.Params.SHA256) || !validRegionSHA(node.InputDigest) {
		return fail()
	}
	frozen := FrozenRegionPlan{body: []byte(node.ParamsJCS), digest: node.Params.SHA256}
	document, err := frozen.decode()
	if err != nil {
		return fail()
	}
	if document.TenantID != tenant || document.AssetID != asset || document.ProjectID != project || source.ProjectID != project || recipe.ProjectID != project {
		return fail()
	}
	matches := func(pin regionObjectPin, m RegionSourceMetadata) bool {
		return pin.Ref == m.Ref && pin.Bucket == bucket && pin.Key == m.Object.Key && pin.Version == m.Object.VersionID && pin.Bytes == m.Object.Bytes
	}
	if !matches(document.Source, source) || !matches(document.Recipe, recipe) {
		return fail()
	}
	evidence, err := LoadRegionEvidence(ctx, objects, document.Request, recipe.Object, source.Ref, source.Object)
	if err != nil {
		return RegionDescriptor{}, err
	}
	restored, err := freezeRegionPlan(tenant, asset, project, bucket, document.Request, source, recipe, evidence)
	if err != nil || !bytes.Equal(restored.JSON(), frozen.JSON()) {
		return fail()
	}
	return restored.Descriptor(targetICC)
}
