package workflow

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "strings"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
)

// Feature packs keep declared task inputs below the transport limit (128),
// including for a full 5,000 image immutable Apply snapshot.
const applyCullPackSize=80

type applyCullSettings struct { Enabled bool `json:"enabled"`; TargetCount *int `json:"target_count"`; TargetFraction *float64 `json:"target_fraction"`; NeverDelete bool `json:"never_delete"`; EyePolicy string `json:"eye_policy"` }
type applyCullStyle struct { Culling applyCullSettings `json:"culling"`;Extensions struct{ Culling *applyCullSettings `json:"culling"` } `json:"extensions"` }
func readApplyCullStyle(raw json.RawMessage)(applyCullStyle,error){var s applyCullStyle;e:=json.Unmarshal(raw,&s);if s.Extensions.Culling!=nil{s.Culling=*s.Extensions.Culling};return s,e}


func applyCullProfile(operation,backend,lock string,pixels,inputBytes,paramsBytes int64)(json.RawMessage,error){
 if pixels<1 || pixels>200000000 || inputBytes<0 || paramsBytes<1 {return nil,errors.New("invalid culling resources")}
 if operation=="features.scene" { return canonical.Marshal(struct { Backend string `json:"backend"`; Precision string `json:"precision"`; ModelLock string `json:"model_lock_id"`; Estimate ResourceEstimate `json:"resource_estimate"` }{backend,"float32",lock,ResourceEstimate{RAMBytes:(1<<30)+pixels*96,VRAMBytes:map[string]int64{"cpu":0,"cuda":1<<30,"mps":1<<30}[backend],ScratchBytes:inputBytes+pixels*12+paramsBytes+(1<<20),OutputBytes:1<<20,PixelCount:pixels}}) }
 ram:=int64(1<<30);output:=int64(8<<20)
 if operation=="collection.features" {ram=64<<20;output=1<<20}
 return canonical.Marshal(struct{Backend string `json:"backend"`;Precision string `json:"precision"`;Estimate ResourceEstimate `json:"resource_estimate"`}{"cpu","float32",ResourceEstimate{RAMBytes:ram,ScratchBytes:inputBytes+pixels*12+paramsBytes+output,OutputBytes:output,PixelCount:pixels}})
}

func(c ApplyCoordinator) buildCulling(ctx context.Context,l ApplyLease,reg Registry,items []applyInput,style json.RawMessage,backend,lock string)(bool,error){
 cfg,parseErr:=readApplyCullStyle(style)
 if parseErr!=nil || !cfg.Culling.Enabled || !cfg.Culling.NeverDelete || cfg.Culling.EyePolicy!="review" || len(items)<1 || len(items)>5000 {return false,errors.New("invalid culling style")}
 if l.Phase=="cull_review" {return c.processCullingReview(ctx,l,items)}
 operation:=map[string]string{"cull_embedding":"features.scene","cull_features":"collection.features","cull_pack":"collection.features_pack","cull_group":"collection.group","cull_rank":"collection.cull"}[l.Phase]
 if operation=="" {return false,errors.New("invalid culling phase")}
 outputs:=func(op,role string)([]applyOutput,error){return c.Store.Outputs(ctx,l,op,role)}
 register:=func(ordinal int,item applyInput,params any,inputs []review.ArtifactRef,prereq []uuid.UUID)error{
  ok,e:=c.Store.Renew(ctx,l);if e!=nil||!ok{return errors.New("culling lease lost")}
  raw,e:=canonical.Marshal(params);if e!=nil{return e};sum:=sha256.Sum256(raw);key:=fmt.Sprintf("%s/%04d",operation,ordinal)
  profile,e:=applyCullProfile(operation,backend,lock,item.Working.Canvas.Width*item.Working.Canvas.Height,item.Image.Bytes+int64(len(inputs))*1048576,int64(len(raw)));if e!=nil{return e}
  profile,e=c.qualifyMixedProfile(item.Config,l.TenantID,operation,profile);if e!=nil{return e}
  return c.materializeNode(ctx,l,reg,applyNodePlan{nodeID(l.RunID,key),l.Phase,ordinal,item.ItemID,item.AssetID,item.VersionID,key,operation,inputs,prereq,hex.EncodeToString(sum[:]),raw},profile)
 }
 if l.Phase=="cull_embedding" {
  previous,e:=outputs("style.resolve","resolved_style");if e!=nil||len(previous)!=len(items){return false,errors.New("culling resolve inputs missing")}
  by:=map[uuid.UUID]applyOutput{};for _,out:=range previous{by[out.ItemID]=out}
  for _,item:=range items {p,ok:=by[item.ItemID];if !ok{return false,errors.New("culling item binding missing")};if e=register(item.Ordinal,item,map[string]any{"image":item.Image.ref(),"model_id":"dinov2_vits14","model_lock_id":lock,"preprocessing_id":"dinov2_pad518_v1","input_size":518},refsForImage(item.Image.ref(),item.Working),[]uuid.UUID{p.TaskID});e!=nil{return false,e}}
 } else if l.Phase=="cull_features" {
  embeddings,e:=outputs("features.scene","scene_embedding");if e!=nil||len(embeddings)!=len(items){return false,errors.New("culling embeddings missing")};stats,e:=outputs("color.stats","stats");if e!=nil||len(stats)!=len(items){return false,errors.New("culling stats missing")}
  eb,sb:=map[uuid.UUID]applyOutput{},map[uuid.UUID]applyOutput{};for _,o:=range embeddings{eb[o.ItemID]=o};for _,o:=range stats{sb[o.ItemID]=o}
  for _,item:=range items {a,aok:=eb[item.ItemID];b,bok:=sb[item.ItemID];if !aok||!bok{return false,errors.New("culling measurement item mismatch")};inputs:=append(refsForImage(item.Image.ref(),item.Working),a.Artifact.ref(),b.Artifact.ref());if e=register(item.Ordinal,item,map[string]any{"item_id":item.ItemID,"image":item.Image.ref(),"embedding":a.Artifact.ref(),"stats":b.Artifact.ref()},inputs,[]uuid.UUID{a.TaskID,b.TaskID});e!=nil{return false,e}}
 } else if l.Phase=="cull_pack" {
  features,e:=outputs("collection.features","scene_features");if e!=nil||len(features)!=len(items){return false,errors.New("culling feature inventory missing")}
  count:=(len(features)+applyCullPackSize-1)/applyCullPackSize;ok,e:=c.Store.SetGroupCount(ctx,l,count);if e!=nil||!ok{return false,errors.New("culling pack count rejected")}
  for start:=0;start<len(features);start+=applyCullPackSize {end:=start+applyCullPackSize;if end>len(features){end=len(features)};refs:=[]review.ArtifactRef{};prereq:=[]uuid.UUID{};for _,o:=range features[start:end]{refs=append(refs,o.Artifact.ref());prereq=append(prereq,o.TaskID)};if e=register(start/applyCullPackSize,items[0],map[string]any{"features":refs},refs,prereq);e!=nil{return false,e}}
 } else {
  packs,e:=outputs("collection.features_pack","scene_feature_pack");if e!=nil||len(packs)!=(len(items)+applyCullPackSize-1)/applyCullPackSize{return false,errors.New("culling packs missing")};refs:=[]review.ArtifactRef{};prereq:=[]uuid.UUID{};for _,o:=range packs{refs=append(refs,o.Artifact.ref());prereq=append(prereq,o.TaskID)}
  params:=map[string]any{"feature_batches":refs};inputs:=append([]review.ArtifactRef{},refs...)
  if l.Phase=="cull_rank" {groups,e:=outputs("collection.group","scene_groups");if e!=nil||len(groups)!=1{return false,errors.New("culling groups missing")};params["groups"]=groups[0].Artifact.ref();params["never_delete"]=true;params["eye_policy"]="review";inputs=append(inputs,groups[0].Artifact.ref());prereq=append(prereq,groups[0].TaskID)
   if cfg.Culling.TargetFraction!=nil {params["target_fraction"]=*cfg.Culling.TargetFraction} else {target:=len(items);if cfg.Culling.TargetCount!=nil&&*cfg.Culling.TargetCount<target{target=*cfg.Culling.TargetCount};if target<1{return false,errors.New("invalid culling target")};params["target_count"]=target}
  }
  if e=register(0,items[0],params,inputs,prereq);e!=nil{return false,e}
 }
 ok,e:=c.Store.Advance(ctx,l,"");if e!=nil||!ok{return false,errors.New("culling stage advance rejected")};return true,nil
}

func readApplyCullingJSON(ctx context.Context,c ApplyCoordinator,a styleArtifact)([]byte,error){
 // Aggregated 5,000-item contracts exceed the normal one MiB item document.
 if a.Bytes>8<<20{return nil,errors.New("culling document too large")}
 return readApplyJSONBound(ctx,c.Objects,c.Bucket,a,8<<20)
}

func(c ApplyCoordinator) processCullingReview(ctx context.Context,l ApplyLease,items []applyInput)(bool,error){
 advanced,processErr:=c.Store.ProcessCullingReview(ctx,l);if processErr!=nil{return false,processErr};if advanced{return true,nil}
 groups,e:=c.Store.Outputs(ctx,l,"collection.group","scene_groups");if e!=nil||len(groups)!=1{return false,errors.New("culling review groups missing")};proposal,e:=c.Store.Outputs(ctx,l,"collection.cull","cull_recommendations");if e!=nil||len(proposal)!=1{return false,errors.New("culling review proposal missing")}
 gr,e:=readApplyCullingJSON(ctx,c,groups[0].Artifact);if e!=nil{return false,e};pr,e:=readApplyCullingJSON(ctx,c,proposal[0].Artifact);if e!=nil{return false,e}
 projection,e:=applyCullingProjection(items,gr,pr,groups[0].Artifact.ref(),proposal[0].Artifact.ref());if e!=nil{return false,e}
 opened,e:=c.Store.RegisterCullingReview(ctx,l,projection);if e!=nil||!opened{return false,errors.New("culling review registration rejected")};return true,nil
}

func applyCullingProjection(items []applyInput,groupsRaw,proposalRaw []byte,groups,proposal review.ArtifactRef)(json.RawMessage,error){
 var g struct{Items []struct{ItemID uuid.UUID `json:"item_id"`;Scene string `json:"scene_id"`;Exact string `json:"exact_group_id"`;Burst string `json:"burst_group_id"`} `json:"items"`}
 var p struct{Groups review.ArtifactRef `json:"groups"`;NeverDelete bool `json:"never_delete"`;Items []struct{ItemID uuid.UUID `json:"item_id"`;Scene string `json:"scene_id"`;Rank int `json:"rank"`;Score float64 `json:"score"`;Selected bool `json:"selected"`;Manual bool `json:"manual"`;EyeReview bool `json:"eye_review"`} `json:"items"`}
 if json.Unmarshal(groupsRaw,&g)!=nil||json.Unmarshal(proposalRaw,&p)!=nil||p.Groups!=groups||!p.NeverDelete||len(g.Items)!=len(items)||len(p.Items)!=len(items){return nil,errors.New("invalid culling inventory")}
 gm:=map[uuid.UUID]int{};pm:=map[uuid.UUID]int{};for i,v:=range g.Items{if _,ok:=gm[v.ItemID];ok{return nil,errors.New("duplicate group item")};gm[v.ItemID]=i};for i,v:=range p.Items{if _,ok:=pm[v.ItemID];ok{return nil,errors.New("duplicate recommendation item")};pm[v.ItemID]=i}
 rows:=[]map[string]any{};seen:=map[uuid.UUID]bool{}
 for _,item:=range items{gi,gok:=gm[item.ItemID];pi,pok:=pm[item.ItemID];if !gok||!pok||seen[item.ItemID]{return nil,errors.New("culling snapshot item mismatch")};seen[item.ItemID]=true;a,b:=g.Items[gi],p.Items[pi];if a.Scene!=b.Scene||strings.TrimSpace(a.Scene)==""||a.Exact==""||a.Burst==""||b.Rank<1||b.Rank>len(items)||b.Score<0||b.Score>1||!b.EyeReview{return nil,errors.New("invalid culling ranking")}
 rows=append(rows,map[string]any{"item_id":item.ItemID,"asset_id":item.AssetID,"asset_version_id":item.VersionID,"scene_id":a.Scene,"exact_group_id":a.Exact,"burst_group_id":a.Burst,"rank":b.Rank,"score":b.Score,"recommended":b.Selected,"manual":b.Manual,"eye_review":true,"eye_state":"unknown"})}
 return canonical.Marshal(map[string]any{"groups":groups,"proposal":proposal,"items":rows,"never_delete":true})
}

func applyStyleCullingEnabled(raw json.RawMessage)bool{ s,e:=readApplyCullStyle(raw);return e==nil&&s.Culling.Enabled }
