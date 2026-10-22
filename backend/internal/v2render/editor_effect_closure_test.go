package v2render

import "testing"

func TestExecutedBaseCannotUseEmptyLayerIdentityEvidenceShortcut(t *testing.T) {
	// Scope/receipt bytes are qualified by admission, not this detector. Even a
	// malformed non-null receipt must not fall through the empty-stack shortcut.
	request := RenderDraftRequest{RecipeRaw: []byte(`{"recipe":{"executed_base_receipt":{"artifact_id":"foreign"}}}`)}
	if !hasEditorExecutedReceipt(request.RecipeRaw) { t.Fatal("receipt was ignored") }
	if validateRenderClosureCoverage(request) == nil { t.Fatal("missing original execution closure accepted") }
	if validateRenderOccurrences(request, nil) == nil { t.Fatal("missing scoped occurrences accepted") }
	for _, raw := range []string{`{"recipe":{}}`, `{"recipe":{"executed_base_receipt":null}}`} {
		plain := RenderDraftRequest{RecipeRaw: []byte(raw)}
		if hasEditorExecutedReceipt(plain.RecipeRaw) { t.Fatal("ordinary identity falsely classified") }
		if validateRenderClosureCoverage(plain) != nil || validateRenderOccurrences(plain, nil) != nil { t.Fatal("ordinary empty-stack contract changed") }
	}
}
