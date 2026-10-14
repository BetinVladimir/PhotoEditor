package semanticreview

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"photoeditor/backend/internal/canonical"
	"testing"
)

func TestApprovedEditInstalledWorkerWireContract(t *testing.T) {
	python := os.Getenv("PHOTO_TEST_WORKER_PYTHON")
	if python == "" {
		t.Skip("explicit installed worker Python required")
	}
	prepared, targets, request := decisionProofFixture(t)
	edits, err := CompileApprovedEdits(prepared, targets, request)
	if err != nil || len(edits) != 1 {
		t.Fatal(err)
	}
	body, err := canonical.Marshal(edits[0])
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(python, "-c", `import sys
sys.path.insert(0,'workers')
from photo_worker.contracts.semantic import ApprovedSemanticEdit
from photo_worker.contracts.base import canonical_bytes
edit=ApprovedSemanticEdit.model_validate_json(sys.stdin.buffer.read())
sys.stdout.buffer.write(canonical_bytes(edit.model_dump(mode='json')))`)
	command.Dir = root
	command.Stdin = bytes.NewReader(body)
	output, err := command.CombinedOutput()
	if err != nil || !bytes.Equal(output, body) {
		t.Fatal("Go/worker approved edit roundtrip mismatch", err, string(output))
	}
}
