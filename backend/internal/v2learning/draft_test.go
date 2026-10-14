package v2learning

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func compiledTrainingDraftFixture(t *testing.T) (uuid.UUID, uuid.UUID,
	TrainCreate, TrainingManifest) {
	t.Helper()
	rows := trainingCorpus()
	split, err := FreezeTrainingSplit(rows)
	if err != nil {
		t.Fatal(err)
	}
	ref := func(letter string) v2input.Ref {
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat(letter, 64)}
	}
	policy := LearningPolicy{SchemaVersion: 1, Mode: "photographer",
		MinExamples: 40, MinSessions: 6, HoldoutFraction: 0.2,
		Seed: 2026, Calibration: ref("5")}
	policySHA, err := canonical.Hash(policy)
	if err != nil {
		t.Fatal(err)
	}
	consentID := uuid.New()
	cases := make(map[uuid.UUID]ResolvedTrainingCase, len(rows))
	sessionSet := make(map[uuid.UUID]struct{})
	for _, source := range rows {
		sessionSet[source.SessionID] = struct{}{}
		base, approved, working := ref("b"), ref("c"), ref("d")
		approval := uuid.New()
		candidate := TrainingCandidate{EventID: source.EventID,
			SessionID: source.SessionID, AssetVersionID: uuid.New(),
			SourceContentSHA: source.SourceContentSHA,
			SourceLineageSHA: source.SourceLineageSHA,
			BaseDocumentID:   uuid.New(), BaseArtifactID: base.ArtifactID,
			BaseSHA: base.SHA256, ApprovedDocumentID: uuid.New(),
			ApprovedArtifactID: approved.ArtifactID,
			ApprovedSHA:        approved.SHA256, ApprovalID: &approval,
			ApprovedAt: source.ApprovedAt, ConsentID: consentID,
			ConsentEpoch: 1, ChangedPaths: []string{"/ev"},
			ConsentLive: true, SourceReady: true, BaseReady: true,
			ApprovedReady: true}
		render := TrainingRenderCase{
			Binding: v2input.SessionVersionBinding{
				AssetVersionID: candidate.AssetVersionID,
				Source:         working, CanvasHash: strings.Repeat("e", 64)},
			WorkingImage: working, BaseRecipe: base,
			ApprovedRecipe: approved, Protected: ref("f"), Faces: ref("0"),
			BaseDocument: base, ApprovedDocument: approved,
			DescriptorShard: ref("8"), DescriptorAnalysisID: uuid.New(),
			DescriptorOrdinal: 0,
			ApprovalID:        approval, SourceLineageSHA: source.SourceLineageSHA}
		cases[source.EventID] = ResolvedTrainingCase{Candidate: candidate,
			Render: render}
	}
	profileID, projectID := uuid.New(), uuid.New()
	context := ManifestContext{ProfileID: profileID, ProjectID: projectID,
		Mode: "photographer", PolicySHA: policySHA,
		Baseline: ref("2"), ICCLockSHA: strings.Repeat("3", 64),
		ModelLockSHA: strings.Repeat("4", 64), Calibration: policy.Calibration}
	manifest, _, _, err := CompileTrainingManifest(context, split, cases)
	if err != nil {
		t.Fatal(err)
	}
	sessions := make([]uuid.UUID, 0, len(sessionSet))
	for id := range sessionSet {
		sessions = append(sessions, id)
	}
	return projectID, profileID, TrainCreate{
		ExpectedRevision: "1", SessionIDs: sessions, Policy: policy}, manifest
}

func TestTrainingDraftRecoveryUsesExactCanonicalBytes(t *testing.T) {
	project, profile, request, manifest := compiledTrainingDraftFixture(t)
	tenant, actor, session := uuid.New(), uuid.New(), uuid.New()
	draft, err := PrepareTrainingDraft(tenant, actor, session, project,
		profile, "train-key", 4096, request, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if draft.RunID != uuid.NewSHA1(tenant,
		[]byte("V2.Admit:"+actor.String()+":train-key")) ||
		draft.SnapshotID != uuid.NewSHA1(draft.RunID,
			[]byte("training.snapshot")) {
		t.Fatal("draft IDs not deterministic")
	}
	recovered, err := ResumeTrainingDraft(tenant, actor, session, project,
		profile, "train-key", request, draft.RecoveryBody)
	if err != nil || !bytes.Equal(recovered.ManifestBody, draft.ManifestBody) ||
		!bytes.Equal(recovered.SnapshotBody, draft.SnapshotBody) ||
		recovered.ConfigSHA != draft.ConfigSHA ||
		recovered.RequestSHA != draft.RequestSHA {
		t.Fatal("pending draft not restored exactly", err)
	}
	var changed map[string]any
	if err := json.Unmarshal(draft.RecoveryBody, &changed); err != nil {
		t.Fatal(err)
	}
	changed["manifest_sha"] = strings.Repeat("f", 64)
	tampered, err := canonical.Marshal(changed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ResumeTrainingDraft(tenant, actor, session, project,
		profile, "train-key", request, tampered); err == nil {
		t.Fatal("tampered recovery accepted")
	}
	request.SessionIDs[0] = uuid.New()
	if _, err := PrepareTrainingDraft(tenant, actor, session, project,
		profile, "train-key", 4096, request, manifest); err == nil {
		t.Fatal("manifest outside selected sessions accepted")
	}
}

func TestTrainingQueueRecoveryPreservesNewAndHistoricalConfigBytes(t *testing.T){
 project,profile,request,manifest:=compiledTrainingDraftFixture(t)
 tenant,actor,session:=uuid.New(),uuid.New(),uuid.New()
 var currentHash string
 for _,queue:=range []string{"training",""}{
  prepared,err:=prepareTrainingDraftForQueue(tenant,actor,session,project,profile,"queue-recovery",4096,request,manifest,queue)
  if err!=nil{t.Fatal(err)}
  var config map[string]json.RawMessage;if err=json.Unmarshal(prepared.ConfigBody,&config);err!=nil{t.Fatal(err)}
  if queue=="training"{if string(config["queue_class"])!=`"training"`{t.Fatal("new class not frozen")};currentHash=prepared.ConfigSHA}else{if _,present:=config["queue_class"];present{t.Fatal("historical config rewritten")};if prepared.ConfigSHA==currentHash{t.Fatal("class not hashed")}}
  recovered,err:=ResumeTrainingDraft(tenant,actor,session,project,profile,"queue-recovery",request,prepared.RecoveryBody)
  if err!=nil||!bytes.Equal(recovered.ConfigBody,prepared.ConfigBody)||recovered.ConfigSHA!=prepared.ConfigSHA||!bytes.Equal(recovered.RecoveryBody,prepared.RecoveryBody){t.Fatal("queue recovery changed saved bytes",queue,err)}
  var changed map[string]any;if err=json.Unmarshal(prepared.RecoveryBody,&changed);err!=nil{t.Fatal(err)}
  changed["queue_class"]="interactive"
  bad,_:=canonical.Marshal(changed)
  if _,err=ResumeTrainingDraft(tenant,actor,session,project,profile,"queue-recovery",request,bad);err==nil{t.Fatal("foreign training queue accepted")}
 }
 fresh,err:=PrepareTrainingDraft(tenant,actor,session,project,profile,"queue-recovery",4096,request,manifest)
 if err!=nil||fresh.ConfigSHA!=currentHash{t.Fatal("public producer did not use training queue",err)}
}
