package review

import (
 "bytes"
 "context"
 "encoding/json"
 "os"
 "testing"
)

func TestDefectSelectionStructureAndProposalBinding(t *testing.T) {
 raw,err:=os.ReadFile("testdata/defect-selection-python.json");if err!=nil {t.Fatal(err)}
 proposalRaw,proposalRef,source:=proposalFixture(t)
 proposal,err:=ReadDefectProposal(context.Background(),bytes.NewReader(proposalRaw),proposalRef,source);if err!=nil {t.Fatal(err)}
 for _,mode:=range []string{"valid","pending","duplicate","overlap","unknown","revision","null","extra","hash","foreign"} {t.Run(mode,func(t *testing.T){
  var doc map[string]any
  if err:=json.Unmarshal(raw,&doc);err!=nil {t.Fatal(err)}
  ids:=doc["accepted_candidate_ids"].([]any)
  switch mode {
  case "pending":doc["accepted_candidate_ids"]=[]any{}
  case "duplicate":doc["accepted_candidate_ids"]=append(ids,ids[0])
  case "overlap":doc["rejected_candidate_ids"]=ids
  case "unknown":doc["accepted_candidate_ids"]=[]any{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
  case "revision":doc["revision"]=0
  case "null":doc["rejected_candidate_ids"]=nil
  case "extra":doc["extra"]=true
  case "foreign":doc["image"].(map[string]any)["sha256"]="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
  }
  body,err:=json.Marshal(doc);if err!=nil {t.Fatal(err)}
  ref:=proposalRefForSelection(body)
  if mode=="hash" {body=append(body,' ')}
  selection,err:=ParseDefectSelection(body,ref)
  structural:=mode=="valid"||mode=="pending"||mode=="unknown"||mode=="foreign"
  if !structural {if err==nil {t.Fatal("invalid selection accepted")};return}
  if err!=nil {t.Fatal(err)}
  err=proposal.ValidateSelection(selection)
  if mode=="unknown"||mode=="foreign" {if err==nil {t.Fatal("foreign decision accepted")}} else if err!=nil {t.Fatal(err)}
 })}
}

func proposalRefForSelection(raw []byte) ArtifactRef {return proposalRef(raw)}

func TestSelectionDirectValidationUsesSameStructuralRules(t *testing.T) {
 p,s,_,_,_,_:=repairFixture(t)
 for _,mode:=range []string{"duplicate","overlap","null","oversized","version","revision"} {t.Run(mode,func(t *testing.T){
  selection:=s
  selection.Accepted=append([]string(nil),s.Accepted...)
  selection.Rejected=append([]string{},s.Rejected...)
  switch mode {
  case "duplicate":selection.Accepted=append(selection.Accepted,selection.Accepted[0])
  case "overlap":selection.Rejected=append(selection.Rejected,selection.Accepted[0])
  case "null":selection.Rejected=nil
  case "oversized":selection.Accepted=make([]string,4097)
  case "version":selection.Version="next"
  case "revision":selection.Revision=0
  }
  if err:=p.ValidateSelection(selection);err==nil {t.Fatal("invalid direct selection accepted")}
 })}
}
