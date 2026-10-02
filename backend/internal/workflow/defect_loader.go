package workflow

import (
	"bytes"
	"context"
	"errors"

	"photoeditor/backend/internal/review"
)

type DefectArtifactReader interface {
	Context(context.Context, DefectExpansionLease) (DefectContextRefs, error)
	JSON(context.Context, DefectExpansionLease, review.ArtifactRef) ([]byte, ExpansionArtifact, error)
	Lookup(context.Context, DefectExpansionLease, review.ArtifactRef) (ExpansionArtifact, error)
}

// DefectBranchBuilder is the lease processor's Build implementation. Its reader
// must enforce tenant ownership and the live lease on every access. Context must
// attest the resolved/report producers against the immutable saved review.
type DefectBranchBuilder struct {
	Artifacts DefectArtifactReader
	Publisher ContractPublisher
}

func (b DefectBranchBuilder) Build(ctx context.Context, lease DefectExpansionLease) (MaterializedDefectBranch, *PreparedDefectTask, error) {
	fail := func(err error) (MaterializedDefectBranch, *PreparedDefectTask, error) {
		return MaterializedDefectBranch{}, nil, err
	}
	invalid := errors.New("invalid defect expansion source")
	if b.Artifacts == nil || b.Publisher == nil {
		return fail(invalid)
	}
	read := func(ref review.ArtifactRef, operation, role string) ([]byte, error) {
		raw, meta, err := b.Artifacts.JSON(ctx, lease, ref)
		if err != nil {
			return nil, err
		}
		if operation != "" && (!meta.Canonical || meta.Producer != operation || meta.Role != role) {
			return nil, invalid
		}
		return raw, nil
	}
	raw, err := read(lease.Selection.Image, "", "")
	if err != nil {
		return fail(err)
	}
	image, err := review.ParseWorkingImage(raw)
	if err != nil {
		return fail(err)
	}
	raw, err = read(lease.Selection.Proposal, "defects.propose", "defect_candidates")
	if err != nil {
		return fail(err)
	}
	proposal, err := review.ReadDefectProposal(ctx, bytes.NewReader(raw), lease.Selection.Proposal, image.Source(lease.Selection.Image))
	if err != nil {
		return fail(err)
	}
	input := DefectExpansionInput{TenantID: lease.TenantID, RunID: lease.RunID, ExpansionID: lease.ExpansionID, ReviewID: lease.ReviewID, Route: lease.Route, Selection: lease.Selection, Proposal: proposal, Alpha: image.Alpha}
	if lease.Route == "repair" {
		refs, err := b.Artifacts.Context(ctx, lease)
		if err != nil {
			return fail(err)
		}
		raw, err = read(refs.Resolved, "style.resolve", "resolved_style")
		if err != nil {
			return fail(err)
		}
		resolved, err := review.ParseResolvedBinding(raw)
		if err != nil {
			return fail(err)
		}
		if resolved.Mode == "skip" {
			return fail(invalid)
		}
		origin := image
		if resolved.Image != lease.Selection.Image {
			raw, err = read(resolved.Image, "", "")
			if err != nil {
				return fail(err)
			}
			origin, err = review.ParseWorkingImage(raw)
			if err != nil {
				return fail(err)
			}
		}
		if image.DType != "float32" || image.Color.ProfileID != "work_rec2020_gamma24_v1" || image.Color != origin.Color || image.Canvas.SourceSHA256 != origin.Canvas.SourceSHA256 {
			return fail(invalid)
		}
		raw, err = read(refs.Report, "defects.propose", "defect_propose_report")
		if err != nil {
			return fail(err)
		}
		protection, err := review.ParseProposalProtection(raw, lease.Selection.Proposal, image.Source(lease.Selection.Image))
		if err != nil {
			return fail(err)
		}
		for _, item := range []struct {
			ref  review.ArtifactRef
			role string
		}{{protection.Artifact, "defect_protection"}, {lease.Selection.LabelMap, "defect_labels"}} {
			meta, err := b.Artifacts.Lookup(ctx, lease, item.ref)
			if err != nil {
				return fail(err)
			}
			if !meta.Canonical || meta.Producer != "defects.propose" || meta.Role != item.role {
				return fail(invalid)
			}
		}
		input.Resolved = refs.Resolved
		input.ResolvedImage = resolved.Image
		input.Protection = protection
	}
	branch, err := MaterializeDefectBranch(ctx, b.Publisher, input)
	if err != nil {
		return fail(err)
	}
	if branch.Route != "repair" {
		return branch, nil, nil
	}
	sizes := make([]ArtifactSize, 0, len(branch.Inputs)+1)
	for _, ref := range append(append([]review.ArtifactRef{}, branch.Inputs...), *branch.Params) {
		meta, err := b.Artifacts.Lookup(ctx, lease, ref)
		if err != nil {
			return fail(err)
		}
		sizes = append(sizes, ArtifactSize{Ref: ref, Bytes: meta.Object.Bytes})
	}
	task, err := PrepareDefectTask(lease.RunID, lease.ReviewID, image.Canvas, branch, sizes)
	if err != nil {
		return fail(err)
	}
	return branch, &task, nil
}
