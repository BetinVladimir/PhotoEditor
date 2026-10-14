package interchange

import (
	"context"

	"photoeditor/backend/internal/v2input"
)

// Adds current target-side physical qualification of a portable background to
// the mandatory complete-content policy. An earlier publication witness alone
// cannot authorize use after its SQL certificate or object version changes.
type ImportOverrideCurrentAssetPolicy struct {
	Other     ImportOverrideContentQualifier
	Binding   v2input.SessionVersionBinding
	Witnesses *importBackgroundClosureWitnesses
	Assets    ImportClosureNodeResolver
}

func (q ImportOverrideCurrentAssetPolicy) QualifyImportOverrideContent(ctx context.Context, raw []byte, binding v2input.SessionVersionBinding) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if q.Other == nil || q.Witnesses == nil || q.Assets == nil || binding != q.Binding {
		return v2input.ErrInvalid
	}
	body := append([]byte(nil), raw...)
	if err := q.Other.QualifyImportOverrideContent(ctx, append([]byte(nil), body...), binding); err != nil {
		return err
	}
	id, present, err := importPortableBackgroundID(body, "Override")
	if err != nil || !present {
		return err
	}
	q.Witnesses.mu.RLock()
	ref, found := q.Witnesses.refs[id]
	q.Witnesses.mu.RUnlock()
	if !found || !validImportRef(ref) || ref.ArtifactID != id {
		return v2input.ErrInvalid
	}
	entry := v2input.Entry{Ref: ref, Contract: "StyleBackgroundAsset"}
	node, err := q.Assets.ResolveImportNode(ctx, entry)
	if err != nil {
		return err
	}
	if node.Entry != entry || node.Body != nil || len(node.Children) != 0 {
		return v2input.ErrInvalid
	}
	q.Witnesses.mu.RLock()
	current, stillFound := q.Witnesses.refs[id]
	q.Witnesses.mu.RUnlock()
	if !stillFound || current != ref {
		return v2input.ErrInvalid
	}
	return ctx.Err()
}

var _ ImportOverrideContentQualifier = ImportOverrideCurrentAssetPolicy{}
