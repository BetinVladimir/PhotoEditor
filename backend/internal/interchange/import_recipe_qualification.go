package interchange

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"sort"
)

// Resolver must independently authorize current prepared SQL provenance and
// physically qualify typed JSON/arrays/assets. A schema-only reader is invalid.
// The recipe root is not published yet; all dependencies must already exist.
type BoundImportPreparedRecipeQualifier struct {
	Scope    ImportPreparedScope
	Resolver ImportClosureNodeResolver
}

func (q BoundImportPreparedRecipeQualifier) QualifyPreparedImportRecipe(ctx context.Context, ref v2input.Ref, raw []byte, binding v2input.SessionVersionBinding) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s := q.Scope
	if q.Resolver == nil || s.Tenant == uuid.Nil || s.Run == uuid.Nil || s.Workflow == uuid.Nil || s.Entry == uuid.Nil || s.Binding != binding || !validImportRef(ref) || ref.ArtifactID != uuid.NewSHA1(s.Workflow, []byte("interchange:recipe:"+s.Entry.String())) || rawSHA(raw) != ref.SHA256 || len(raw) > 1<<20 {
		return v2input.ErrInvalid
	}
	owned := append([]byte(nil), raw...)
	children, err := interchangeRecipeChildren(ctx, owned, binding)
	if err != nil {
		return err
	}
	root := ImportClosureNode{Entry: v2input.Entry{Ref: ref, Contract: "EditingRecipeV3"}, Body: owned, Children: children}
	first, err := resolvePreparedRecipeClosure(ctx, root, q.Resolver)
	if err != nil {
		return err
	}
	second, err := resolvePreparedRecipeClosure(ctx, root, q.Resolver)
	if err != nil {
		return err
	}
	if first != second {
		return v2input.ErrInvalid
	}
	return ctx.Err()
}
func resolvePreparedRecipeClosure(ctx context.Context, root ImportClosureNode, resolver ImportClosureNodeResolver) (string, error) {
	seen := map[uuid.UUID]v2input.Entry{}
	active := map[uuid.UUID]bool{}
	entries := []v2input.Entry{}
	total := 0
	var visit func(v2input.Entry, int) error
	visit = func(entry v2input.Entry, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if depth > 32 || !validImportRef(entry.Ref) || len(entry.Contract) < 1 || len(entry.Contract) > 120 {
			return v2input.ErrInvalid
		}
		if old, exists := seen[entry.Ref.ArtifactID]; exists {
			if old != entry || active[entry.Ref.ArtifactID] {
				return v2input.ErrInvalid
			}
			return nil
		}
		if len(seen) >= 30000 {
			return v2input.ErrInvalid
		}
		seen[entry.Ref.ArtifactID] = entry
		active[entry.Ref.ArtifactID] = true
		node := root
		if entry != root.Entry {
			var err error
			node, err = resolver.ResolveImportNode(ctx, entry)
			if err != nil {
				return err
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if node.Entry != entry {
			return v2input.ErrInvalid
		}
		body := append([]byte(nil), node.Body...)
		children := append([]v2input.Entry(nil), node.Children...)
		if node.Body == nil {
			switch entry.Contract {
			case "WorkingPixels", "WorkingAlpha", "MaskPixels", "MaskArray", "DenseWarpField", "DenseWarpArray", "StyleBackgroundAsset", "AbsoluteLUTArray", "LUTArray", "DefectLabelMap", "DefectLabelPNG":
			default:
				return v2input.ErrInvalid
			}
			if len(children) != 0 {
				return v2input.ErrInvalid
			}
		} else {
			if len(body) < 2 || len(body) > 8<<20 || total > 128<<20-len(body) || rawSHA(body) != entry.Ref.SHA256 {
				return v2input.ErrInvalid
			}
			total += len(body)
			normalized, err := canonical.Bytes(body)
			if err != nil || !bytes.Equal(body, normalized) {
				return v2input.ErrInvalid
			}
			refs, err := importClosureDocumentRefs(body, entry.Contract, children)
			if err != nil {
				return err
			}
			supplied := map[uuid.UUID]v2input.Entry{}
			for _, child := range children {
				if _, exists := supplied[child.Ref.ArtifactID]; exists {
					return v2input.ErrInvalid
				}
				supplied[child.Ref.ArtifactID] = child
			}
			if len(refs) != len(supplied) {
				return v2input.ErrInvalid
			}
			for id, ref := range refs {
				if supplied[id].Ref != ref {
					return v2input.ErrInvalid
				}
			}
		}
		for _, child := range children {
			if err := visit(child, depth+1); err != nil {
				return err
			}
		}
		active[entry.Ref.ArtifactID] = false
		entries = append(entries, entry)
		return nil
	}
	if resolver == nil {
		return "", v2input.ErrInvalid
	}
	if err := visit(root.Entry, 0); err != nil {
		return "", err
	}
	sort.Slice(entries, func(i, j int) bool {
		return bytes.Compare(entries[i].Ref.ArtifactID[:], entries[j].Ref.ArtifactID[:]) < 0
	})
	return canonical.Hash(entries)
}

var _ ImportPreparedRecipeQualifier = BoundImportPreparedRecipeQualifier{}
