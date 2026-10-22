package interchange

import (
	"context"
	"photoeditor/backend/internal/v2input"
	"sync"
)

// A workflow-local dispatch index, never an authorization cache. Every lookup
// delegates to the original scoped SQL/physical resolver again.
type ImportPreparedNodeRegistry struct {
	mu    sync.RWMutex
	nodes map[v2input.Entry]ImportClosureNodeResolver
 auxiliary map[v2input.Entry]ImportClosureNodeResolver
 auxiliaryParents map[v2input.Entry]v2input.Entry
}

func (r *ImportPreparedNodeRegistry) Contains(entry v2input.Entry) bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.nodes[entry] != nil
}
func (r *ImportPreparedNodeRegistry) ResolveImportNode(ctx context.Context, entry v2input.Entry) (ImportClosureNode, error) {
	if err := ctx.Err(); err != nil {
		return ImportClosureNode{}, err
	}
	if r == nil {
		return ImportClosureNode{}, v2input.ErrInvalid
	}
	r.mu.RLock()
	resolver := r.nodes[entry]
	r.mu.RUnlock()
	if resolver == nil {
		return ImportClosureNode{}, v2input.ErrInvalid
	}
	node, err := resolver.ResolveImportNode(ctx, entry)
	if err != nil {
		return ImportClosureNode{}, err
	}
	if node.Entry != entry {
		return ImportClosureNode{}, v2input.ErrInvalid
	}
	return node, ctx.Err()
}
func (r *ImportPreparedNodeRegistry) register(nodes map[v2input.Entry]ImportClosureNodeResolver) error {
	if r == nil || len(nodes) < 1 || len(nodes) > 30000 {
		return v2input.ErrInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	identities := map[[16]byte]v2input.Entry{}
	for old := range r.nodes {
		identities[old.Ref.ArtifactID] = old
	}
	for entry, resolver := range nodes {
		if resolver == nil || !validImportRef(entry.Ref) {
			return v2input.ErrInvalid
		}
		switch entry.Contract {
		case "EditingRecipeV3", "StyleV2", "CorrectionPlan", "Override", "LayerStack", "MaskRef", "Params", "Stats20", "ResolvedStyle", "DefectCandidates", "DefectReview", "DustCandidates", "DefectProposeReport", "WorkingImage", "FaceLandmarks", "FaceBoxes", "HistoricalOriginReceipt", "WorkingPixels", "WorkingAlpha", "MaskPixels", "DenseWarpField", "AbsoluteLUTArray", "DefectLabelMap", "DefectLabelPNG", "StyleBackgroundAsset":
		default:
			return v2input.ErrInvalid
		}
		if old, ok := identities[entry.Ref.ArtifactID]; ok && old != entry {
			return v2input.ErrInvalid
		}
		identities[entry.Ref.ArtifactID] = entry
	}
	if len(identities) > 30000 {
		return v2input.ErrInvalid
	}
	if r.nodes == nil {
		r.nodes = map[v2input.Entry]ImportClosureNodeResolver{}
	}
	// Repeated qualification cannot replace the authority installed for a pin.
	for entry, resolver := range nodes {
		if r.nodes[entry] == nil {
			r.nodes[entry] = resolver
		}
	}
	return nil
}

type recordingPreparedImportResolver struct {
	base  ImportClosureNodeResolver
	nodes map[v2input.Entry]ImportClosureNodeResolver
}

func (r *recordingPreparedImportResolver) ResolveImportNode(ctx context.Context, entry v2input.Entry) (ImportClosureNode, error) {
	node, err := r.base.ResolveImportNode(ctx, entry)
	if err != nil {
		return ImportClosureNode{}, err
	}
	if node.Entry != entry {
		return ImportClosureNode{}, v2input.ErrInvalid
	}
	switch entry.Contract {
	case "StyleV2", "CorrectionPlan", "Override", "LayerStack", "MaskRef", "Params", "Stats20", "ResolvedStyle", "DefectCandidates", "DefectReview", "DustCandidates", "DefectProposeReport", "WorkingImage", "FaceLandmarks", "FaceBoxes", "HistoricalOriginReceipt", "WorkingPixels", "WorkingAlpha", "MaskPixels", "DenseWarpField", "AbsoluteLUTArray", "DefectLabelMap", "DefectLabelPNG", "StyleBackgroundAsset":
		r.nodes[entry] = r.base
	}
	return node, nil
}

type registeredPreparedImportRecipeQualifier struct {
 Historical *HistoricalOriginIssuer
 HistoricalSource *ImportHistoricalNativeAuthority
 Publisher *PreparedImportArtifactPublisher
	Scope       ImportPreparedScope
	Resolver    ImportClosureNodeResolver
	Catalog     ImportDocumentCatalog
	Objects     ImportDocumentObjects
	Registry    *ImportPreparedNodeRegistry
	Composition *NativeExportStyleValidator
	Masters     ImportMasterReader
}

func (q registeredPreparedImportRecipeQualifier) QualifyPreparedImportRecipe(ctx context.Context, ref v2input.Ref, raw []byte, binding v2input.SessionVersionBinding) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if q.Registry == nil || q.Resolver == nil || q.Catalog == nil || q.Objects == nil {
		return v2input.ErrInvalid
	}
	recorder := &recordingPreparedImportResolver{base: q.Resolver, nodes: map[v2input.Entry]ImportClosureNodeResolver{}}
	if err := (BoundImportPreparedRecipeQualifier{Scope: q.Scope, Resolver: recorder}).QualifyPreparedImportRecipe(ctx, ref, raw, binding); err != nil {
		return err
	}
	if q.Composition != nil {
		composition := ImportPreparedCompositionQualifier{Scope: q.Scope, Resolver: recorder, Masters: q.Masters, Native: q.Composition}
		if err := composition.qualifyComposition(ctx, ref, raw, binding); err != nil {
			return err
		}
	}
	root := v2input.Entry{Ref: ref, Contract: "EditingRecipeV3"}
	recorder.nodes[root] = ImportJSONNodeResolver{Catalog: q.Catalog, Objects: q.Objects, Validator: pinnedPreparedRecipeValidator{Ref: ref, Binding: binding}}
	if err := ctx.Err(); err != nil {
		return err
	}
	receipts,err:=q.historicalReceipts(ctx,recorder.nodes);if err!=nil{return err}
 for entry,resolver:=range receipts{recorder.nodes[entry]=resolver}
 if err=q.Registry.register(recorder.nodes);err!=nil{return err}
 q.Registry.mu.Lock();defer q.Registry.mu.Unlock();if q.Registry.auxiliary==nil{q.Registry.auxiliary=map[v2input.Entry]ImportClosureNodeResolver{}};if q.Registry.auxiliaryParents==nil{q.Registry.auxiliaryParents=map[v2input.Entry]v2input.Entry{}};for entry,resolver:=range receipts{q.Registry.auxiliary[entry]=resolver;typed:=resolver.(ImportJSONNodeResolver).Validator.(historicalOriginNodeValidator);q.Registry.auxiliaryParents[entry]=v2input.Entry{Ref:typed.Expected.Prepared,Contract:typed.Expected.Contract}};return nil
}

type pinnedPreparedRecipeValidator struct {
	Ref     v2input.Ref
	Binding v2input.SessionVersionBinding
}

func (v pinnedPreparedRecipeValidator) ImportDocumentChildren(ctx context.Context, entry v2input.Entry, raw []byte) ([]v2input.Entry, error) {
	if entry.Contract != "EditingRecipeV3" || entry.Ref != v.Ref || rawSHA(raw) != v.Ref.SHA256 {
		return nil, v2input.ErrInvalid
	}
	return interchangeRecipeChildren(ctx, append([]byte(nil), raw...), v.Binding)
}

// Additional roots are privately issued authorization documents, never archive
// dependencies. Actual typed resolution is repeated during every inventory pass.
func(r *ImportPreparedNodeRegistry)AdditionalImportRoots(ctx context.Context)([]v2input.Entry,error){
 if err:=ctx.Err();err!=nil{return nil,err};if r==nil{return nil,nil};r.mu.RLock();defer r.mu.RUnlock();roots:=[]v2input.Entry{};for entry:=range r.auxiliary{roots=append(roots,entry)};return roots,nil
}

func(r *ImportPreparedNodeRegistry)AdditionalImportRootsFor(ctx context.Context,reachable []v2input.Entry)([]v2input.Entry,error){
 if err:=ctx.Err();err!=nil{return nil,err};if r==nil{return nil,nil};seen:=map[v2input.Entry]bool{};for _,entry:=range reachable{seen[entry]=true};r.mu.RLock();defer r.mu.RUnlock();roots:=[]v2input.Entry{};for entry,parent:=range r.auxiliaryParents{if seen[parent]{roots=append(roots,entry)}};return roots,nil
}
