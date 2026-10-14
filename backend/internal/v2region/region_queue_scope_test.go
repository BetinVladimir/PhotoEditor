package v2region

import (
	"errors"
	"github.com/google/uuid"
	"testing"
)

func TestQueueRejectsPreparationScopeSubstitution(t *testing.T) {
	plan, _, _, _ := regionNodeFixture(t)
	doc, err := plan.decode()
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.validateQueueScope(doc.TenantID, doc.AssetID, doc.Request); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name          string
		tenant, asset uuid.UUID
		request       RegionPreviewRequest
	}{
		{"tenant", uuid.New(), doc.AssetID, doc.Request},
		{"asset", doc.TenantID, uuid.New(), doc.Request},
	}
	for _, field := range []string{"version", "recipe", "recipe_sha", "rect", "max_edge"} {
		req := doc.Request
		switch field {
		case "version":
			req.VersionID = uuid.New()
		case "recipe":
			req.Recipe.ArtifactID = uuid.New()
		case "recipe_sha":
			req.Recipe.SHA256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		case "rect":
			req.Rect.X0 += 0.01
		case "max_edge":
			req.MaxEdge++
		}
		tests = append(tests, struct {
			name          string
			tenant, asset uuid.UUID
			request       RegionPreviewRequest
		}{field, doc.TenantID, doc.AssetID, req})
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := plan.validateQueueScope(tc.tenant, tc.asset, tc.request); !errors.Is(err, ErrRegionInvalid) {
				t.Fatalf("substituted preparation accepted: %v", err)
			}
		})
	}
	broken := plan
	broken.body = append([]byte(nil), plan.body...)
	broken.body[0] = '!'
	if !errors.Is(broken.validateQueueScope(doc.TenantID, doc.AssetID, doc.Request), ErrRegionInvalid) {
		t.Fatal("corrupt plan accepted")
	}
}
