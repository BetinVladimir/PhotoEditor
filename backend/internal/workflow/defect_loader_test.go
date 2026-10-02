package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"strings"
	"testing"
)

type loaderReader struct {
	raw  map[uuid.UUID][]byte
	meta map[uuid.UUID]ExpansionArtifact
	refs DefectContextRefs
	fail bool
}

func (r *loaderReader) Context(context.Context, DefectExpansionLease) (DefectContextRefs, error) {
	return r.refs, nil
}
func (r *loaderReader) JSON(_ context.Context, _ DefectExpansionLease, a review.ArtifactRef) ([]byte, ExpansionArtifact, error) {
	v, ok := r.raw[a.ID]
	if !ok {
		return nil, ExpansionArtifact{}, errors.New("missing fixture")
	}
	return v, r.meta[a.ID], nil
}
func (r *loaderReader) Lookup(_ context.Context, _ DefectExpansionLease, a review.ArtifactRef) (ExpansionArtifact, error) {
	if r.fail {
		return ExpansionArtifact{}, errors.New("lost lease")
	}
	if m, ok := r.meta[a.ID]; ok {
		return m, nil
	}
	return ExpansionArtifact{Object: objectstore.Object{Bytes: 4096}}, nil
}
func loaderFixture(t *testing.T) (DefectExpansionLease, *loaderReader) {
	t.Helper()
	input := expansionFixture(t)
	lease := DefectExpansionLease{ExpansionID: input.ExpansionID, Token: uuid.New(), TenantID: input.TenantID, RunID: input.RunID, ReviewID: input.ReviewID, ParentTaskID: uuid.New(), Route: "repair", Selection: input.Selection}
	read := func(n string) []byte {
		b, e := os.ReadFile("../review/testdata/" + n)
		if e != nil {
			t.Fatal(e)
		}
		return b
	}
	var p struct {
		Pixels review.ArtifactRef `json:"source_pixels"`
		Canvas review.Canvas      `json:"canvas"`
		Chain  json.RawMessage    `json:"transform_chain"`
	}
	json.Unmarshal(read("defect-proposal-python.json"), &p)
	img := review.WorkingImage{Pixels: p.Pixels, Canvas: p.Canvas, TransformChain: p.Chain, DType: "float32", Color: review.ColorSpace{ProfileID: "work_rec2020_gamma24_v1", Encoding: "gamma24", ICCSHA256: strings.Repeat("a", 64)}}
	imageRaw, _ := canonical.Marshal(img)
	var style map[string]any
	json.Unmarshal(read("resolved-binding-python.json"), &style)
	style["params"].(map[string]any)["image"] = input.Selection.Image
	style["mode"] = "fixed"
	delete(style, "hash")
	body, _ := canonical.Marshal(style)
	hash := sha256.Sum256(body)
	style["hash"] = hex.EncodeToString(hash[:])
	styleRaw, _ := canonical.Marshal(style)
	report := review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("c", 64)}
	r := &loaderReader{refs: DefectContextRefs{Resolved: input.Resolved, Report: report}, raw: map[uuid.UUID][]byte{input.Selection.Image.ID: imageRaw, input.Selection.Proposal.ID: read("defect-proposal-python.json"), input.Resolved.ID: styleRaw, report.ID: read("proposal-report-python.json")}, meta: map[uuid.UUID]ExpansionArtifact{}}
	for _, v := range []struct {
		ref      review.ArtifactRef
		op, role string
	}{{input.Selection.Proposal, "defects.propose", "defect_candidates"}, {input.Resolved, "style.resolve", "resolved_style"}, {report, "defects.propose", "defect_propose_report"}, {input.Protection.Artifact, "defects.propose", "defect_protection"}, {input.Selection.LabelMap, "defects.propose", "defect_labels"}} {
		r.meta[v.ref.ID] = ExpansionArtifact{Object: objectstore.Object{Bytes: 4096}, Producer: v.op, Role: v.role, Canonical: true}
	}
	return lease, r
}
func TestDefectLoaderBuildsCompleteTask(t *testing.T) {
	lease, r := loaderFixture(t)
	p := new(documentPublisher)
	branch, task, err := (DefectBranchBuilder{Artifacts: r, Publisher: p}).Build(context.Background(), lease)
	if err != nil {
		t.Fatal(err)
	}
	if task == nil || branch.Params == nil || task.Params != *branch.Params || len(p.calls) != 2 {
		t.Fatal("incomplete task")
	}
}
func TestDefectLoaderRejectsUntrustedProducerBeforePublishing(t *testing.T) {
	lease, r := loaderFixture(t)
	m := r.meta[r.refs.Report.ID]
	m.Canonical = false
	r.meta[r.refs.Report.ID] = m
	p := new(documentPublisher)
	_, task, err := (DefectBranchBuilder{Artifacts: r, Publisher: p}).Build(context.Background(), lease)
	if err == nil || task != nil || len(p.calls) != 0 {
		t.Fatal("untrusted report published")
	}
}
func TestDefectLoaderBaseDoesNotLoadStyleOrPublish(t *testing.T) {
	lease, r := loaderFixture(t)
	lease.Route = "base"
	lease.Selection.Accepted = []string{}
	delete(r.raw, r.refs.Resolved.ID)
	p := new(documentPublisher)
	branch, task, err := (DefectBranchBuilder{Artifacts: r, Publisher: p}).Build(context.Background(), lease)
	if err != nil || task != nil || branch.BaseImage == nil || len(p.calls) != 0 {
		t.Fatalf("base branch: %v", err)
	}
}

func TestDefectLoaderRejectsSourceAndLeaseFailures(t *testing.T) {
	for _, scenario := range []string{"wrong color", "wrong dtype", "missing origin", "protection provenance", "lookup lease lost"} {
		t.Run(scenario, func(t *testing.T) {
			lease, r := loaderFixture(t)
			publisher := new(documentPublisher)
			switch scenario {
			case "wrong color", "wrong dtype":
				var img review.WorkingImage
				json.Unmarshal(r.raw[lease.Selection.Image.ID], &img)
				if scenario == "wrong dtype" {
					img.DType = "uint16"
				} else {
					img.Color.ProfileID = "linear_srgb_v1"
					img.Color.Encoding = "linear"
				}
				r.raw[lease.Selection.Image.ID], _ = canonical.Marshal(img)
			case "missing origin":
				var style map[string]any
				json.Unmarshal(r.raw[r.refs.Resolved.ID], &style)
				style["params"].(map[string]any)["image"] = review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("b", 64)}
				delete(style, "hash")
				body, _ := canonical.Marshal(style)
				sum := sha256.Sum256(body)
				style["hash"] = hex.EncodeToString(sum[:])
				r.raw[r.refs.Resolved.ID], _ = canonical.Marshal(style)
			case "protection provenance":
				for id, meta := range r.meta {
					if meta.Role == "defect_protection" {
						meta.Producer = "mask.rasterize"
						r.meta[id] = meta
					}
				}
			case "lookup lease lost":
				r.fail = true
			}
			branch, task, err := (DefectBranchBuilder{Artifacts: r, Publisher: publisher}).Build(context.Background(), lease)
			if err == nil || task != nil || branch.Params != nil || len(publisher.calls) != 0 {
				t.Fatal("invalid source produced a partial task")
			}
		})
	}
}
