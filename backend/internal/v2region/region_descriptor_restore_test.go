package v2region

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"io"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"testing"
)

type descriptorObjects struct {
	bodies map[string][]byte
	opens  int
}

func (o *descriptorObjects) Open(_ context.Context, obj objectstore.Object) (io.ReadCloser, error) {
	o.opens++
	return io.NopCloser(bytes.NewReader(o.bodies[obj.Key])), nil
}
func TestRestoreRegionDescriptorVerifiesPersistedNodeAndPinnedDocuments(t *testing.T) {
	req, recipeBody, sourceRef, sourceBody := regionEvidenceFixture(t)
	evidence, err := CompileRegionEvidence(req, recipeBody, sourceRef, sourceBody)
	if err != nil {
		t.Fatal(err)
	}
	tenant, asset, project, task := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	metadata := func(ref v2input.Ref, key string, body []byte) RegionSourceMetadata {
		return RegionSourceMetadata{ProjectID: project, Ref: ref, Object: objectstore.Object{Key: key, VersionID: "v1", Bytes: int64(len(body)), SHA256: ref.SHA256, ContentType: "application/json"}}
	}
	source := metadata(sourceRef, "source", sourceBody)
	recipe := metadata(v2input.Ref{ArtifactID: req.Recipe.ArtifactID, SHA256: req.Recipe.SHA256}, "recipe", recipeBody)
	plan, err := freezeRegionPlan(tenant, asset, project, "private", req, source, recipe, evidence)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := CompileRegionExecutionPolicy(plan, regionCostFixture(t, plan))
	if err != nil {
		t.Fatal(err)
	}
	profile := policy.Profile
	node, err := plan.CompileNode(task, v2input.Ref{ArtifactID: uuid.New(), SHA256: plan.SHA256()}, plan.InputRequirements(), profile)
	if err != nil {
		t.Fatal(err)
	}
	reader := &descriptorObjects{bodies: map[string][]byte{"source": sourceBody, "recipe": recipeBody}}
	got, err := RestoreRegionDescriptor(context.Background(), reader, node.JSON(), tenant, asset, project, task, "private", regionSHA, source, recipe)
	if err != nil || got.ParamsSHA256 != plan.SHA256() || reader.opens != 2 {
		t.Fatal("valid restore failed", err)
	}
	for _, bad := range []struct {
		asset  uuid.UUID
		source RegionSourceMetadata
		node   []byte
	}{{uuid.New(), source, node.JSON()}, {asset, func() RegionSourceMetadata { m := source; m.Object.VersionID = "v2"; return m }(), node.JSON()}, {asset, source, append(node.JSON(), ' ')}} {
		reader.opens = 0
		if _, err := RestoreRegionDescriptor(context.Background(), reader, bad.node, tenant, bad.asset, project, task, "private", regionSHA, bad.source, recipe); err == nil || reader.opens != 0 {
			t.Fatal("invalid binding reached object IO", err)
		}
	}
	reader.bodies["source"] = append(append([]byte(nil), sourceBody...), ' ')
	if _, err := RestoreRegionDescriptor(context.Background(), reader, node.JSON(), tenant, asset, project, task, "private", regionSHA, source, recipe); err == nil {
		t.Fatal("corrupt source accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader.opens = 0
	if _, err := RestoreRegionDescriptor(ctx, reader, node.JSON(), tenant, asset, project, task, "private", regionSHA, source, recipe); err != context.Canceled || reader.opens != 0 {
		t.Fatal("cancelled restore reached IO", err)
	}
}
