package semanticreview

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLocalExposureMemoryBounds(t *testing.T) {
	for _, sample := range []struct{ width, height, want int64 }{{1, 1, 96}, {6000, 4000, 2304000000}, {200000000, 1, 19200000000}} {
		got, err := LocalExposureMemory(sample.width, sample.height)
		if err != nil || got != sample.want {
			t.Fatal(got, err)
		}
	}
	for _, shape := range [][2]int64{{0, 1}, {1, 0}, {-1, 1}, {1, 200000001}, {9223372036854775807, 9223372036854775807}} {
		if _, err := LocalExposureMemory(shape[0], shape[1]); err == nil {
			t.Fatal("invalid or overflowing shape admitted")
		}
	}
}

func TestLocalExposureMemoryMatchesInstalledWorker(t *testing.T) {
	python := os.Getenv("PHOTO_TEST_WORKER_PYTHON")
	if python == "" {
		t.Skip("explicit installed worker Python required for cross-language integration")
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(python, "-c", `import json,sys
sys.path.insert(0,'workers')
from photo_worker.operations.semantic.local_exposure import local_exposure_memory
print(json.dumps([local_exposure_memory(s) for s in [(1,1),(4000,6000),(257,513),(1,200000000)]]))`)
	command.Dir = root
	output, err := command.Output()
	if err != nil {
		t.Fatal("installed worker cross-language verification failed", err)
	}
	var values []int64
	if json.Unmarshal(output, &values) != nil || len(values) != 4 {
		t.Fatal("worker estimate output invalid")
	}
	for n, shape := range [][2]int64{{1, 1}, {6000, 4000}, {513, 257}, {200000000, 1}} {
		got, err := LocalExposureMemory(shape[0], shape[1])
		if err != nil || got != values[n] {
			t.Fatal("admission/worker memory mismatch", got, values[n], err)
		}
	}
}

func TestLocalExposureExecutionMemoryIncludesLoader(t *testing.T) {
	for _, sample := range []struct{ peak, retained, want int64 }{{3000000000, 500000000, 3000000000}, {1000000000, 500000000, 2804000000}, {0, 0, 2304000000}} {
		got, err := LocalExposureExecutionMemory(6000, 4000, sample.peak, sample.retained)
		if err != nil || got != sample.want {
			t.Fatal(got, err)
		}
	}
	for _, cost := range [][2]int64{{-1, 0}, {1, -1}, {0, 1}, {9007199254740992, 0}, {9007199254740991, 9007199254740991}} {
		if _, err := LocalExposureExecutionMemory(1, 1, cost[0], cost[1]); err == nil {
			t.Fatal("invalid loader cost accepted")
		}
	}
}

func TestExecutionMemoryMatchesInstalledWorker(t *testing.T) {
	python := os.Getenv("PHOTO_TEST_WORKER_PYTHON")
	if python == "" {
		t.Skip("explicit installed worker Python required")
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(python, "-c", `import json,sys
sys.path.insert(0,'workers')
from photo_worker.operations.semantic.local_exposure import local_exposure_execution_memory
print(json.dumps([local_exposure_execution_memory((4000,6000),loader_peak_bytes=p,retained_loader_bytes=r) for p,r in [(3000000000,500000000),(1000000000,500000000),(0,0)]]))`)
	command.Dir = root
	raw, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	var values []int64
	if json.Unmarshal(raw, &values) != nil || len(values) != 3 {
		t.Fatal("invalid worker output")
	}
	for n, cost := range [][2]int64{{3000000000, 500000000}, {1000000000, 500000000}, {0, 0}} {
		got, err := LocalExposureExecutionMemory(6000, 4000, cost[0], cost[1])
		if err != nil || got != values[n] {
			t.Fatal("execution budget mismatch", err)
		}
	}
}

func TestEncodedMemoryMatchesInstalledWorker(t *testing.T) {
	python := os.Getenv("PHOTO_TEST_WORKER_PYTHON")
	if python == "" {
		t.Skip("explicit installed worker Python required")
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(python, "-c", `import json,sys
sys.path.insert(0,'workers')
from photo_worker.operations.semantic.exposure_pipeline import encoded_local_exposure_memory
print(json.dumps([encoded_local_exposure_memory(shape) for shape in [(1,1),(4000,6000),(257,513),(1,200000000)]]))`)
	command.Dir = root
	raw, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	var values []int64
	if json.Unmarshal(raw, &values) != nil || len(values) != 4 {
		t.Fatal("invalid worker estimates")
	}
	for n, shape := range [][2]int64{{1, 1}, {6000, 4000}, {513, 257}, {200000000, 1}} {
		got, err := EncodedLocalExposureMemory(shape[0], shape[1])
		if err != nil || got != values[n] {
			t.Fatal("full encoded memory differs", got, err)
		}
	}
	for _, shape := range [][2]int64{{0, 1}, {1, -1}, {200000001, 1}, {9223372036854775807, 9223372036854775807}} {
		if _, err := EncodedLocalExposureMemory(shape[0], shape[1]); err == nil {
			t.Fatal("invalid canvas admitted")
		}
	}
}
