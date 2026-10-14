package v2render

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestMultiPreviewDocumentsRequireExactVerifiedScope(t *testing.T) {
	s, o, task := multiPipelineFixture(t, "optical.panorama", "valid")
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tenant, project := uuid.New(), uuid.New()
	content, err := LoadAdoptedMultiContent(context.Background(), s, o, who, tenant, project, task, s.binding.Operation, s.binding.Result, "photos")
	if err != nil {
		t.Fatal(err)
	}
	pin := MultiPreviewSource{Tenant: tenant, Project: project, Producer: task, Binding: s.binding, Attempt: 1, Fence: 2, Principal: who}
	root, image, err := content.PreviewDocuments(pin)
	if err != nil || adoptedArraySHA(root) != pin.Binding.Result.SHA256 {
		t.Fatal("root binding", err)
	}
	root[0] = 'x'
	image[0] = 'x'
	again, other, err := content.PreviewDocuments(pin)
	if err != nil || again[0] != '{' || other[0] != '{' {
		t.Fatal("bytes alias caller")
	}
	for _, kind := range []string{"tenant", "project", "result", "producer", "zero"} {
		changed := pin
		value := content
		switch kind {
		case "tenant":
			changed.Tenant = uuid.New()
		case "project":
			changed.Project = uuid.New()
		case "result":
			changed.Binding.ResultID = uuid.New()
		case "producer":
			changed.Producer = uuid.New()
		case "zero":
			value = MultiResultContent{}
		}
		if _, _, err := value.PreviewDocuments(changed); !errors.Is(err, v2input.ErrInvalid) {
			t.Fatalf("scope %s accepted: %v", kind, err)
		}
	}
}
