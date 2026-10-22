package semanticreview

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func retouchResultRef(id string, raw []byte) v2input.Ref {
	sum := sha256.Sum256(raw)
	return v2input.Ref{ArtifactID: uuid.MustParse(id), SHA256: hex.EncodeToString(sum[:])}
}
func TestRetouchResultSharedPythonWire(t *testing.T) {
	paramsRaw, err := os.ReadFile("testdata/retouch_preview_params.json")
	if err != nil {
		t.Fatal(err)
	}
	params, err := ParseRetouchPreviewParams(paramsRaw)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("testdata/retouch_preview_evidence.json")
	if err != nil {
		t.Fatal(err)
	}
	ref := retouchResultRef("00000000-0000-0000-0000-0000000000d4", raw)
	evidence, err := DecodeRetouchPreviewEvidence(ref, raw, params)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range [][2]string{
		{`"alpha_unchanged":true`, `"alpha_unchanged":1`},
		{`"alpha_unchanged":true`, `"alpha_unchanged":false`},
		{`"alpha_unchanged":true`, `"alpha_unchanged":null`},
		{`"algorithm":"beautyfilter_v1"`, `"algorithm":"big_lama_work_srgb_delta_v1"`},
		{`"changed_pixels":4`, `"changed_pixels":65`},
		{`"roi":[0,0,8,8]`, `"roi":[0,0,200000001,8]`},
		{`"schema_version":1`, `"schema_version":1,"approval":true`},
	} {
		changed := bytes.Replace(raw, []byte(change[0]), []byte(change[1]), 1)
		if bytes.Equal(changed, raw) {
			t.Fatal("ineffective mutation", change)
		}
		if _, err := DecodeRetouchPreviewEvidence(retouchResultRef(ref.ArtifactID.String(), changed), changed, params); err == nil {
			t.Fatal("invalid evidence accepted", change)
		}
	}
	displayRaw, err := os.ReadFile("testdata/retouch_preview_presentation.json")
	if err != nil {
		t.Fatal(err)
	}
	expected := VerifiedRetouchPreviewEvidence{Evidence: evidence, Source: PlanSource{Ref: ref}}
	rootID := "00000000-0000-0000-0000-0000000000d9"
	if _, err := DecodeRetouchPreviewPresentation(retouchResultRef(rootID, displayRaw), displayRaw, expected,
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"); err != nil {
		t.Fatal(err)
	}
	for _, change := range [][2]string{{`"width":8`, `"width":2561`}, {`"overlay":"max_coverage_ceil_alpha_v1"`, `"overlay":"other"`}, {`"kind":"retouch_region"`, `"kind":"background_cleanup"`}} {
		changed := bytes.Replace(displayRaw, []byte(change[0]), []byte(change[1]), 1)
		if bytes.Equal(changed, displayRaw) {
			t.Fatal("ineffective display mutation")
		}
		if _, err := DecodeRetouchPreviewPresentation(retouchResultRef(rootID, changed), changed, expected,
			"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"); err == nil {
			t.Fatal("invalid display accepted")
		}
	}
}

func TestRetouchPresentationRejectsRehashedLinkageForgery(t *testing.T) {
	read := func(name string) []byte { b, err := os.ReadFile("testdata/"+name); if err != nil { t.Fatal(err) }; return b }
	params, err := ParseRetouchPreviewParams(read("retouch_preview_params.json"))
	if err != nil { t.Fatal(err) }
	evidenceRaw := read("retouch_preview_evidence.json")
	evidenceRef := retouchResultRef("00000000-0000-0000-0000-0000000000d4", evidenceRaw)
	evidence, err := DecodeRetouchPreviewEvidence(evidenceRef,evidenceRaw,params)
	if err != nil { t.Fatal(err) }
	expected := VerifiedRetouchPreviewEvidence{Evidence:evidence,Source:PlanSource{Ref:evidenceRef}}
	rootID := "00000000-0000-0000-0000-0000000000d9"
	for _, mode := range []string{"evidence", "image", "pixels", "duplicate_media", "source_alias", "root_alias", "nil_media", "bad_sha", "params", "icc", "srgb", "resize", "width_zero", "height_max", "noncanonical"} {
		t.Run(mode,func(t *testing.T) {
			var display RetouchPreviewPresentation
			if err := json.Unmarshal(read("retouch_preview_presentation.json"),&display); err != nil { t.Fatal(err) }
			switch mode {
			case "evidence": display.NumericalEvidence.ArtifactID = uuid.New()
			case "image": display.WorkingImage.ArtifactID = uuid.New()
			case "pixels": display.Pixels.ArtifactID = uuid.New()
			case "duplicate_media": display.After = display.Before
			case "source_alias": display.Protected = params.Source.Source
			case "root_alias": display.Affected.ArtifactID = uuid.MustParse(rootID)
			case "nil_media": display.Before.ArtifactID = uuid.Nil
			case "bad_sha": display.After.SHA256 = "invalid"
			case "params": display.Params.TargetRevision = "1"
			case "icc": display.ICCLockSHA = params.Plan.SHA256
			case "srgb": display.SRGBSHA = params.Plan.SHA256
			case "resize": display.Resize = "nearest"
			case "width_zero": display.Width = 0
			case "height_max": display.Height = 2561
			}
			raw, err := canonical.Marshal(display)
			if err != nil { t.Fatal(err) }
			if mode == "noncanonical" { raw = append(raw,' ') }
			got, err := DecodeRetouchPreviewPresentation(retouchResultRef(rootID,raw),raw,expected,
				"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc")
			if err == nil || got.SchemaVersion != 0 { t.Fatal("rehashed forged linkage accepted",mode) }
		})
	}
}
