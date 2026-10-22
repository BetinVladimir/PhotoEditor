package v2draft

import (
 "encoding/json"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

func TestConservativeDefectParamsExposeTypedReport(t *testing.T){
 ref:=func(label string)v2input.Ref{sha,err:=canonical.Hash(label);if err!=nil{t.Fatal(err)};return v2input.Ref{ArtifactID:uuid.New(),SHA256:sha}}
 image,proposal,report,resolved,parent:=ref("image"),ref("proposal"),ref("report"),ref("resolved"),ref("parent")
 params:=map[string]any{"policy":"conservative_auto_v1","image":image,"proposal":proposal,"proposal_report":report,"resolved":resolved}
 raw,err:=canonical.Marshal(params);if err!=nil{t.Fatal(err)}
 children,err:=DocumentChildren(raw,"Params",parent)
 want:=[]string{"WorkingImage","DefectCandidates","DefectProposeReport","ResolvedStyle"}
 if err!=nil||len(children)!=len(want){t.Fatal(children,err)}
 for i:=range want{if children[i].Contract!=want[i]||children[i].ParentRef!=parent{t.Fatal(children)}}
 for _,mode:=range []string{"human_review","missing_report","null_proposal","unknown_policy"}{t.Run(mode,func(t *testing.T){
  var fields map[string]json.RawMessage;if json.Unmarshal(raw,&fields)!=nil{t.Fatal("fixture")}
  switch mode{case "human_review":fields["review"]=json.RawMessage(`{}`);case "missing_report":delete(fields,"proposal_report");case "null_proposal":fields["proposal"]=json.RawMessage(`null`);case "unknown_policy":fields["policy"]=json.RawMessage(`"unknown"`)}
  body,err:=canonical.Marshal(fields);if err!=nil{t.Fatal(err)}
  if children,err:=DocumentChildren(body,"Params",parent);err==nil||children!=nil{t.Fatal("malformed automatic contract accepted")}
 })}
}

func TestConservativeProjectionDoesNotClaimMetadataPolicy(t *testing.T){
 raw:=[]byte(`{"image":{},"policy":"copyright_only"}`)
 if _,handled,err:=conservativeParamRefs(raw,"Document",v2input.Ref{});handled||err!=nil{t.Fatal("unrelated Params intercepted",err)}
}
