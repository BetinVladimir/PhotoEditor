package provenance

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestSigningRenderDependencyClassificationNeverDropsEntries(t *testing.T) {
	pin := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	inventory := SigningRenderCausalInventory{Models: []v2input.Ref{pin()}, Backgrounds: []v2input.Ref{pin()}}
	for _, contract := range []string{"FrozenRenderRecipe", "StyleV2", "CorrectionPlan", "Override", "LayerStack", "Params", "RasterizedMask", "ManualMask", "MaskRef", "NpyArray", "DescriptorShard", "FaceObservations", "IdentityReport", "ImageReport"} {
		inventory.Controls = append(inventory.Controls, v2input.Entry{Ref: pin(), Contract: contract})
	}
	claims, err := compileSigningRenderDependencyClaims(inventory)
	if err != nil || len(claims) != len(inventory.Controls)+2 {
		t.Fatal("dependency lost", err)
	}
	if claims[0].Kind != "parameter" || claims[1].Kind != "source" {
		t.Fatal("model/background provenance confused")
	}
	for _, claim := range claims[2:] {
		if claim.Kind != "parameter" || claim.Producer != uuid.Nil {
			t.Fatal("control became original source")
		}
	}
	inventory.Controls = append(inventory.Controls, v2input.Entry{Ref: pin(), Contract: "Artifact"})
	if got, err := compileSigningRenderDependencyClaims(inventory); err == nil || got != nil {
		t.Fatal("unknown content silently omitted")
	}
}
func TestSigningRenderOperationRouterPreservesAuthorityFailure(t *testing.T) {
	for _, operation := range []string{"render.compose", "export.encode"} {
		t.Run(operation, func(t *testing.T) {
			failure := errors.New("authority revoked")
			renderCalls, presentationCalls := 0, 0
			router := RenderPresentationSigningTaskResolver{
				Render: signingTaskClaimsFunc(func(context.Context, auth.Identity, ReadySigningExport, CanonicalSigningTask) (ResolvedSigningTask, error) {
					renderCalls++
					return ResolvedSigningTask{}, failure
				}),
				Presentation: signingTaskClaimsFunc(func(context.Context, auth.Identity, ReadySigningExport, CanonicalSigningTask) (ResolvedSigningTask, error) {
					presentationCalls++
					return ResolvedSigningTask{}, failure
				}),
			}
			_, err := router.ResolveSigningTaskLineage(context.Background(), auth.Identity{}, ReadySigningExport{}, CanonicalSigningTask{Operation: operation, Version: "1"})
			if !errors.Is(err, failure) {
				t.Fatal("authority error hidden", err)
			}
			if operation == "render.compose" {
				if renderCalls != 1 || presentationCalls != 0 {
					t.Fatal("render fell through")
				}
			} else if renderCalls != 0 || presentationCalls != 1 {
				t.Fatal("presentation misrouted")
			}
		})
	}
}
