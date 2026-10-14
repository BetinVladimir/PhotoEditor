package interchange

import (
 "bytes"
 "context"
 "encoding/json"
 "strconv"

 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/idempotency"
)

type ImportDecision struct { EntryID uuid.UUID `json:"entry_id"`; Decision string `json:"decision"` }
type ImportReviewRequest struct { ExpectedRevision string `json:"expected_revision"`; ReportSHA string `json:"report_sha"`; Decisions []ImportDecision `json:"decisions"` }
type ImportReviewCloseRequest struct { ExpectedRevision string `json:"expected_revision"`; ReportSHA string `json:"report_sha"` }

func strictReviewDecode(raw []byte, target any)error{
 if len(raw)<2||len(raw)>65536{return domain.NewError("VALIDATION_FAILED",422)}
 normalized,err:=canonical.Bytes(raw);if err!=nil{return domain.NewError("VALIDATION_FAILED",422)}
 decoder:=json.NewDecoder(bytes.NewReader(normalized));decoder.DisallowUnknownFields()
 if err:=decoder.Decode(target);err!=nil{return domain.NewError("VALIDATION_FAILED",422)}
 encoded,err:=canonical.Marshal(target);if err!=nil||!bytes.Equal(encoded,normalized){return domain.NewError("VALIDATION_FAILED",422)}
 return nil
}
func(r *ImportReviewRequest)UnmarshalJSON(raw []byte)error{
 type wire ImportReviewRequest;var decoded wire
 if err:=strictReviewDecode(raw,&decoded);err!=nil{return err}
 if _,err:=validateReviewRevision(decoded.ExpectedRevision,decoded.ReportSHA);err!=nil{return err}
 if len(decoded.Decisions)<1||len(decoded.Decisions)>100{return domain.NewError("VALIDATION_FAILED",422)}
 seen:=map[uuid.UUID]bool{}
 for _,decision:=range decoded.Decisions{if decision.EntryID==uuid.Nil||seen[decision.EntryID]||(decision.Decision!="approve"&&decision.Decision!="reject"){return domain.NewError("VALIDATION_FAILED",422)};seen[decision.EntryID]=true}
 *r=ImportReviewRequest(decoded);return nil
}
func(r *ImportReviewCloseRequest)UnmarshalJSON(raw []byte)error{
 type wire ImportReviewCloseRequest;var decoded wire
 if err:=strictReviewDecode(raw,&decoded);err!=nil{return err}
 if _,err:=validateReviewRevision(decoded.ExpectedRevision,decoded.ReportSHA);err!=nil{return err};*r=ImportReviewCloseRequest(decoded);return nil
}
func validateReviewRevision(revision,sha string)(int64,error){
 value,err:=strconv.ParseInt(revision,10,64)
 if err!=nil||value<1||value>=9007199254740991||strconv.FormatInt(value,10)!=revision||!validImportRefForSHA(sha){return 0,domain.NewError("VALIDATION_FAILED",422)}
 return value,nil
}
func validImportRefForSHA(sha string)bool{if len(sha)!=64{return false};for _,c:=range sha{if !(c>='0'&&c<='9'||c>='a'&&c<='f'){return false}};return true}

// FrozenReviewIntent owns canonical request and immutable review_selection bytes.
// Publication and final SQL commit must independently recheck current authority.
type FrozenReviewIntent struct { workflow,tenant,owner uuid.UUID;actor auth.Identity;action,key,requestSHA,documentSHA string;revision int64;request,document []byte }
func(i FrozenReviewIntent)RequestBytes()[]byte{return append([]byte(nil),i.request...)}
func(i FrozenReviewIntent)DocumentBytes()[]byte{return append([]byte(nil),i.document...)}
func(i FrozenReviewIntent)OwnerID()uuid.UUID{return i.owner}
func(i FrozenReviewIntent)RequestSHA()string{return i.requestSHA}
func(i FrozenReviewIntent)DocumentSHA()string{return i.documentSHA}
func(i FrozenReviewIntent)ExpectedRevision()int64{return i.revision}

func FreezeImportReviewIntent(ctx context.Context,tid,workflow uuid.UUID,who auth.Identity,key,action string,raw []byte)(FrozenReviewIntent,error){
 if err:=ctx.Err();err!=nil{return FrozenReviewIntent{},err}
 if tid==uuid.Nil||workflow==uuid.Nil||who.UserID==uuid.Nil||who.SessionID==uuid.Nil||!idempotency.ValidKey(key){return FrozenReviewIntent{},domain.NewError("VALIDATION_FAILED",422)}
 var request any;var revision,report string
 switch action{
 case "decide":var value ImportReviewRequest;if err:=json.Unmarshal(raw,&value);err!=nil{return FrozenReviewIntent{},err};request=value;revision=value.ExpectedRevision;report=value.ReportSHA
 case "close":var value ImportReviewCloseRequest;if err:=json.Unmarshal(raw,&value);err!=nil{return FrozenReviewIntent{},err};request=value;revision=value.ExpectedRevision;report=value.ReportSHA
 default:return FrozenReviewIntent{},domain.NewError("VALIDATION_FAILED",422)
 }
 body,err:=canonical.Marshal(map[string]any{"workflow_id":workflow,"request":request});if err!=nil{return FrozenReviewIntent{},err}
 document:=map[string]any{"schema_version":1,"kind":"interchange_review_"+action,"workflow_id":workflow,"expected_revision":revision,"report_sha":report}
 if value,ok:=request.(ImportReviewRequest);ok{document["decisions"]=value.Decisions}
 encoded,err:=canonical.Marshal(document);if err!=nil{return FrozenReviewIntent{},err}
 numeric,_:=validateReviewRevision(revision,report);sha:=rawSHA(body)
 owner:=uuid.NewSHA1(workflow,[]byte("interchange:review:"+action+":"+who.UserID.String()+":"+sha))
 return FrozenReviewIntent{workflow:workflow,tenant:tid,owner:owner,actor:who,action:action,key:key,requestSHA:sha,documentSHA:rawSHA(encoded),revision:numeric,request:body,document:encoded},nil
}
