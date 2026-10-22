package v2render

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

func TestRenderResourcesProviderAndLimits(t *testing.T) {
	peak := RenderProviderPeak{RAMBytes: 123, ScratchBytes: 234, VRAMBytes: 345, OutputBytes: 456}
	base, err := RenderResources(6000, 4000, 1, 2, 512, "require_float32", RenderProviderPeak{})
	if err != nil {
		t.Fatal(err)
	}
	actual, err := RenderResources(6000, 4000, 1, 2, 512, "require_float32", peak)
	if err != nil || actual.PixelCount != 24000000 || actual.RAMBytes != base.RAMBytes+123 || actual.ScratchBytes != base.ScratchBytes+234 || actual.VRAMBytes != 345 || actual.OutputBytes != base.OutputBytes+456 {
		t.Fatal(actual, err)
	}
	if _, err := RenderResources(6000, 4000, 1, 2, 512, "require_float32", RenderProviderPeak{RAMBytes: 9007199254740991}); err == nil {
		t.Fatal("overflow accepted")
	}
	if _, err := RenderResources(200000001, 1, 0, 0, 512, "require_float32", peak); err == nil {
		t.Fatal("canvas accepted")
	}
}

func TestRenderResourcesActualWorkerParity(t *testing.T) {
	python := os.Getenv("PHOTO_WORKER_PYTHON")
	if python == "" {
		t.Skip("set PHOTO_WORKER_PYTHON for actual worker formula verification")
	}
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	peak := RenderProviderPeak{RAMBytes: 123, ScratchBytes: 234, VRAMBytes: 345, OutputBytes: 456}
	for _, size := range [][2]int64{{1, 1}, {6000, 4000}, {199999999, 1}} {
		for _, tile := range []int{256, 512, 1024} {
			for _, mode := range []string{"require_float32", "normalize_uint16"} {
				expected, err := RenderResources(size[0], size[1], 257, 512, tile, mode, peak)
				if err != nil {
					t.Fatal(err)
				}
				command := exec.Command(python, "-c", "import sys,json; from photo_worker.v2.render_resources import render_resources; print(json.dumps(render_resources(int(sys.argv[1]),int(sys.argv[2]),mask_refs=257,mask_nodes=512,tile_size=int(sys.argv[3]),alpha_mode=sys.argv[4],provider_peak=dict(ram_bytes=123,scratch_bytes=234,vram_bytes=345,output_bytes=456))))", strconv.FormatInt(size[0], 10), strconv.FormatInt(size[1], 10), strconv.Itoa(tile), mode)
				command.Env = append(os.Environ(), "PYTHONPATH="+filepath.Join(repo, "workers")+string(os.PathListSeparator)+filepath.Join(repo, "apps", "local-editor", "src"))
				raw, err := command.Output()
				if err != nil {
					t.Fatal(err)
				}
				var actual LayerResourceEstimate
				if err := json.Unmarshal(raw, &actual); err != nil {
					t.Fatal(err)
				}
				if actual != expected {
					t.Fatal(size, tile, mode, actual, expected)
				}
			}
		}
	}
}
