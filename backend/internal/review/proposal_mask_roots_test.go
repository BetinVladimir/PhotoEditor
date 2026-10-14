package review

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/canonical"
	"testing"
)

func TestResolveCompleteFaceMaskRoots(t *testing.T) {
	raw, err := os.ReadFile("testdata/proposal-report-multiple-faces-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var report proposalReportDocument
	if err := decodeExact(raw, &report); err != nil {
		t.Fatal(err)
	}
	source := DefectSource{Image: report.Params.Image, Canvas: report.Protection.Canvas, TransformChain: report.Protection.TransformChain}
	calls := 0
	loader := func(ctx context.Context, binding ProposalFaceMask) (ArtifactRef, []byte, error) {
		if binding.Ordinal != calls {
			t.Fatal("incorrect inventory order")
		}
		calls++
		body, err := canonical.Marshal(binding.Mask)
		if err != nil {
			return ArtifactRef{}, nil, err
		}
		sum := sha256.Sum256(body)
		return ArtifactRef{ID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}, body, nil
	}
	result, err := ResolveProposalFaceMaskRoots(context.Background(), raw, report.Proposal, source, loader)
	if err != nil || len(result) != 2 || calls != 2 {
		t.Fatal(result, err, calls)
	}
	calls = 0
	sentinel := errors.New("missing original root")
	broken := func(ctx context.Context, binding ProposalFaceMask) (ArtifactRef, []byte, error) {
		if binding.Ordinal == 1 {
			return ArtifactRef{}, nil, sentinel
		}
		return loader(ctx, binding)
	}
	if got, err := ResolveProposalFaceMaskRoots(context.Background(), raw, report.Proposal, source, broken); !errors.Is(err, sentinel) || got != nil {
		t.Fatal("partial result", got, err)
	}
	if got, err := ResolveProposalFaceMaskRoots(context.Background(), raw, report.Proposal, source, nil); err == nil || got != nil {
		t.Fatal("missing loader accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	calls = 0
	cancelled := func(ctx context.Context, binding ProposalFaceMask) (ArtifactRef, []byte, error) {
		ref, body, err := loader(ctx, binding)
		cancel()
		return ref, body, err
	}
	if got, err := ResolveProposalFaceMaskRoots(ctx, raw, report.Proposal, source, cancelled); !errors.Is(err, context.Canceled) || got != nil || calls != 1 {
		t.Fatal("cancellation", got, err, calls)
	}
	// A loader must not be able to rewrite the expected object/geometry through
	// shared pointers and pass a different producer document for that same mask.
	mutated := func(ctx context.Context, binding ProposalFaceMask) (ArtifactRef, []byte, error) {
		*binding.Mask.ObjectID = uuid.New()
		binding.Mask.TransformChain[0] = 'x'
		body, _ := canonical.Marshal(binding.Mask)
		sum := sha256.Sum256(body)
		return ArtifactRef{ID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}, body, nil
	}
	if got, err := ResolveProposalFaceMaskRoots(context.Background(), raw, report.Proposal, source, mutated); err == nil || got != nil {
		t.Fatal("mutable callback changed expected evidence")
	}
	calls = 0
	if _, err := ResolveProposalFaceMaskRoots(context.Background(), raw, report.Proposal, source, loader); err != nil {
		t.Fatal("caller bytes mutated", err)
	}
}
