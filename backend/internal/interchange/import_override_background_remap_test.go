package interchange

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

type overrideBaseStyleStub struct {
	style, after v2input.Ref
	calls        int
}

func (s *overrideBaseStyleStub) ReadBaseStyle(_ context.Context, override v2input.Ref) (v2input.Ref, error) {
	s.calls++
	if !validImportRef(override) {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	if s.calls > 1 && s.after != (v2input.Ref{}) {
		return s.after, nil
	}
	return s.style, nil
}

type overrideStylePublisherStub struct {
	style v2input.Ref
	calls int
}

func (s *overrideStylePublisherStub) RemapImportedStyleBackground(_ context.Context, style v2input.Ref, portable string, target v2input.SessionVersionBinding) (string, error) {
	s.calls++
	if style != s.style || portable != "foreign-asset" || target.AssetVersionID == uuid.Nil {
		return "", v2input.ErrInvalid
	}
	return uuid.NewSHA1(target.AssetVersionID, []byte("qualified-background")).String(), nil
}

func TestBoundOverrideBackgroundRemapRechecksArchivedRecipe(t *testing.T) {
	style := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("style"))}
	override := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("override"))}
	target := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("source"))}, CanvasHash: rawSHA([]byte("canvas"))}
	source := &overrideBaseStyleStub{style: style}
	publisher := &overrideStylePublisherStub{style: style}
	remapper := BoundImportOverrideBackgroundRemapper{Source: source, OldVersion: uuid.New(), Target: target, Style: publisher}
	id, err := remapper.RemapImportedOverrideBackground(context.Background(), override, "foreign-asset", target)
	if err != nil || id == "" || source.calls != 2 || publisher.calls != 1 {
		t.Fatal("exact style-bound publication failed", err)
	}
	foreign := target
	foreign.AssetVersionID = uuid.New()
	if id, err := remapper.RemapImportedOverrideBackground(context.Background(), override, "foreign-asset", foreign); err == nil || id != "" || source.calls != 2 || publisher.calls != 1 {
		t.Fatal("foreign target reached publisher", err)
	}
	source.calls = 0
	source.after = v2input.Ref{ArtifactID: uuid.New(), SHA256: style.SHA256}
	if id, err := remapper.RemapImportedOverrideBackground(context.Background(), override, "foreign-asset", target); err == nil || id != "" || source.calls != 2 || publisher.calls != 2 {
		t.Fatal("changed archived recipe retained publisher receipt", err)
	}
}
