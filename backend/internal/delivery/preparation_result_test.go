package delivery

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestPreparationResultRequiresExactFrozenMatrix(t *testing.T) {
	snapshot, snapshotRaw, policy, configs := freezeFixture(t)
	_, frozenRaw, sha, err := CompileFrozenInput(snapshot, snapshotRaw, policy, configs)
	if err != nil {
		t.Fatal(err)
	}
	frozen := v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}
	project, session := uuid.New(), uuid.New()
	_, _, params, err := CompilePrepareProof(project, session, frozen, frozenRaw, snapshotRaw, configs)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := CompilePreparePlan(uuid.New(), project, session, v2input.Ref{ArtifactID: uuid.New(), SHA256: digest(params)}, frozen, frozenRaw, snapshotRaw, configs)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := canonical.Marshal(plan.preparation)
	if err != nil {
		t.Fatal(err)
	}
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: digest(raw)}
	if result, err := plan.ValidatePreparation(ref, raw); err != nil || result.ItemCount != 1 || result.SlotCount != 1 {
		t.Fatal("valid result rejected", err)
	}
	for _, kind := range []string{"count", "digest", "input", "schema", "extra", "whitespace", "null"} {
		t.Run(kind, func(t *testing.T) {
			changed := plan.preparation
			switch kind {
			case "count":
				changed.SlotCount++
			case "digest":
				changed.SnapshotDigest = digest([]byte("other"))
			case "input":
				changed.Input.ArtifactID = uuid.New()
			case "schema":
				changed.SchemaVersion = 2
			}
			body, err := canonical.Marshal(changed)
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "extra":
				body = append([]byte(`{"extra":1,`), body[1:]...)
			case "whitespace":
				body = append(body, ' ')
			case "null":
				body = []byte("null")
			}
			changedRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: digest(body)}
			if _, err := plan.ValidatePreparation(changedRef, body); err == nil {
				t.Fatal("substituted result accepted")
			}
		})
	}
	badRef := ref
	badRef.SHA256 = digest([]byte("other"))
	if _, err := plan.ValidatePreparation(badRef, raw); err == nil {
		t.Fatal("physical hash ignored")
	}
	if _, err := (PreparePlan{}).ValidatePreparation(ref, raw); err == nil {
		t.Fatal("zero plan accepted")
	}
}
