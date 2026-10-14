package v2learning

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

func TestFrozenDustHoldoutEvidenceExactReviewHash(t *testing.T) {
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	image, stageImage, protection, proposal, params := ref(), ref(), ref(), ref(), ref()
	selection := review.DustSelection{Version: "sensor_dust_review_v1",
		Proposal: review.ArtifactRef{ID: proposal.ArtifactID, SHA256: proposal.SHA256},
		Base:     review.ArtifactRef{ID: stageImage.ArtifactID, SHA256: stageImage.SHA256},
		Revision: 2, Accepted: []string{strings.Repeat("b", 64)}, Rejected: []string{}}
	decision := DustHoldoutDecision{TenantID: uuid.New(), ReviewID: uuid.New(), RunID: uuid.New(), ActorID: uuid.New(),
		Decision: "accept", Route: "repair", Revision: 2, Selection: selection}
	hash, err := canonical.Hash(struct {
		Version   string               `json:"version"`
		TenantID  uuid.UUID            `json:"tenant_id"`
		ReviewID  uuid.UUID            `json:"review_id"`
		RunID     uuid.UUID            `json:"run_id"`
		ActorID   uuid.UUID            `json:"actor_id"`
		Decision  string               `json:"decision"`
		Selection review.DustSelection `json:"selection"`
	}{"sensor_dust_decision_v1", decision.TenantID, decision.ReviewID, decision.RunID, decision.ActorID, "accept", selection})
	if err != nil {
		t.Fatal(err)
	}
	decision.DecisionHash = hash
	event, version := uuid.New(), uuid.New()
	frozen, body, pin, err := CompileFrozenDustHoldoutEvidence(decision, event, version,
		image, stageImage, protection, proposal, params)
	sum := sha256.Sum256(body)
	if err != nil || frozen.DecisionHash != hash || pin != hex.EncodeToString(sum[:]) || len(body) == 0 {
		t.Fatal(err)
	}
	artifact := ref()
	artifact.SHA256 = pin
	expected := DustHoldoutExpected{TenantID: decision.TenantID, EventID: event,
		AssetVersionID: version, ReviewID: decision.ReviewID, RunID: decision.RunID,
		ActorID: decision.ActorID, DecisionHash: hash, Image: image, StageImage: stageImage,
		Protection: protection, Proposal: proposal, Params: params}
	decoded, err := DecodeFrozenDustHoldoutEvidence(body, artifact, expected)
	if err != nil || decoded.DecisionHash != hash || decoded.Selection.Revision != 2 {
		t.Fatal("exact frozen dust decode", err)
	}
	wrong := expected
	wrong.ActorID = uuid.New()
	if _, err := DecodeFrozenDustHoldoutEvidence(body, artifact, wrong); err == nil {
		t.Fatal("accepted foreign actor")
	}
	wrong = expected
	wrong.Proposal = ref()
	if _, err := DecodeFrozenDustHoldoutEvidence(body, artifact, wrong); err == nil {
		t.Fatal("accepted changed proposal")
	}
	badPin := artifact
	badPin.SHA256 = strings.Repeat("f", 64)
	if _, err := DecodeFrozenDustHoldoutEvidence(body, badPin, expected); err == nil {
		t.Fatal("accepted wrong object hash")
	}
	changed := append([]byte(nil), body...)
	changed = append(changed, '\n')
	if _, err := DecodeFrozenDustHoldoutEvidence(changed, artifact, expected); err == nil {
		t.Fatal("accepted noncanonical bytes")
	}
	selection.Accepted[0] = strings.Repeat("c", 64)
	if bytes.Contains(body, []byte(strings.Repeat("c", 64))) {
		t.Fatal("caller mutation changed frozen bytes")
	}
	selection.Accepted[0] = strings.Repeat("b", 64)
	for _, mode := range []string{"hash", "route", "selection", "alias", "revision"} {
		current := decision
		current.Selection = decision.Selection
		current.Selection.Accepted = append([]string{}, decision.Selection.Accepted...)
		p := params
		switch mode {
		case "hash":
			current.DecisionHash = strings.Repeat("d", 64)
		case "route":
			current.Route = "base"
		case "selection":
			current.Selection.Accepted = []string{strings.Repeat("e", 64)}
		case "alias":
			p = proposal
		case "revision":
			current.Revision++
		}
		if _, _, _, err := CompileFrozenDustHoldoutEvidence(current, event, version,
			image, stageImage, protection, proposal, p); err == nil {
			t.Fatal("accepted", mode)
		}
	}
	stable, err := ReviewedDustHoldoutRef(decision.RunID, event, pin)
	if err != nil || stable.ArtifactID == uuid.Nil || stable.SHA256 != pin {
		t.Fatal("invalid stable dust reference", err)
	}
	stableReplay, err := ReviewedDustHoldoutRef(decision.RunID, event, pin)
	if err != nil || stableReplay != stable {
		t.Fatal("dust reference changed on replay", err)
	}
	if _, err := ReviewedDustHoldoutRef(decision.RunID, uuid.Nil, pin); err == nil {
		t.Fatal("accepted missing event")
	}
	aliased := decision
	aliased.Selection.Base = review.ArtifactRef{ID: params.ArtifactID, SHA256: params.SHA256}
	if _, _, _, err := CompileFrozenDustHoldoutEvidence(aliased, event, version,
		image, stageImage, protection, proposal, params); err == nil {
		t.Fatal("accepted Params as review base")
	}
}
