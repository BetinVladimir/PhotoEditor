package review

import (
 "context"
 "encoding/json"
 "testing"
)

func TestDefectProposalStructureChecksChainAndOriginalHash(t *testing.T) {
 raw,ref,_:=proposalFixture(t)
 if _,err:=ParseDefectProposalStructure(context.Background(),raw,ref);err!=nil {t.Fatal(err)}
 changed:=append(append([]byte(nil),raw...), ' ')
 if _,err:=ParseDefectProposalStructure(context.Background(),changed,ref);err==nil {t.Fatal("changed original bytes accepted")}
 var doc map[string]any
 if err:=json.Unmarshal(raw,&doc);err!=nil {t.Fatal(err)}
 doc["transform_chain"].(map[string]any)["hash"]="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
 changed,_=json.Marshal(doc)
 if _,err:=ParseDefectProposalStructure(context.Background(),changed,proposalRef(changed));err==nil {t.Fatal("invalid chain accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=ParseDefectProposalStructure(ctx,raw,ref);err!=context.Canceled {t.Fatal("cancellation lost",err)}
}
