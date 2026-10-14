package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestSavedDefectOriginBindsRestartContext(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	project := uuid.New()
	request := Request{ActorID: who.UserID, RunID: uuid.New(), RequestSHA: strings.Repeat("a", 64)}
	request.Intent.Reference = v2input.SessionVersionBinding{AssetVersionID: uuid.New()}
	intent := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}
	snapshot := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}
	selected := defectOriginSelection{ProducerID: uuid.New(), ParamsSHA: strings.Repeat("d", 64)}
	native := map[string]any{"schema_version": 1, "parent_run_id": request.RunID, "request_sha": request.RequestSHA, "auth_session_id": who.SessionID, "source": request.Intent.Reference, "intent": intent, "snapshot": snapshot, "original_producer": selected}
	body, err := canonical.Marshal(map[string]any{"project_id": project, "v27_defect_prepare": native})
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeSavedDefectOrigin(body, project, who, request, intent, snapshot)
	if err != nil || got != selected {
		t.Fatal("exact restart selection", err)
	}
	for _, name := range []string{"project", "session", "parent", "request", "source", "intent", "snapshot"} {
		t.Run(name, func(t *testing.T) {
			p, w, r, i, s := project, who, request, intent, snapshot
			switch name {
			case "project":
				p = uuid.New()
			case "session":
				w.SessionID = uuid.New()
			case "parent":
				r.RunID = uuid.New()
			case "request":
				r.RequestSHA = strings.Repeat("e", 64)
			case "source":
				r.Intent.Reference.AssetVersionID = uuid.New()
			case "intent":
				i.ArtifactID = uuid.New()
			case "snapshot":
				s.ArtifactID = uuid.New()
			}
			got, err := decodeSavedDefectOrigin(body, p, w, r, i, s)
			if err == nil || got != (defectOriginSelection{}) {
				t.Fatal("foreign restart context accepted", err)
			}
		})
	}
	for _, bad := range []any{
		nil,
		[]string{"task", "params"},
		map[string]any{"producer_task_id": uuid.Nil, "params_sha256": selected.ParamsSHA},
		map[string]any{"producer_task_id": selected.ProducerID, "params_sha256": "invalid"},
		map[string]any{"producer_task_id": selected.ProducerID, "params_sha256": selected.ParamsSHA, "extra": true},
	} {
		native["original_producer"] = bad
		body, err = canonical.Marshal(map[string]any{"project_id": project, "v27_defect_prepare": native})
		if err != nil {
			t.Fatal(err)
		}
		got, err = decodeSavedDefectOrigin(body, project, who, request, intent, snapshot)
		if err == nil || got != (defectOriginSelection{}) {
			t.Fatal("invalid locator accepted", err)
		}
	}
	delete(native, "original_producer")
	body, err = canonical.Marshal(map[string]any{"project_id": project, "v27_defect_prepare": native})
	if err != nil {
		t.Fatal(err)
	}
	got, err = decodeSavedDefectOrigin(body, project, who, request, intent, snapshot)
	if err == nil || got != (defectOriginSelection{}) {
		t.Fatal("legacy snapshot guessed producer", err)
	}
}

func TestSavedDefectOriginRefusesInvalidContextBeforeSQL(t *testing.T) {
	r := SQLTransferSourceReader{DB: &pgxpool.Pool{}, ProjectID: uuid.New()}
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	request := Request{TenantID: uuid.New(), ActorID: who.UserID, RunID: uuid.New()}
	got, err := r.readSavedDefectOrigin(context.Background(), who, request, v2input.Ref{ArtifactID: uuid.New()}, v2input.Ref{ArtifactID: uuid.New()})
	if !errors.Is(err, v2input.ErrInvalid) || got != (DefectOriginPins{}) {
		t.Fatal("incomplete restart entered SQL", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err = r.readSavedDefectOrigin(ctx, who, request, v2input.Ref{}, v2input.Ref{})
	if !errors.Is(err, context.Canceled) || got != (DefectOriginPins{}) {
		t.Fatal("cancelled restart entered SQL", err)
	}
}
