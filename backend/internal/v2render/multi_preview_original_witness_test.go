package v2render

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestMultiPreviewOriginalInventoryRoundTripAndOwnership(t *testing.T) {
	for _, operation := range []string{"optical.panorama", "optical.focus_stack"} {
		t.Run(operation, func(t *testing.T) {
			s, o, task := multiPipelineFixture(t, operation, "valid")
			who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
			tenant, project := uuid.New(), uuid.New()
			content, err := LoadAdoptedMultiContent(context.Background(), s, o, who, tenant, project, task, operation, s.binding.Result, "photos")
			if err != nil {
				t.Fatal(err)
			}
			source := MultiPreviewSource{Tenant: tenant, Project: project, Producer: task, Binding: s.binding, Attempt: 1, Fence: 2, Principal: who}
			raw, err := content.PreviewOriginalInventory(source)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := decodeMultiOriginalInventory(raw, s.binding, task, "photos")
			if err != nil || !decoded.equal(content.documents.sources.originals.inventory) {
				t.Fatal("incomplete or changed source inventory", err)
			}
			saved := string(raw)
			raw[0] = 'x'
			again, err := content.PreviewOriginalInventory(source)
			if err != nil || string(again) != saved {
				t.Fatal("aliased serialized evidence", err)
			}
			changed := source
			changed.Tenant = uuid.New()
			if _, err := content.PreviewOriginalInventory(changed); err == nil {
				t.Fatal("foreign source scope accepted")
			}
			corrupt := content
			corrupt.documents.sources.originals.inventory = content.documents.sources.originals.inventory.owned()
			corrupt.documents.sources.originals.inventory.items[0].Object.VersionID = "null"
			if _, err := corrupt.PreviewOriginalInventory(source); err == nil {
				t.Fatal("unversioned source evidence accepted")
			}
		})
	}
	if _, err := (MultiResultContent{}).PreviewOriginalInventory(MultiPreviewSource{}); err == nil {
		t.Fatal("zero content accepted")
	}
}
