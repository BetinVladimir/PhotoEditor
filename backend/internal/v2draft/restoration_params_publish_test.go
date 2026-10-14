package v2draft

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

type reflectionParamsPublisher struct {
	tx    *restorationStoreTx
	calls int
}

func (p *reflectionParamsPublisher) Publish(_ context.Context, scope artifactjson.Scope,
	body []byte) (artifactjson.Ref, error) {
	p.calls++
	if p.tx.commits != 1 || !scope.RestorationPortrait || scope.Purpose != "task_params" ||
		scope.Owner == uuid.Nil || len(body) < 2 {
		return artifactjson.Ref{}, v2input.ErrInvalid
	}
	digest, err := canonical.Hash(json.RawMessage(body))
	if err != nil {
		return artifactjson.Ref{}, err
	}
	return artifactjson.Ref{ID: uuid.New(), SHA256: digest}, nil
}

func TestRestorationParamsIntentUsesSchedulerAndExactRequest(t *testing.T) {
	tx := &restorationStoreTx{role: "photo_scheduler", accepted: true}
	service := SQLRestorationParamsPublisher{DB: restorationStoreDB{tx}}
	tenant, run := uuid.New(), uuid.New()
	request := []byte(`{"actor_id":"owned"}`)
	if err := service.saveIntent(context.Background(), tenant, run, request); err != nil {
		t.Fatal(err)
	}
	if tx.tenant != tenant.String() || tx.commits != 1 ||
		len(tx.args) != 3 || tx.args[0] != tenant || tx.args[1] != run ||
		tx.args[2] != string(request) ||
		!strings.Contains(tx.queries[len(tx.queries)-1],
			"save_v28_restoration_params_intent") {
		t.Fatal("restoration intent was not saved under exact scheduler scope")
	}
	tx.role = "photo_api"
	tx.queries = nil
	if err := service.saveIntent(context.Background(), tenant, run, request); err == nil ||
		len(tx.queries) != 1 {
		t.Fatal("non-scheduler reached intent writer")
	}
}

func TestReflectionParamsPublicationRequiresSavedIntentAndCurrentScope(t *testing.T) {
	sha := strings.Repeat("a", 64)
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: sha} }
	pin := v2portrait.RestorationQualificationPin{Operation: "image.reflection",
		OperationVersion: "1", PolicyID: uuid.New(), QualificationID: uuid.New(),
		PolicySHA: sha, Report: ref(), Calibration: ref(), ModelLockSHA: sha,
		RuntimeLockSHA: sha, DeviceProfileSHA: sha, DatasetSHA: sha,
		IssuerID: uuid.New(), Backend: "cpu"}
	base := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref(), CanvasHash: sha}
	mask, guard := ref(), ref()
	params, err := canonical.Marshal(map[string]any{"schema_version": 1,
		"base": base, "strength": .5, "mask": mask, "protected": guard,
		"model_lock_sha": pin.ModelLockSHA, "calibration": pin.Calibration})
	if err != nil {
		t.Fatal(err)
	}
	origin := RestorationManualOrigin{Root: ref(), Params: ref(), TargetPixel: ref(), ProtectedPixel: ref()}
	tx := &restorationStoreTx{role: "photo_scheduler", accepted: true}
	publisher := &reflectionParamsPublisher{tx: tx}
	service := SQLRestorationParamsPublisher{DB: restorationStoreDB{tx}, Publisher: publisher}
	got, err := service.PublishReflection(context.Background(), auth.Identity{
		UserID: uuid.New(), SessionID: uuid.New()}, uuid.New(), uuid.New(),
		uuid.New(), 4, params, pin, origin)
	if err != nil || got.ArtifactID == uuid.Nil || publisher.calls != 1 || tx.commits != 2 ||
		len(tx.args) != 3 || !strings.Contains(tx.queries[len(tx.queries)-1],
		"save_v28_restoration_params_intent") {
		t.Fatal("reflection Params were not published under repeated SQL scope", err)
	}
	changed := pin
	changed.ModelLockSHA = strings.Repeat("b", 64)
	tx.queries = nil
	publisher.calls = 0
	if _, err := service.PublishReflection(context.Background(), auth.Identity{
		UserID: uuid.New(), SessionID: uuid.New()}, uuid.New(), uuid.New(),
		uuid.New(), 4, params, changed, origin); err != v2input.ErrInvalid ||
		len(tx.queries) != 0 || publisher.calls != 0 {
		t.Fatal("changed reflection model reached SQL/object publication", err)
	}
}
