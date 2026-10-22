package workflow

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/exportpreset"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func deliveryExportPlanFixture(t *testing.T, customize func(*exportpreset.Config)) (delivery.PreparePlan, uuid.UUID, delivery.ExportChildIntent, []byte, DownloadExportSpec) {
	t.Helper()
	base := downloadExportFixture()
	image := base.Items[0].Image
	steps := []any{}
	stepsSHA, _ := canonical.Hash(map[string]any{"steps": steps})
	image.TransformChain, _ = canonical.Marshal(map[string]any{"steps": steps, "hash": stepsSHA})
	imageRaw, err := canonical.Marshal(image)
	if err != nil {
		t.Fatal(err)
	}
	source := v2input.Ref{ArtifactID: uuid.New(), SHA256: renderDerivativeBytesSHA(imageRaw)}
	canvas, _ := canonical.Hash(image.Canvas)
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: source, CanvasHash: canvas}
	_, snapshotRaw, err := v2input.CompilePinnedSessionInputSnapshot(uuid.New(), []v2input.SessionSnapshotItem{{Ordinal: 0, Binding: binding, WorkingImage: source, Recipe: v2input.Ref{ArtifactID: uuid.New(), SHA256: downloadDigest("recipe")}}})
	if err != nil {
		t.Fatal(err)
	}
	quality := 95
	config := exportpreset.Config{Format: "jpeg", ICCID: "srgb_v1", Quality: &quality, Subsampling: "444", Alpha: "matte", MatteRGB: []float64{1, 1, 1}, MetadataPolicy: "strip", NamingTemplate: "{name}"}
	if customize != nil {
		customize(&config)
	}
	configRaw, err := canonical.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	configSHA := renderDerivativeBytesSHA(configRaw)
	configs := []delivery.PresetContent{{ID: id, SHA: configSHA, Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: configSHA}, Raw: configRaw}}
	_, frozenRaw, frozenSHA, err := delivery.CompileFrozenInput(v2input.Ref{ArtifactID: uuid.New(), SHA256: renderDerivativeBytesSHA(snapshotRaw)}, snapshotRaw, delivery.Policy{RequiredPresets: []uuid.UUID{id}, OptionalPresets: []uuid.UUID{}, MetadataPolicy: config.MetadataPolicy, Provenance: "off"}, configs)
	if err != nil {
		t.Fatal(err)
	}
	frozen := v2input.Ref{ArtifactID: uuid.New(), SHA256: frozenSHA}
	parent, project, session := uuid.New(), uuid.New(), uuid.New()
	workflow, _ := v2input.WorkflowID(parent)
	_, _, params, err := delivery.CompilePrepareProof(project, session, frozen, frozenRaw, snapshotRaw, configs)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := delivery.CompilePreparePlan(workflow, project, session, v2input.Ref{ArtifactID: uuid.New(), SHA256: renderDerivativeBytesSHA(params)}, frozen, frozenRaw, snapshotRaw, configs)
	if err != nil {
		t.Fatal(err)
	}
	page, err := plan.ExportChildren(parent, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	return plan, parent, page.Items[0], imageRaw, base
}

func TestCompileDeliveryExportPinsWorkingSource(t *testing.T) {
	plan, parent, child, imageRaw, base := deliveryExportPlanFixture(t, nil)
	source := child.Binding.Source
	registry, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	presentation, err := CompileDeliveryPresentation(plan, parent, child, imageRaw, registry)
	if err != nil || presentation != nil {
		t.Fatal("unexpected derivative stage", err)
	}
	if _, err := CompileDeliveryPresentation(plan, parent, child, append(imageRaw, ' '), registry); err == nil {
		t.Fatal("presentation source mismatch accepted")
	}
	node, err := CompileDeliveryExport(plan, parent, child, imageRaw, base.TargetICCSHA256, registry)
	if err != nil {
		t.Fatal(err)
	}
	limits := DeliveryDerivativeLimits{RAMBytes: 4 << 30, ScratchBytes: 2 << 30, OutputBytes: 1 << 30}
	profile, err := CompileDeliveryEncodeProfile(node, imageRaw, limits)
	if err != nil {
		t.Fatal(err)
	}
	estimate, err := ParseResourceEstimate(profile)
	if err != nil || estimate.RAMBytes != (128<<20)+400*200*(24+96) || estimate.VRAMBytes != 0 || estimate.PixelCount != 400*200 {
		t.Fatal("encoder resource parity", err)
	}
	for _, resource := range []string{"ram", "scratch", "output"} {
		low := limits
		switch resource {
		case "ram":
			low.RAMBytes = estimate.RAMBytes - 1
		case "scratch":
			low.ScratchBytes = estimate.ScratchBytes - 1
		case "output":
			low.OutputBytes = estimate.OutputBytes - 1
		}
		if _, err := CompileDeliveryEncodeProfile(node, imageRaw, low); err == nil {
			t.Fatal("underreserved encoder", resource)
		}
	}
	var encoded encodeParams
	if json.Unmarshal(node.Params, &encoded) != nil || encoded.Image.ID != source.ArtifactID || node.Operation != "export.encode" || len(node.Inputs) != 2 {
		t.Fatal("source drift")
	}
	encodedRef := review.ArtifactRef{ID: uuid.New(), SHA256: downloadDigest("encoded physical bytes")}
	manifest := DeliveryEncodedImage{Artifact: encodedRef, Format: "jpeg", Width: 400, Height: 200, ICCID: "srgb_v1", ICCSHA256: base.TargetICCSHA256, Alpha: false, MetadataPolicy: "strip"}
	manifestRaw, _ := canonical.Marshal(manifest)
	manifestRef := review.ArtifactRef{ID: uuid.New(), SHA256: renderDerivativeBytesSHA(manifestRaw)}
	if _, err := ValidateDeliveryEncodedManifest(node, imageRaw, manifestRef, manifestRaw, encodedRef); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"width", "icc", "alpha", "metadata", "artifact"} {
		changed := manifest
		switch kind {
		case "width":
			changed.Width++
		case "icc":
			changed.ICCSHA256 = downloadDigest("other icc")
		case "alpha":
			changed.Alpha = true
		case "metadata":
			changed.MetadataPolicy = "safe_camera"
		case "artifact":
			changed.Artifact.ID = uuid.New()
		}
		changedRaw, _ := canonical.Marshal(changed)
		changedRef := manifestRef
		changedRef.SHA256 = renderDerivativeBytesSHA(changedRaw)
		if _, err := ValidateDeliveryEncodedManifest(node, imageRaw, changedRef, changedRaw, encodedRef); err == nil {
			t.Fatal("manifest substitution accepted", kind)
		}
	}
	if _, err := CompileDeliveryExport(plan, parent, child, append(imageRaw, ' '), base.TargetICCSHA256, registry); err == nil {
		t.Fatal("physical mismatch accepted")
	}
}

func TestDeliveryMetadataModesEncodeCleanIntermediate(t *testing.T) {
	registry, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"copyright", "approved_exif"} {
		t.Run(mode, func(t *testing.T) {
			plan, parent, child, image, base := deliveryExportPlanFixture(t, func(config *exportpreset.Config) { config.MetadataPolicy = mode })
			request, _, err := plan.ExportRequest(parent, child)
			if err != nil {
				t.Fatal(err)
			}
			pinned, err := exportpreset.DecodeFrozenConfig([]byte(request.ConfigJCS), request.Preset.PresetSHA)
			if err != nil || pinned.MetadataPolicy != mode {
				t.Fatal("lost delivery policy", err)
			}
			initial, err := CompileDeliveryExport(plan, parent, child, image, base.TargetICCSHA256, registry)
			if err != nil {
				t.Fatal(err)
			}
			final, err := CompileDeliveryFinalEncode(plan, parent, child, image, nil, base.TargetICCSHA256, registry)
			if err != nil {
				t.Fatal(err)
			}
			for _, node := range []CompiledDownloadNode{initial, final} {
				var params encodeParams
				if json.Unmarshal(node.Params, &params) != nil || params.MetadataPolicy != "strip" {
					t.Fatal("metadata leaked into encoder")
				}
			}
			if _, _, err := plan.ChildConfig(parent, child, uuid.New()); err == nil {
				t.Fatal("unbound metadata child admitted")
			}
		})
	}
}
