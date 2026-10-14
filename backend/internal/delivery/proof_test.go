package delivery

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestPrepareProofRepeatsExactFrozenInputs(t *testing.T) {
	ref, snapshot, policy, configs := freezeFixture(t)
	_, frozen, sha, err := CompileFrozenInput(ref, snapshot, policy, configs)
	if err != nil {
		t.Fatal(err)
	}
	root := v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}
	project, session := uuid.New(), uuid.New()
	proof, body, params, err := CompilePrepareProof(project, session, root, frozen, snapshot, configs)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) == 0 || len(params) == 0 || proof.FrozenInputJCS != string(frozen) || proof.SnapshotJCS != string(snapshot) || len(proof.ConfigsJCS) != 1 {
		t.Fatal("changed proof")
	}
	configs[0].Raw[0] = 'x'
	if proof.ConfigsJCS[0].ConfigJCS[0] != '{' {
		t.Fatal("borrowed bytes")
	}
	if _, _, _, err := CompilePrepareProof(project, session, root, frozen, snapshot, configs); err == nil {
		t.Fatal("substituted config accepted")
	}
	if _, _, _, err := CompilePrepareProof(uuid.Nil, session, root, frozen, snapshot, nil); err == nil {
		t.Fatal("missing project accepted")
	}
}
