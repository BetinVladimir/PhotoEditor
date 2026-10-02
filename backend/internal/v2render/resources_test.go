package v2render

import "testing"

func TestLayerResourcesWorkerParity(t *testing.T) {
	got, err := LayerResources(2, 1, 1, 1, 512)
	want := LayerResourceEstimate{PixelCount: 2, RAMBytes: 268436112, ScratchBytes: 67109008, OutputBytes: 33554480}
	if err != nil || got != want {
		t.Fatalf("got %+v %v want %+v", got, err, want)
	}
	small, _ := LayerResources(20000, 10000, 1, 1, 512)
	large, err := LayerResources(20000, 10000, 128, 256, 512)
	if err != nil || large.RAMBytes-small.RAMBytes != 4*127*200000000+(4*259-256)*512*512 || large.ScratchBytes-small.ScratchBytes != 4*127*200000000 || large.OutputBytes != 24*200000000+32*1024*1024 {
		t.Fatalf("large estimates: %+v %+v %v", small, large, err)
	}
}

func TestLayerResourcesRejectsInvalidAndOverflowDimensions(t *testing.T) {
	for _, args := range [][5]int64{{0, 1, 1, 1, 512}, {1, 0, 1, 1, 512}, {200000001, 1, 1, 1, 512}, {9223372036854775807, 2, 1, 1, 512}, {1, 1, 0, 1, 512}, {1, 1, 258, 1, 512}, {1, 1, 1, 257, 512}, {1, 1, 1, 1, 128}} {
		if _, err := LayerResources(args[0], args[1], int(args[2]), int(args[3]), int(args[4])); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
