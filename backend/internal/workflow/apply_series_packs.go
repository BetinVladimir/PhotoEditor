package workflow

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
)

func(c ApplyCoordinator) buildSeriesPacks(ctx context.Context,l ApplyLease,reg Registry,items []applyInput)(bool,error){
 stats,e:=c.Store.Outputs(ctx,l,"series.measure","series_stats");if e!=nil||len(stats)!=len(items)||len(items)<2||len(items)>5000{return false,errors.New("invalid series pack source count")}
 count:=(len(stats)+79)/80;ok,e:=c.Store.SetGroupCount(ctx,l,count);if e!=nil||!ok{return false,errors.New("series pack cardinality rejected")}
 for start:=0;start<len(stats);start+=80{
  end:=start+80;if end>len(stats){end=len(stats)};refs:=[]review.ArtifactRef{};prereq:=[]uuid.UUID{};inputBytes:=int64(0)
  for _,out:=range stats[start:end]{refs=append(refs,out.Artifact.ref());prereq=append(prereq,out.TaskID);inputBytes+=out.Artifact.Bytes}
  raw,e:=canonical.Marshal(map[string]any{"stats":refs});if e!=nil{return false,e};sum:=sha256.Sum256(raw);key:=fmt.Sprintf("series.pack/%04d",start/80);item:=items[0]
  ok,e=c.Store.Renew(ctx,l);if e!=nil||!ok{return false,errors.New("series pack lease lost")}
  profile,e:=canonical.Marshal(struct{Backend string `json:"backend"`;Precision string `json:"precision"`;Estimate ResourceEstimate `json:"resource_estimate"`}{"cpu","float32",ResourceEstimate{RAMBytes:256<<20,ScratchBytes:inputBytes+int64(len(raw))+(1<<20),OutputBytes:1<<20,PixelCount:1}});if e!=nil{return false,e}
  if e=c.materializeNode(ctx,l,reg,applyNodePlan{nodeID(l.RunID,key),l.Phase,start/80,item.ItemID,item.AssetID,item.VersionID,key,"series.pack",refs,prereq,hex.EncodeToString(sum[:]),raw},profile);e!=nil{return false,e}
 }
 ok,e=c.Store.Advance(ctx,l,"");if e!=nil||!ok{return false,errors.New("series pack advance rejected")};return true,nil
}

// Preserve the existing model/runtime/device authorization fields while
// admitting all immutable pack bytes and the full 5,000-item correction report.
func applySeriesAggregateResources(profile json.RawMessage,packs []applyOutput,paramsBytes int64)(json.RawMessage,error){
 var value map[string]json.RawMessage;if json.Unmarshal(profile,&value)!=nil||len(packs)<1||len(packs)>63||paramsBytes<1{return nil,errors.New("invalid series aggregate profile")}
 var estimate ResourceEstimate;if json.Unmarshal(value["resource_estimate"],&estimate)!=nil{return nil,errors.New("invalid series estimate")}
 bytes:=int64(0);for _,p:=range packs{if p.Artifact.Bytes<1||p.Artifact.Bytes>1<<20{return nil,errors.New("invalid series stats pack size")};bytes+=p.Artifact.Bytes}
 estimate.ScratchBytes=bytes+paramsBytes+(8<<20);estimate.OutputBytes=8<<20
 raw,e:=canonical.Marshal(estimate);if e!=nil{return nil,e};value["resource_estimate"]=raw;return canonical.Marshal(value)
}
