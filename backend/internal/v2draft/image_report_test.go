package v2draft

import (
	"bytes"
	"github.com/google/uuid"
	"os"
	"os/exec"
	"path/filepath"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func reportFixture() (map[string]any, v2input.Ref) {
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	source := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref(), CanvasHash: strings.Repeat("b", 64)}
	return map[string]any{"schema_version": 1, "status": "review", "operation": "layer.compose", "skipped_count": 0, "reason": nil, "outside_mask_max_error": 0, "clipping_fraction": 0, "identity": nil, "transform": nil, "sources": []v2input.SessionVersionBinding{source}, "registrations": []v2input.Ref{}}, ref()
}

func TestImageReportTypedSourcesAndNullableEvidence(t *testing.T) {
	fields, parent := reportFixture()
	raw, _ := canonical.Marshal(fields)
	children, err := imageReportRefs(raw, "report", parent)
	if err != nil || len(children) != 1 || children[0].Contract != "WorkingImage" || children[0].CanvasHash != strings.Repeat("b", 64) || children[0].ParentRef != parent {
		t.Fatal(children, err)
	}
	fields["identity"] = parent
	fields["transform"] = parent
	fields["registrations"] = []v2input.Ref{parent}
	raw, _ = canonical.Marshal(fields)
	children, err = imageReportRefs(raw, "report", parent)
	if err != nil || len(children) != 4 || children[0].Contract != "IdentityReport" {
		t.Fatal(children, err)
	}
}

func TestImageReportRejectsInvalidWireFields(t *testing.T) {
	mutations := []func(map[string]any){
		func(f map[string]any) { delete(f, "identity") }, func(f map[string]any) { f["unknown"] = 1 },
		func(f map[string]any) { f["status"] = "approved" }, func(f map[string]any) { f["operation"] = "" },
		func(f map[string]any) { f["skipped_count"] = nil }, func(f map[string]any) { f["skipped_count"] = 5001 },
		func(f map[string]any) { f["clipping_fraction"] = nil }, func(f map[string]any) { f["clipping_fraction"] = 1.1 },
		func(f map[string]any) { f["outside_mask_max_error"] = -1 }, func(f map[string]any) { f["reason"] = "" },
		func(f map[string]any) { f["sources"] = []any{} }, func(f map[string]any) { f["registrations"] = nil },
		func(f map[string]any) { f["identity"] = map[string]any{} }, func(f map[string]any) {
			f["sources"] = []any{map[string]any{"asset_version_id": uuid.New(), "source": f["transform"], "canvas_hash": strings.Repeat("b", 64)}}
		},
	}
	for i, mutate := range mutations {
		fields, parent := reportFixture()
		mutate(fields)
		raw, _ := canonical.Marshal(fields)
		if _, err := imageReportRefs(raw, "report", parent); err == nil {
			t.Fatalf("accepted %d: %s", i, raw)
		}
	}
}

func TestImageReportParentReports(t *testing.T) {
	fields, root := reportFixture()
	child := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}
	fields["parent_reports"] = []v2input.Ref{child}
	raw, _ := canonical.Marshal(fields)
	refs, err := DocumentChildren(raw, "ImageReport", root)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ref := range refs {
		if ref.Contract == "ImageReport" && ref.Ref == child {
			found = true
		}
	}
	if !found {
		t.Fatal(refs)
	}
	for _, bad := range []any{nil, []v2input.Ref{child, child}, []v2input.Ref{root}} {
		fields["parent_reports"] = bad
		raw, _ = canonical.Marshal(fields)
		if _, err := DocumentChildren(raw, "ImageReport", root); err == nil {
			t.Fatal("invalid parents accepted", bad)
		}
	}
}

func TestImageReportWorkerWire(t *testing.T) {
	python := os.Getenv("PHOTO_WORKER_PYTHON")
	if python == "" {
		t.Skip("set PHOTO_WORKER_PYTHON for actual Python/Go wire verification")
	}
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	fields, root := reportFixture()
	child := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("d", 64)}
	for _, extended := range []bool{false, true} {
		if extended {
			fields["parent_reports"] = []v2input.Ref{child}
		}
		raw, _ := canonical.Marshal(fields)
		command := exec.Command(python, "-c", "import sys; from photo_worker.operations.optical.contracts import ImageReport; from photo_worker.contracts.base import canonical_bytes; raw=sys.stdin.buffer.read(); sys.stdout.buffer.write(canonical_bytes(ImageReport.model_validate_json(raw)))")
		command.Env = append(os.Environ(), "PYTHONPATH="+filepath.Join(repo, "workers")+string(os.PathListSeparator)+filepath.Join(repo, "apps", "local-editor", "src"))
		command.Stdin = bytes.NewReader(raw)
		wire, err := command.Output()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(wire, raw) {
			t.Fatal("worker wire changed", string(wire), string(raw))
		}
		refs, err := DocumentChildren(wire, "ImageReport", root)
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, ref := range refs {
			if ref.Contract == "ImageReport" && ref.Ref == child {
				count++
			}
		}
		if extended && count != 1 || !extended && count != 0 {
			t.Fatal(refs)
		}
	}
}

func TestParseImageReportRetainsTypedSourcesAndOwnsBytes(t *testing.T) {
	fields, parent := reportFixture()
	fields["parent_reports"] = []v2input.Ref{}
	raw, err := canonical.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	result, err := ParseImageReport(raw, parent)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "review" || len(result.Sources) != 1 || result.ParentReports == nil || result.Registrations == nil {
		t.Fatal("typed report fields lost")
	}
	raw[0] = 'x'
	if result.Operation != "layer.compose" {
		t.Fatal("typed report aliases bytes")
	}
	fields["unknown"] = true
	raw, err = canonical.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseImageReport(raw, parent); err == nil {
		t.Fatal("typed parser bypassed strict report validation")
	}
}
