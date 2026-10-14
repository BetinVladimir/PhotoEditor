package interchange

import (
 "context"
 "strconv"
 "unicode/utf8"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

type importMetadataReceipt struct{
 ID uuid.UUID `json:"entry_id"`;Version uuid.UUID `json:"asset_version_id"`;Master v2input.Ref `json:"master"`
 Item uuid.UUID `json:"item_id"`;Rating int `json:"rating"`;Flag string `json:"flag"`;Label *string `json:"label,omitempty"`
}
type importMetadataDocument struct{
 Schema int `json:"schema_version"`;Workflow uuid.UUID `json:"workflow_id"`;Project uuid.UUID `json:"project_id"`
 Parsed v2input.Ref `json:"parsed"`;Collection uuid.UUID `json:"collection_id"`;Entries []importMetadataReceipt `json:"entries"`
}
type importRecipeDiagnostic struct{ID uuid.UUID `json:"entry_id"`;Reason string `json:"reason"`}
type importEvidenceDocument struct{
 Schema int `json:"schema_version"`;Workflow uuid.UUID `json:"workflow_id"`;Project uuid.UUID `json:"project_id"`
 Plan v2input.Ref `json:"plan"`;Parsed v2input.Ref `json:"parsed"`;Sources importSourcesDocument `json:"sources"`
 Metadata importMetadataDocument `json:"metadata"`;Diagnostics []importRecipeDiagnostic `json:"recipe_diagnostics"`
}

func(v ImportScopedDocumentValidator)importEvidenceChildren(ctx context.Context,entry v2input.Entry,raw []byte)([]v2input.Entry,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if !validImportRef(entry.Ref)||rawSHA(raw)!=entry.Ref.SHA256{return nil,v2input.ErrInvalid}
 var document importEvidenceDocument;var plan importPlanDocument;var parsed importParsedDocument
 if err:=decodeImportDocument(raw,&document);err!=nil{return nil,err}
 if err:=decodeImportDocument(v.planRaw,&plan);err!=nil{return nil,err}
 if err:=decodeImportDocument(v.parsedRaw,&parsed);err!=nil{return nil,err}
 if len(plan.Entries)!=len(parsed.Manifest.Entries){return nil,v2input.ErrInvalid}
 task:=uuid.NewSHA1(document.Workflow,[]byte("v2.interchange.import:0"))
 if document.Schema!=1||document.Workflow==uuid.Nil||document.Project!=v.roots.Project||document.Diagnostics==nil ||
 document.Plan!=(v2input.Ref{ArtifactID:uuid.NewSHA1(task,[]byte("interchange:plan:1")),SHA256:rawSHA(v.planRaw)}) ||
 entry.Ref.ArtifactID!=uuid.NewSHA1(task,[]byte("interchange:evidence:1")){return nil,v2input.ErrInvalid}
 sourceRaw,err:=canonical.Marshal(document.Sources);if err!=nil{return nil,err}
 sources,err:=freezeImportSources(ctx,document.Workflow,document.Project,document.Parsed,v.parsedRaw,sourceRaw);if err!=nil{return nil,err}
 if plan.Format!=sources.document.Format{return nil,v2input.ErrInvalid}
 metadata:=document.Metadata
 if metadata.Schema!=1||metadata.Workflow!=document.Workflow||metadata.Project!=document.Project||metadata.Parsed!=document.Parsed ||
 metadata.Collection!=uuid.NewSHA1(document.Workflow,[]byte("interchange:draft-collection:1"))||metadata.Entries==nil{return nil,v2input.ErrInvalid}
 rows:=map[uuid.UUID]importMetadataReceipt{}
 for _,row:=range metadata.Entries{if _,ok:=rows[row.ID];ok{return nil,v2input.ErrInvalid};rows[row.ID]=row}
 diagnostics:=map[uuid.UUID]string{};previous:=-1;ordinals:=map[uuid.UUID]int{}
 for ordinal,original:=range parsed.Manifest.Entries{ordinals[original.ID]=ordinal}
 for _,diagnostic:=range document.Diagnostics{
  ordinal,ok:=ordinals[diagnostic.ID]
  if !ok||ordinal<=previous||!utf8.ValidString(diagnostic.Reason)||utf8.RuneCountInString(diagnostic.Reason)<1||utf8.RuneCountInString(diagnostic.Reason)>200{return nil,v2input.ErrInvalid}
  previous=ordinal;diagnostics[diagnostic.ID]=diagnostic.Reason
 }
 refs:=map[uuid.UUID]v2input.Entry{}
 add:=func(ref v2input.Ref,contract string)error{if !validImportRef(ref){return v2input.ErrInvalid};child:=v2input.Entry{Ref:ref,Contract:contract};if old,ok:=refs[ref.ArtifactID];ok&&old!=child{return v2input.ErrInvalid};refs[ref.ArtifactID]=child;return nil}
 if err:=add(document.Plan,"InterchangePlan");err!=nil{return nil,err};if err:=add(document.Parsed,"ParsedInterchange");err!=nil{return nil,err}
 for ordinal,source:=range sources.document.Entries{
  if err:=ctx.Err();err!=nil{return nil,err}
  item:=plan.Entries[ordinal];original:=parsed.Manifest.Entries[ordinal]
  if source.State!="adopted"{if item.Binding!=nil||item.Recipe!=nil{return nil,v2input.ErrInvalid};continue}
  if item.Binding==nil||item.Binding.AssetVersionID!=*source.Version||item.Binding.Source!=*source.Master{return nil,v2input.ErrInvalid}
  row,ok:=rows[source.ID];rating:=original.Rating;if rating<0{rating=0}
  flag:=map[string]string{"picked":"pick","rejected":"reject","unmarked":"none"}[original.Pick]
  run:=uuid.NewSHA1(document.Workflow,[]byte("interchange:canonical-ingest:1"))
  if !ok||row.Version!=*source.Version||row.Master!=*source.Master||row.Item!=uuid.NewSHA1(run,[]byte("item:"+strconv.Itoa(ordinal)))||row.Rating!=rating||row.Flag!=flag||!validImportColorLabel(row.Label)||!sameImportColorLabel(row.Label,original.Label)||!sameImportColorLabel(item.Label,original.Label){return nil,v2input.ErrInvalid}
  delete(rows,source.ID)
  if item.Recipe!=nil&&original.RecipePath==nil{return nil,v2input.ErrInvalid}
  if item.Recipe==nil&&original.RecipePath!=nil{if _,ok:=diagnostics[source.ID];!ok{return nil,v2input.ErrInvalid};delete(diagnostics,source.ID)}
  if err:=add(*source.Master,"WorkingImage");err!=nil{return nil,err}
 }
 if len(rows)!=0||len(diagnostics)!=0{return nil,v2input.ErrInvalid}
 children:=make([]v2input.Entry,0,len(refs));for _,child:=range refs{children=append(children,child)}
 return children,ctx.Err()
}
