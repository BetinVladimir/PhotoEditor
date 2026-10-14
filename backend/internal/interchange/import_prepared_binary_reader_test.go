package interchange

import (
	"context"
	"testing"
)

func TestPreparedImportArraysRequirePhysicalAndNativeProof(t *testing.T) {
	for _, kind := range []string{"MaskPixels", "DenseWarpField", "DefectLabelMap"} {
		for _, mode := range []string{"valid", "hash", "version", "native", "missing_native", "cancelled", "opaque_role"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				entry, catalog, objects := importOpaqueFixture()
				entry.Contract = kind
				native := &exportBinaryQualifierStub{}
				var qualifier ExportBinaryQualifier = native
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				switch mode {
				case "hash":
					objects.body[0] = '!'
				case "version":
					catalog.change = true
				case "native":
					native.fail = true
				case "missing_native":
					qualifier = nil
				case "cancelled":
					cancel()
				case "opaque_role":
					entry.Contract = "Artifact"
				}
				node, err := (ImportPreparedBinaryNodeResolver{Catalog: catalog, Objects: objects, Qualifier: qualifier, MaxBytes: 1024}).ResolveImportNode(ctx, entry)
				if (err == nil) != (mode == "valid") {
					t.Fatal("unexpected prepared binary qualification", err)
				}
				if mode == "valid" {
					if node.Entry != entry || node.Body != nil || native.calls != 1 || objects.opens != 1 || catalog.calls != 5 {
						t.Fatal("physical/native/current checks missing")
					}
				}
				if mode == "hash" || mode == "version" {
					if native.calls != 0 {
						t.Fatal("native called on invalid physical object")
					}
				}
				if mode == "missing_native" || mode == "cancelled" || mode == "opaque_role" {
					if objects.opens != 0 || catalog.calls != 0 {
						t.Fatal("invalid reader performed IO")
					}
				}
				if mode == "cancelled" && err != context.Canceled {
					t.Fatal("cancellation lost", err)
				}
			})
		}
	}
}
