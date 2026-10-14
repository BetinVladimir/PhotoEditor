package workflow

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/exportpreset"
	"photoeditor/backend/internal/review"
	"testing"
)

func TestDeliverySharpenResultPreservesMetadata(t *testing.T) {
	plan, parent, child, raw, _ := deliveryExportPlanFixture(t, func(c *exportpreset.Config) {
		c.Sharpen = &exportpreset.Sharpen{Radius: 1, Strength: .4, Threshold: .01}
	})
	registry, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	node, err := CompileDeliveryPresentation(plan, parent, child, raw, registry)
	if err != nil {
		t.Fatal(err)
	}
	image, err := review.ParseWorkingImage(raw)
	if err != nil {
		t.Fatal(err)
	}
	image.Pixels = review.ArtifactRef{ID: uuid.New(), SHA256: downloadDigest("sharpen pixels")}
	outputRaw, _ := canonical.Marshal(image)
	outputRef := review.ArtifactRef{ID: uuid.New(), SHA256: renderDerivativeBytesSHA(outputRaw)}
	report := DeliveryDerivativeReport{Image: node.Inputs[0], Operation: "sharpen", Applied: true, Width: image.Canvas.Width, Height: image.Canvas.Height, MaxChannelDelta: .02, Warnings: []string{}}
	reportRaw, _ := canonical.Marshal(report)
	reportRef := review.ArtifactRef{ID: uuid.New(), SHA256: renderDerivativeBytesSHA(reportRaw)}
	if _, err := ValidateDeliverySharpenResult(*node, raw, outputRef, outputRaw, reportRef, reportRaw); err != nil {
		t.Fatal(err)
	}
	report.MaxChannelDelta = .04
	badReport, _ := canonical.Marshal(report)
	badRef := reportRef
	badRef.SHA256 = renderDerivativeBytesSHA(badReport)
	if _, err := ValidateDeliverySharpenResult(*node, raw, outputRef, outputRaw, badRef, badReport); err == nil {
		t.Fatal("excess delta accepted")
	}
	image.Color.ICCSHA256 = downloadDigest("other icc")
	badOutput, _ := canonical.Marshal(image)
	badOutputRef := outputRef
	badOutputRef.SHA256 = renderDerivativeBytesSHA(badOutput)
	if _, err := ValidateDeliverySharpenResult(*node, raw, badOutputRef, badOutput, reportRef, reportRaw); err == nil {
		t.Fatal("color drift accepted")
	}
}
