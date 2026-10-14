package v2portrait

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func restorationDraftFixture(t *testing.T, operation, mode string) (
	RestorationQualificationPin, []byte, []byte, v2input.Ref, []v2input.Entry) {
	t.Helper()
	pin := restorationPin(operation)
	sha := strings.Repeat("b", 64)
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: sha} }
	source, target, guard := ref(), ref(), ref()
	params := map[string]any{"schema_version": 1, "model_lock_sha": pin.ModelLockSHA,
		"calibration": pin.Calibration, "base": map[string]any{"source": source},
		"protected": guard}
	entries := []v2input.Entry{{Ref: source, Contract: "WorkingImage"},
		{Ref: target, Contract: "MaskRef"}, {Ref: guard, Contract: "MaskRef"},
		{Ref: pin.Calibration, Contract: "Calibration"}}
	arrayCount := 3
	if operation == "image.reflection" {
		params["mask"] = target
	} else {
		proof, faces := ref(), ref()
		params["mode"] = mode
		params["glasses_mask"] = target
		params["confirmation"] = proof
		params["source_faces"] = faces
		entries = append(entries, v2input.Entry{Ref: proof, Contract: "DonorConfirmation"},
			v2input.Entry{Ref: faces, Contract: "FaceObservations"})
		if mode == "donor" {
			donor, donorFaces := ref(), ref()
			params["donor"] = map[string]any{"source": donor}
			params["donor_faces"] = donorFaces
			entries = append(entries, v2input.Entry{Ref: donor, Contract: "WorkingImage"},
				v2input.Entry{Ref: donorFaces, Contract: "FaceObservations"})
			arrayCount = 4
		}
	}
	for i := 0; i < arrayCount; i++ {
		entries = append(entries, v2input.Entry{Ref: ref(), Contract: "NpyArray"})
	}
	paramsRaw, err := canonical.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(paramsRaw)
	paramsRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	entries = append(entries, v2input.Entry{Ref: paramsRef, Contract: "Params"})
	entries = append(entries, v2input.Entry{Ref: pin.Report, Contract: "Report"})
	sort.Slice(entries, func(i, j int) bool { return entries[i].Ref.ArtifactID.String() < entries[j].Ref.ArtifactID.String() })
	_, profile := restorationProfileFixture(t, pin)
	return pin, paramsRaw, profile, paramsRef, entries
}

func TestRestorationDraftBindsDirectRootsAndDeterministicTask(t *testing.T) {
	for _, item := range []struct{ operation, mode string }{
		{"image.reflection", ""}, {"portrait.glare", "donor"},
		{"portrait.glare", "generative"}} {
		pin, params, profile, paramsRef, entries := restorationDraftFixture(t, item.operation, item.mode)
		workflow := uuid.New()
		first, err := CompileRestorationDraftTask(context.Background(), workflow, 0, item.operation,
			params, profile, pin, paramsRef, entries)
		if err != nil {
			t.Fatal(item, err)
		}
		second, err := CompileRestorationDraftTask(context.Background(), workflow, 0, item.operation,
			params, profile, pin, paramsRef, entries)
		if err != nil || first.Task.ID != second.Task.ID ||
			first.Task.InputDigest != second.Task.InputDigest ||
			first.Task.ResourceClass != "gpu" || first.Task.ParamsArtifactID != paramsRef.ArtifactID {
			t.Fatal("restoration draft identity changed", item, err)
		}
	}
}

func TestRestorationDraftRejectsMissingOrSubstitutedInventory(t *testing.T) {
	pin, params, profile, paramsRef, entries := restorationDraftFixture(t, "image.reflection", "")
	compile := func(items []v2input.Entry, p v2input.Ref) error {
		_, err := CompileRestorationDraftTask(context.Background(), uuid.New(), 0,
			"image.reflection", params, profile, pin, p, items)
		return err
	}
	if compile(entries[:len(entries)-1], paramsRef) == nil {
		t.Fatal("missing input accepted")
	}
	withoutReport := make([]v2input.Entry, 0, len(entries)-1)
	for _, entry := range entries {
		if entry.Contract != "Report" {
			withoutReport = append(withoutReport, entry)
		}
	}
	if compile(withoutReport, paramsRef) == nil {
		t.Fatal("qualification report missing from inventory")
	}
	changed := append([]v2input.Entry(nil), entries...)
	changed[0].Ref.SHA256 = strings.Repeat("c", 64)
	if compile(changed, paramsRef) == nil {
		t.Fatal("changed input accepted")
	}
	unsorted := append([]v2input.Entry(nil), entries...)
	unsorted[0], unsorted[1] = unsorted[1], unsorted[0]
	if compile(unsorted, paramsRef) == nil {
		t.Fatal("unsorted closure accepted")
	}
	wrong := paramsRef
	wrong.SHA256 = strings.Repeat("d", 64)
	if compile(entries, wrong) == nil {
		t.Fatal("wrong Params digest accepted")
	}
}
