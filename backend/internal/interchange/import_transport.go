package interchange
import (
 "bytes"
 "context"
 "encoding/json"
 "sort"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
type ImportInputInventory struct{tenant uuid.UUID;documents ImportPreparedDocuments;entries []v2input.Entry;digest string}
type ImportTransport struct{inventory ImportInputInventory;built v2input.Built}
// The published Params and the separately owned evidence document must name
// the same immutable object before either can become an input-closure root.
func importParamsRoots(documents ImportPreparedDocuments)(v2input.Ref,error){
 var params struct{Parsed v2input.Ref `json:"parsed"`;Evidence *v2input.Ref `json:"evidence"`}
 if err:=json.Unmarshal(documents.params.Body,&params);err!=nil{return v2input.Ref{},err}
 if !validImportRef(params.Parsed)||(params.Evidence==nil)!=(documents.evidence==nil){return v2input.Ref{},v2input.ErrInvalid}
 if params.Evidence!=nil&&(!validImportRef(*params.Evidence)||*params.Evidence!=documents.evidence.Ref){return v2input.Ref{},v2input.ErrInvalid}
 return params.Parsed,nil
}
// FreezeImportInputs owns a compiler-resolved full closure, not a live project
// scan. Root membership is checked here; typed recursive qualification remains
// the resolver's obligation and is repeated by final adoption.
func FreezeImportInputs(tenant uuid.UUID,documents ImportPreparedDocuments,entries []v2input.Entry)(ImportInputInventory,error){
 if tenant==uuid.Nil||len(entries)<3||len(entries)>v2input.MaxEntries{return ImportInputInventory{},v2input.ErrInvalid}
 for _,doc:=range documents.Documents(){if err:=validateImportStorageDocument(documents.identity,doc);err!=nil{return ImportInputInventory{},err}}
 parsed,err:=importParamsRoots(documents);if err!=nil{return ImportInputInventory{},err}
 owned:=append([]v2input.Entry(nil),entries...);seen:=map[uuid.UUID]v2input.Entry{}
 for _,entry:=range owned{
  if entry.Ref.ArtifactID==uuid.Nil||len(entry.Ref.SHA256)!=64||len(entry.Contract)<1||len(entry.Contract)>120{return ImportInputInventory{},v2input.ErrInvalid}
  for _,c:=range entry.Ref.SHA256{if !(c>='0'&&c<='9'||c>='a'&&c<='f'){return ImportInputInventory{},v2input.ErrInvalid}}
  if _,exists:=seen[entry.Ref.ArtifactID];exists{return ImportInputInventory{},v2input.ErrInvalid};seen[entry.Ref.ArtifactID]=entry
 }
 for _,root:=range []v2input.Entry{{Ref:documents.params.Ref,Contract:"Params"},{Ref:documents.plan.Ref,Contract:"InterchangePlan"},{Ref:parsed,Contract:"ParsedInterchange"}}{
  if seen[root.Ref.ArtifactID]!=root{return ImportInputInventory{},v2input.ErrInvalid}
 }
 if documents.evidence!=nil&&seen[documents.evidence.Ref.ArtifactID]!=(v2input.Entry{Ref:documents.evidence.Ref,Contract:"FrozenInterchangeImportEvidence"}){return ImportInputInventory{},v2input.ErrInvalid}
 sort.Slice(owned,func(i,j int)bool{return bytes.Compare(owned[i].Ref.ArtifactID[:],owned[j].Ref.ArtifactID[:])<0})
 digest,err:=canonical.Hash(owned);if err!=nil{return ImportInputInventory{},err}
 return ImportInputInventory{tenant:tenant,documents:cloneImportPreparedDocuments(documents),entries:owned,digest:digest},nil
}
func(i ImportInputInventory)InputDigest()string{return i.digest}
// PublishInputs follows blocked-shell materialization, so scoped publisher
// reservations have an existing task owner. It does not activate that task.
func(i ImportInputInventory)PublishInputs(ctx context.Context,publisher v2input.Publisher)(ImportTransport,error){
 if err:=ctx.Err();err!=nil{return ImportTransport{},err}
 if i.tenant==uuid.Nil||i.digest==""||publisher==nil{return ImportTransport{},v2input.ErrInvalid}
 built,err:=v2input.BuildInputClosure(ctx,i.documents.TaskID(),&parseCursor{entries:append([]v2input.Entry(nil),i.entries...)},publisher)
 if err!=nil{return ImportTransport{},err}
 if built.Count!=len(i.entries)||built.SHA256!=i.digest{return ImportTransport{},v2input.ErrInvalid}
 seen:=map[uuid.UUID]bool{};for _,entry:=range i.entries{seen[entry.Ref.ArtifactID]=true}
 refs:=append([]v2input.Ref{built.Manifest},built.Chunks...)
 for _,ref:=range refs{if seen[ref.ArtifactID]{return ImportTransport{},v2input.ErrInvalid};seen[ref.ArtifactID]=true}
 return ImportTransport{inventory:i,built:built},nil
}
func(t ImportTransport)RegisterInputs(ctx context.Context,tx pgx.Tx,tenant,run uuid.UUID,parsedBodies ...[]byte)error{
 if err:=ctx.Err();err!=nil{return err}
 i:=t.inventory
 if tx==nil||tenant!=i.tenant||run!=i.documents.identity.Run||t.built.SHA256!=i.digest{return v2input.ErrInvalid}
 built:=t.built;built.Chunks=append([]v2input.Ref(nil),built.Chunks...)
 open:=func(ctx context.Context)(v2input.Cursor,error){if err:=ctx.Err();err!=nil{return nil,err};return &parseCursor{entries:append([]v2input.Entry(nil),i.entries...)},nil}
 parsed,err:=importParamsRoots(i.documents);if err!=nil{return err}
 if len(parsedBodies)!=1||rawSHA(parsedBodies[0])!=parsed.SHA256{return v2input.ErrInvalid}
 normalized,err:=canonical.Bytes(parsedBodies[0]);if err!=nil||!bytes.Equal(normalized,parsedBodies[0]){return v2input.ErrInvalid}
 roots:=[]v2input.Ref{i.documents.params.Ref,i.documents.plan.Ref,parsed}
 if i.documents.evidence!=nil{roots=append(roots,i.documents.evidence.Ref)}
 return v2input.RegisterInterchangeInTransaction(ctx,tx,tenant,run,i.documents.TaskID(),built,open,roots,parsedBodies[0])
}
