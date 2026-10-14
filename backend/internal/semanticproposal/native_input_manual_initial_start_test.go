package semanticproposal

import (
	"context"
	"testing"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

func TestManualDefectBootstrapRejectsMissingServerDependencies(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, current := range []context.Context{ctx, context.Background()} {
		out, err := (nativePreparationBootstrap{}).bootstrapManualDefect(current, auth.Identity{}, nativeInputRestoredFaceProducers{}, TransferColorProfile{}, nil, [2]float64{}, v2input.Ref{}, nativePreparationBudget{})
		if err == nil || out.plan.seal != "" || out.ticket != (v2input.JobTicket{}) {
			t.Fatal("incomplete bootstrap exposed submission", out, err)
		}
		if current == ctx && err != context.Canceled {
			t.Fatal(err)
		}
	}
}

func TestInitialManualResultCatalogRejectsIncompletePlan(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, current := range []context.Context{ctx, context.Background()} {
		out, err := (SQLTransferSourceReader{}).nativeInputManualInitialResultCatalog(current, auth.Identity{}, nativeInputManualInitialDraftPlan{})
		if err == nil || out.ResultID != (v2input.Ref{}).ArtifactID || out.Outputs != nil || out.Params != (v2input.Ref{}) {
			t.Fatal("incomplete manual plan exposed result", out, err)
		}
		if current == ctx && err != context.Canceled {
			t.Fatal(err)
		}
	}
}

func TestInitialManualResultObjectsRejectsIncompletePlan(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, current := range []context.Context{ctx, context.Background()} {
		out, err := (SQLTransferSourceReader{}).nativeInputManualInitialResultObjects(current, auth.Identity{}, nativeInputManualInitialDraftPlan{}, nativeInputDefectResultCatalog{})
		if err == nil || out != ([6]defectEvidenceObject{}) {
			t.Fatal("incomplete plan exposed object pins", out, err)
		}
		if current == ctx && err != context.Canceled {
			t.Fatal(err)
		}
	}
}

func TestInitialManualCompletedContentRejectsIncompletePlan(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, current := range []context.Context{ctx, context.Background()} {
		out, err := (SQLTransferSourceReader{}).readCompletedNativeInputManualInitialDefect(current, auth.Identity{}, nativeInputManualInitialDraftPlan{}, TransferColorProfile{})
		if err == nil || out.catalog.Outputs != nil || out.objects != ([6]defectEvidenceObject{}) || out.evidence.Params != nil {
			t.Fatal("incomplete plan exposed physical evidence", err)
		}
		if current == ctx && err != context.Canceled {
			t.Fatal(err)
		}
	}
}

func TestInitialManualProtectionRefusesIncompleteAuthority(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := SQLTransferSourceReader{}
	for _, current := range []context.Context{ctx, context.Background()} {
		out, err := reader.prepareNativeInputManualInitialProtection(current, auth.Identity{}, nativeInputManualInitialDraftPlan{}, TransferColorProfile{})
		if err == nil || out.body != "" || out.pixels != (v2input.Ref{}) {
			t.Fatal("incomplete authority exposed protection", err)
		}
		if current == ctx && err != context.Canceled {
			t.Fatal(err)
		}
		published, err := reader.publishNativeInputManualInitialProtection(current, auth.Identity{}, nativeInputManualInitialDraftPlan{}, TransferColorProfile{}, v2draft.Publisher{})
		if err == nil || published.root != (v2input.Ref{}) {
			t.Fatal("incomplete authority exposed published root", err)
		}
		if current == ctx && err != context.Canceled {
			t.Fatal(err)
		}
	}
}

func TestRegisteredManualProtectionRefusesIncompleteAuthority(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, current := range []context.Context{ctx, context.Background()} {
		out, err := (SQLTransferSourceReader{}).readRegisteredNativeInputManualInitialProtection(current, auth.Identity{}, publishedNativeInputManualInitialProtection{}, TransferColorProfile{})
		if err == nil || out.verified.Root != (v2input.Ref{}) || out.objects != ([2]defectEvidenceObject{}) {
			t.Fatal("incomplete authority exposed registered protection", err)
		}
		if current == ctx && err != context.Canceled {
			t.Fatal(err)
		}
	}
}
