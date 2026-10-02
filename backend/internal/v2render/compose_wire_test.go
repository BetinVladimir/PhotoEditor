package v2render

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"os"
	"os/exec"
	"path/filepath"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"strconv"
	"strings"
	"testing"
)

func TestCompositionWirePythonCanonicalParity(t *testing.T) {
	python := os.Getenv("PHOTO_LAYER_PYTHON")
	if python == "" {
		t.Skip("PHOTO_LAYER_PYTHON selects the verified worker environment")
	}
	layer, base, proposal := compositionInputs()
	for _, withParent := range []bool{false, true} {
		var compiled CompiledComposition
		var err error
		if withParent {
			protected := proposal
			protected.ArtifactID = uuid.New()
			compiled, err = CompileComposition(layer, base, &proposal, proposal, &protected, 257, 2)
		} else {
			compiled, err = CompileComposition(layer, base, nil, proposal, nil, 257, 2)
		}
		if err != nil {
			t.Fatal(err)
		}
		root, err := filepath.Abs("../../..")
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(compiled.JSON)
		paramsRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(digest[:])}
		roots, err := CompositionInputRoots(compiled, paramsRef)
		if err != nil {
			t.Fatal(err)
		}
		workflow := uuid.New()
		task, _ := v2input.TaskID(workflow, "layer.compose", 0)
		draft, _, err := CompileCompositionDraft(workflow, 0, CompositionPreparation{Tenant: uuid.New(), Task: task, Actor: auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, Layer: layer, Base: base, SourceResult: compiled.Params.SourceResult, ProposalResult: proposal, ProtectedMask: compiled.Params.ProtectedMask, Width: 257, Height: 2, ParamsRef: paramsRef}, roots)
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(python, "-c", `import sys,hashlib,json
from pathlib import Path
from jsonschema import Draft202012Validator,FormatChecker
from photo_worker.operations.layers.contracts import LayerComposeParams
from photo_worker.contracts.base import canonical_bytes
from photo_worker.operations.layers.resources import admit_layer
raw=sys.stdin.buffer.read()
schema=json.loads((Path(sys.argv[1])/'docs/implementation-v2/contracts/schemas.json').read_text())
Draft202012Validator({**schema,'$ref':'#/$defs/LayerComposeParams'},format_checker=FormatChecker()).validate(json.loads(raw))
params=LayerComposeParams.model_validate_json(raw)
admit_layer(json.loads(sys.argv[2]),257,2,mask_refs=int(sys.argv[3]),mask_nodes=int(sys.argv[4]),tile_size=512)
print(hashlib.sha256(canonical_bytes(params)).hexdigest())`, root, string(draft.ExecutionProfile), strconv.Itoa(compiled.Budget.MaskRefs), strconv.Itoa(len(compiled.Budget.Masks)))
		cmd.Env = append(os.Environ(), "PYTHONPATH="+filepath.Join(root, "workers"))
		cmd.Stdin = bytes.NewReader(compiled.JSON)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("worker rejects compiler JSON: %v %s", err, output)
		}
		if strings.TrimSpace(string(output)) != hex.EncodeToString(digest[:]) {
			t.Fatalf("canonical mismatch: %s %s", compiled.JSON, output)
		}
	}
}
