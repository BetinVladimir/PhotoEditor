package v2render

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"sort"
)

// Validate the unchanged real final input compiler first. Append only the
// reader-owned physical generation/report witnesses before immutable staging.
func compileAuthoredFinalRoot3Task(ctx context.Context, evidence AuthoredFinalInputEvidence, params v2input.Ref, raw []byte, proof VerifiedAuthoredFinalGeneration) (AuthoredFinalTaskPlan, error) {
	bad := func() (AuthoredFinalTaskPlan, error) { return AuthoredFinalTaskPlan{}, v2input.ErrInvalid }
	if e := ctx.Err(); e != nil {
		return AuthoredFinalTaskPlan{}, e
	}
	var profile AuthoredFinalRoot3ExecutionProfile
	if decodeAuthoredParams(raw, &profile) != nil || profile.ContractsSHA != AuthoredFinalRoot3SchemasSHA || profile.GenerationEvidence != proof.reference || len(proof.documents) != 2 {
		return bad()
	}
	round, e := canonical.Marshal(profile)
	if e != nil || !bytes.Equal(round, raw) {
		return bad()
	}
	d, e := proof.Evidence()
	if e != nil || profile.Qualification.RenderQualification != d.Qualification || profile.Qualification.CalibrationSHA != d.CalibrationSHA {
		return bad()
	}
	base := profile.AuthoredFinalExecutionProfile
	base.ContractsSHA = AuthoredFinalSchemasSHA
	baseRaw, e := canonical.Marshal(base)
	if e != nil {
		return bad()
	}
	plan, e := compileAuthoredFinalTask(evidence, params, baseRaw)
	if e != nil {
		return AuthoredFinalTaskPlan{}, e
	}
	docs := proof.documents
	if docs[0].Contract != "AuthoredFinalGenerationEvidence" || docs[0].Ref != proof.reference || docs[0].JCS != proof.documentJCS || docs[1].Contract != "QualificationReport" || docs[1].Ref != d.Report || docs[1].Parent != proof.reference || docs[1].JCS != proof.reportJCS {
		return bad()
	}
	for _, doc := range docs {
		entry := v2input.Entry{Ref: doc.Ref, Contract: doc.Contract}
		found := false
		for _, old := range plan.Entries {
			if old.Ref.ArtifactID == entry.Ref.ArtifactID {
				if old != entry {
					return bad()
				}
				found = true
			}
		}
		if !found {
			plan.Entries = append(plan.Entries, entry)
		}
	}
	if len(plan.Entries) > 4096 || validateRenderClosureDocuments(docs, plan.Entries) != nil {
		return bad()
	}
	sort.Slice(plan.Entries, func(i, j int) bool {
		return bytes.Compare(plan.Entries[i].Ref.ArtifactID[:], plan.Entries[j].Ref.ArtifactID[:]) < 0
	})
	digest, e := canonical.Hash(struct {
		Task      uuid.UUID       `json:"task"`
		Operation string          `json:"operation"`
		Version   string          `json:"version"`
		Entries   []v2input.Entry `json:"entries"`
	}{plan.Task.ID, "render.compose", "2", plan.Entries})
	if e != nil {
		return bad()
	}
	plan.Task.InputDigest = digest
	plan.Task.ExecutionProfile = append([]byte(nil), raw...)
	sum := sha256.Sum256(raw)
	plan.ProfileSHA = hex.EncodeToString(sum[:])
	fingerprint, e := canonical.Hash(plan)
	if e != nil {
		return bad()
	}
	plan.fingerprint = fingerprint
	return plan, ctx.Err()
}
