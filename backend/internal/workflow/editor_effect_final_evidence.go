package workflow

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"sort"
	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// Resolve both geometries as separate authorized branches. Original authored
// provenance is never validated against, or relabelled as, the processed canvas.
func resolveEditorFinalBranches(ctx context.Context,objects ArtifactOpener,catalog editorFinalCatalog,receipt v2draft.EditorExecutedBaseReceipt,receiptRef v2input.Ref)(v2draft.TaskInputEvidence,error){
	invalid:=func()(v2draft.TaskInputEvidence,error){return v2draft.TaskInputEvidence{},v2input.ErrInvalid}
	if objects==nil||catalog.Build!=receipt.BuildRunID||receiptRef.ArtifactID==uuid.Nil{return invalid()}
	resolver:=v2draft.ClosureResolver{Catalog:catalog,Objects:objects}
	prefix:="TaskInputs[0].recipe.executed_base_receipt"
	root:=func(path,contract string,ref v2input.Ref)v2draft.LayerRef{return v2draft.LayerRef{Path:path,Contract:contract,Ref:ref,ParentRef:receiptRef}}
	originalImage:=root(prefix+".original_source.source","WorkingImage",receipt.OriginalSource.Source);originalImage.CanvasHash=receipt.OriginalSource.CanvasHash
	original,err:=resolver.ResolveBoundProvenanceEvidence(ctx,catalog.Actor,catalog.Tenant,[]v2draft.LayerRef{originalImage,root(prefix+".original_recipe","FrozenRenderRecipe",receipt.OriginalRecipe)},receipt.OriginalSource)
	if err!=nil{return v2draft.TaskInputEvidence{},err}
	processedImage:=root(prefix+".processed_source.source","WorkingImage",receipt.ProcessedSource.Source);processedImage.CanvasHash=receipt.ProcessedSource.CanvasHash
	processed,err:=resolver.ResolveBoundProvenanceEvidence(ctx,catalog.Actor,catalog.Tenant,[]v2draft.LayerRef{processedImage,root(prefix+".executed_style","StyleV2",receipt.ExecutedStyle),root(prefix+".quality","QualityReport",receipt.Quality),root(prefix+".image_result","ImageResult",receipt.ImageResult)},receipt.ProcessedSource)
	if err!=nil{return v2draft.TaskInputEvidence{},err}
	return mergeEditorFinalEvidence(original,processed)
}

func mergeEditorFinalEvidence(branches ...v2draft.TaskInputEvidence)(v2draft.TaskInputEvidence,error){
	result:=v2draft.TaskInputEvidence{Entries:[]v2input.Entry{},Documents:[]v2draft.DocumentWitness{},Occurrences:[]v2draft.OccurrenceWitness{}}
	refs:=map[uuid.UUID]int{};paths:=map[string]bool{};objects:=map[v2input.Ref]objectstore.Object{}
	for _,branch:=range branches{
		for _,entry:=range branch.Entries{
			if index,exists:=refs[entry.Ref.ArtifactID];exists{old:=result.Entries[index];if old.Ref!=entry.Ref||old.Contract!=entry.Contract&&old.Contract!="Artifact"&&entry.Contract!="Artifact"{return v2draft.TaskInputEvidence{},v2input.ErrInvalid};if old.Contract=="Artifact"{result.Entries[index]=entry}}else{refs[entry.Ref.ArtifactID]=len(result.Entries);result.Entries=append(result.Entries,entry)}
		}
		for _,occurrence:=range branch.Occurrences{
			if paths[occurrence.Node.Path]{return v2draft.TaskInputEvidence{},v2input.ErrInvalid};paths[occurrence.Node.Path]=true
			if old,exists:=objects[occurrence.Node.Ref];exists&&old!=occurrence.Object{return v2draft.TaskInputEvidence{},v2input.ErrExpired};objects[occurrence.Node.Ref]=occurrence.Object
			result.Occurrences=append(result.Occurrences,occurrence)
		}
		result.Documents=append(result.Documents,branch.Documents...)
	}
	if len(result.Entries)>4096||len(result.Occurrences)>4096{return v2draft.TaskInputEvidence{},v2input.ErrInvalid}
	sort.Slice(result.Entries,func(i,j int)bool{return bytes.Compare(result.Entries[i].Ref.ArtifactID[:],result.Entries[j].Ref.ArtifactID[:])<0})
	return result,nil
}

// Verify the exact original freeze before branch traversal; a structurally valid
// receipt with foreign original pixels cannot become final render provenance.
func readEditorFinalReceipt(ctx context.Context,objects ArtifactOpener,catalog editorFinalCatalog,ref v2input.Ref)(v2draft.EditorExecutedBaseReceipt,[]byte,error){
	var none v2draft.EditorExecutedBaseReceipt
	lookup:=func(ref v2input.Ref)(objectstore.Object,error){return catalog.Lookup(ctx,catalog.Actor,catalog.Tenant,v2draft.LayerRef{Ref:ref,Contract:"EditorExecutedBaseReceipt"})}
	object,err:=lookup(ref);if err!=nil{return none,nil,err}
	raw,err:=readEditorFinalDocument(ctx,objects,object,1<<20);if err!=nil{return none,nil,err}
	receipt,err:=v2draft.DecodeEditorExecutedBaseReceipt(raw);if err!=nil||receipt.BuildRunID!=catalog.Build{return none,nil,v2input.ErrInvalid}
	originalObject,err:=catalog.Lookup(ctx,catalog.Actor,catalog.Tenant,v2draft.LayerRef{Ref:receipt.OriginalRecipe,Contract:"FrozenRenderRecipe"});if err!=nil{return none,nil,err}
	originalRaw,err:=readEditorFinalDocument(ctx,objects,originalObject,1<<20);if err!=nil{return none,nil,err}
	original,err:=v2draft.DecodeFrozenRenderRecipe(originalRaw);if err!=nil||original.Scalars.Source!=receipt.OriginalSource{return none,nil,v2input.ErrInvalid}
	var recipe map[string]json.RawMessage;if json.Unmarshal(original.Recipe,&recipe)!=nil||recipe["executed_base_receipt"]!=nil{return none,nil,v2input.ErrInvalid}
	return receipt,raw,nil
}

func readEditorFinalDocument(ctx context.Context,objects ArtifactOpener,object objectstore.Object,maximum int64)([]byte,error){
	if err:=ctx.Err();err!=nil{return nil,err};if objects==nil||object.ContentType!="application/json"||object.Bytes<2||object.Bytes>maximum||object.VersionID==""||object.VersionID=="null"{return nil,objectstore.ErrIntegrity}
	reader,err:=objects.Open(ctx,object);if err!=nil{return nil,err}
	raw,readErr:=io.ReadAll(io.LimitReader(reader,object.Bytes+1));closeErr:=reader.Close();if readErr!=nil{return nil,readErr};if closeErr!=nil{return nil,closeErr};if err:=ctx.Err();err!=nil{return nil,err}
	digest:=sha256.Sum256(raw);if int64(len(raw))!=object.Bytes||hex.EncodeToString(digest[:])!=object.SHA256{return nil,objectstore.ErrIntegrity};return raw,nil
}

func(c *EditorEffectsCoordinator)resolveFinalEvidence(ctx context.Context,prepared editorPreparedFinalFreeze)(v2draft.TaskInputEvidence,[]byte,error){
	invalid:=func()(v2draft.TaskInputEvidence,[]byte,error){return v2draft.TaskInputEvidence{},nil,v2input.ErrInvalid}
	branches,err:=resolveEditorFinalBranches(ctx,c.Objects,prepared.Catalog,prepared.Receipt,v2input.Ref{ArtifactID:prepared.Origin.ReceiptID,SHA256:prepared.Origin.ReceiptSHA});if err!=nil{return v2draft.TaskInputEvidence{},nil,err}
	resolver:=v2draft.ClosureResolver{Catalog:prepared.Catalog,Objects:c.Objects}
	source:=prepared.Receipt.ProcessedSource
	root:=v2draft.LayerRef{Path:"TaskInputs[1]",Contract:"WorkingImage",Ref:source.Source,CanvasHash:source.CanvasHash}
	child:=root;child.Path="TaskInputs[0].recipe.source.source";child.ParentRef=prepared.Recipe
	style:=v2draft.LayerRef{Path:"TaskInputs[0].recipe.base_style",Contract:"StyleV2",Ref:prepared.Receipt.ExecutedStyle,ParentRef:prepared.Recipe}
	direct,err:=resolver.ResolveBoundProvenanceEvidence(ctx,prepared.Catalog.Actor,prepared.Catalog.Tenant,[]v2draft.LayerRef{root,child,style},source);if err!=nil{return v2draft.TaskInputEvidence{},nil,err}
	header:=v2draft.TaskInputEvidence{Entries:[]v2input.Entry{},Documents:[]v2draft.DocumentWitness{},Occurrences:[]v2draft.OccurrenceWitness{}}
	for _,item:=range []struct{node v2draft.LayerRef;raw []byte}{
		{v2draft.LayerRef{Path:"TaskInputs[0]",Contract:"FrozenRenderRecipe",Ref:prepared.Recipe},prepared.RecipeRaw},
		{v2draft.LayerRef{Path:"TaskInputs[0].recipe.executed_base_receipt",Contract:"EditorExecutedBaseReceipt",Ref:v2input.Ref{ArtifactID:prepared.Origin.ReceiptID,SHA256:prepared.Origin.ReceiptSHA},ParentRef:prepared.Recipe},prepared.ReceiptRaw},
	}{
		object,err:=prepared.Catalog.Lookup(ctx,prepared.Catalog.Actor,prepared.Catalog.Tenant,item.node);if err!=nil{return v2draft.TaskInputEvidence{},nil,err}
		raw,err:=readEditorFinalDocument(ctx,c.Objects,object,1<<20);if err!=nil{return v2draft.TaskInputEvidence{},nil,err};if !bytes.Equal(raw,item.raw){return invalid()}
		if _,err:=v2draft.DocumentChildren(raw,item.node.Contract,item.node.Ref);err!=nil{return invalid()}
		header.Entries=append(header.Entries,v2input.Entry{Ref:item.node.Ref,Contract:item.node.Contract})
		header.Documents=append(header.Documents,v2draft.DocumentWitness{Path:item.node.Path,Contract:item.node.Contract,Ref:item.node.Ref,Parent:item.node.ParentRef,Object:object,JCS:string(raw)})
		header.Occurrences=append(header.Occurrences,v2draft.OccurrenceWitness{Node:item.node,Object:object})
	}
	evidence,err:=mergeEditorFinalEvidence(header,direct,branches);if err!=nil{return v2draft.TaskInputEvidence{},nil,err}
	for _,document:=range direct.Documents{if document.Path==root.Path&&document.Ref==source.Source{return evidence,[]byte(document.JCS),nil}}
	return invalid()
}

