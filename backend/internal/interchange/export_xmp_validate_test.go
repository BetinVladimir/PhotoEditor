package interchange

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func exportXMPTestPayload(entry importPlanEntry, orientation int) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><x:xmpmeta xmlns:x="adobe:ns:meta/" xmlns:rdf="%s" xmlns:xmp="http://ns.adobe.com/xap/1.0/" xmlns:pe="https://photoeditor.invalid/ns/1.0/" xmlns:tiff="http://ns.adobe.com/tiff/1.0/"><rdf:RDF><rdf:Description rdf:about="" xmp:Rating="%d" pe:Pick="%s" tiff:Orientation="%d"/></rdf:RDF></x:xmpmeta>`, exportXMPRDF, entry.Rating, entry.Pick, orientation)
}
func TestExportXMPValidatesGeneratedNamespacesMetadataAndOrientations(t *testing.T) {
	entry := importPlanEntry{Rating: 4, Pick: "picked"}
	for orientation := 1; orientation <= 8; orientation++ {
		got, err := validateExportXMP(context.Background(), []byte(exportXMPTestPayload(entry, orientation)), entry)
		if err != nil || got != orientation {
			t.Fatal("valid orientation refused", orientation, err)
		}
	}
	raw := exportXMPTestPayload(entry, 1)
	for _, bad := range []string{
		strings.Replace(raw, `xmp:Rating="4"`, `xmp:Rating="5"`, 1),
		strings.Replace(raw, `pe:Pick="picked"`, `pe:Pick="unmarked"`, 1),
		strings.Replace(raw, `tiff:Orientation="1"`, `tiff:Orientation="9"`, 1),
		strings.Replace(raw, `xmp:Rating="4"`, `xmp:Rating="4" xmp:Rating="4"`, 1),
		strings.Replace(raw, `<rdf:RDF>`, `<!DOCTYPE x><rdf:RDF>`, 1),
		strings.Replace(raw, `http://ns.adobe.com/xap/1.0/`, `https://foreign.invalid/`, 1),
		strings.Replace(raw, `rdf:about=""`, `rdf:about="" foreign="unsupported"`, 1),
	} {
		if _, err := validateExportXMP(context.Background(), []byte(bad), entry); err == nil {
			t.Fatal("invalid XMP accepted")
		}
	}
}
func TestExportXMPCropMustMatchFrozenCanonicalAxes(t *testing.T) {
	entry := importPlanEntry{Rating: 4, Pick: "picked", Crop: &importCrop{X0: .1, Y0: .2, X1: .7, Y1: .8}}
	raw := exportXMPTestPayload(entry, 1)
	raw = strings.Replace(raw, `xmlns:tiff=`, `xmlns:crs="http://ns.adobe.com/camera-raw-settings/1.0/" xmlns:tiff=`, 1)
	raw = strings.Replace(raw, `tiff:Orientation="1"`, `tiff:Orientation="1" crs:CropLeft="0.1" crs:CropTop="0.2" crs:CropRight="0.7" crs:CropBottom="0.8"`, 1)
	if _, err := validateExportXMP(context.Background(), []byte(raw), entry); err != nil {
		t.Fatal("valid crop refused", err)
	}
	bad := strings.Replace(raw, `CropLeft="0.1"`, `CropLeft="0.3"`, 1)
	if _, err := validateExportXMP(context.Background(), []byte(bad), entry); err == nil {
		t.Fatal("changed crop accepted")
	}
}

func TestExportXMPColorLabelMustMatchFrozenPlan(t *testing.T) {
	label := "Blue"
	entry := importPlanEntry{Rating: 4, Pick: "picked", Label: &label}
	raw := strings.Replace(exportXMPTestPayload(entry, 1), `pe:Pick="picked"`, `pe:Pick="picked" xmp:Label="Blue"`, 1)
	if _, err := validateExportXMP(context.Background(), []byte(raw), entry); err != nil {
		t.Fatal("matching label refused", err)
	}
	for _, bad := range []string{strings.Replace(raw, `xmp:Label="Blue"`, `xmp:Label="Red"`, 1), strings.Replace(raw, ` xmp:Label="Blue"`, "", 1)} {
		if _, err := validateExportXMP(context.Background(), []byte(bad), entry); err == nil {
			t.Fatal("mismatched label admitted")
		}
	}
	entry.Label = nil
	if _, err := validateExportXMP(context.Background(), []byte(raw), entry); err == nil {
		t.Fatal("unplanned label admitted")
	}
}

func TestExportXMPCropAllEightEXIFOrientations(t *testing.T) {
	entry := importPlanEntry{Rating: 4, Pick: "picked", Crop: &importCrop{X0: .1, Y0: .2, X1: .7, Y1: .8}}
	// Independent expected forward rectangles in oriented axes.
	rectangles := [][4]float64{{.1, .2, .7, .8}, {.3, .2, .9, .8}, {.3, .2, .9, .8}, {.1, .2, .7, .8}, {.2, .1, .8, .7}, {.2, .1, .8, .7}, {.2, .3, .8, .9}, {.2, .3, .8, .9}}
	for i, crop := range rectangles {
		raw := exportXMPTestPayload(entry, i+1)
		raw = strings.Replace(raw, `xmlns:tiff=`, `xmlns:crs="http://ns.adobe.com/camera-raw-settings/1.0/" xmlns:tiff=`, 1)
		attrs := fmt.Sprintf(` crs:CropLeft="%.17g" crs:CropTop="%.17g" crs:CropRight="%.17g" crs:CropBottom="%.17g"`, crop[0], crop[1], crop[2], crop[3])
		raw = strings.Replace(raw, `/></rdf:RDF>`, attrs+`/></rdf:RDF>`, 1)
		got, err := validateExportXMP(context.Background(), []byte(raw), entry)
		if err != nil || got != i+1 {
			t.Fatal("oriented crop refused", i+1, err)
		}
	}
}
