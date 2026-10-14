package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestInitialFacePipelineCancelledBeforeCreation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out, err := (nativePreparationBootstrap{}).advanceInitialFacePipeline(ctx, auth.Identity{}, nativePreparationAnchor{}, nativeFacePipelineStartConfiguration{})
	if !errors.Is(err, context.Canceled) || out.phase != "" || out.operation != "" || out.producers.detector.seal != "" {
		t.Fatal("cancelled initial pipeline exposed creation", err)
	}
}
func TestInitialFacePipelineConfigurationBounds(t *testing.T) {
	budget := nativePreparationBudget{RAM: 1, VRAM: 1, Scratch: 1, Output: 1, Reserved: 8388609}
	stage := nativeFacePipelineStageConfiguration{Report: v2input.Ref{ArtifactID: uuid.New(), SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}, Budget: budget}
	// artifactjson.Publisher satisfies the dependency interface; physical publication
	// remains separately exercised by the isolated integration fixture.
	valid := nativeFacePipelineStartConfiguration{Landmarks: stage, Parser: stage, Skin: budget, TransportDocuments: artifactjson.Publisher{}}
	if err := valid.validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*nativeFacePipelineStartConfiguration){func(c *nativeFacePipelineStartConfiguration) { c.TransportDocuments = nil }, func(c *nativeFacePipelineStartConfiguration) { c.Parser.Report.ArtifactID = uuid.Nil }, func(c *nativeFacePipelineStartConfiguration) { c.Landmarks.Report.SHA256 = "bad" }, func(c *nativeFacePipelineStartConfiguration) { c.Parser.Budget.VRAM = 0 }, func(c *nativeFacePipelineStartConfiguration) { c.Skin.Reserved-- }, func(c *nativeFacePipelineStartConfiguration) { c.Skin.Scratch = 9007199254740992 }} {
		c := valid
		mutate(&c)
		if err := c.validate(); err == nil {
			t.Fatal("invalid initial stage configuration accepted")
		}
	}
}
