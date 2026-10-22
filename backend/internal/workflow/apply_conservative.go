package workflow

import (
 "bytes"
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

const conservativeApplyPolicy = "conservative_auto_v1"

// A sibling report is admitted only from the same canonical attempt as the
// already lease-scoped proposal. Never accept a same-role artifact from a retry.
func (s ApplyStore) conservativeProposalReport(ctx context.Context,l ApplyLease,proposal applyOutput)(styleArtifact,error){
 var report styleArtifact
 err:=s.DB.QueryRow(ctx,`SELECT a.id,a.sha256,a.bytes,a.bucket,a.object_key,a.object_version,a.media_type
 FROM photo.apply_builds b JOIN photo.tasks t ON t.tenant_id=b.tenant_id AND t.id=$4 AND t.run_id=b.run_id
 JOIN photo.task_results r ON r.tenant_id=t.tenant_id AND r.task_id=t.id AND r.id=t.canonical_result_id
 JOIN photo.artifacts p ON p.tenant_id=t.tenant_id AND p.id=$5 AND p.created_by_task_id=t.id
  AND p.produced_attempt=r.attempt AND p.produced_fence=r.fencing_token AND p.sha256=$6 AND p.state='ready'
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.created_by_task_id=t.id
  AND a.produced_attempt=r.attempt AND a.produced_fence=r.fencing_token AND a.output_role='defect_propose_report'
 WHERE b.tenant_id=$1 AND b.run_id=$2 AND b.lease_token=$3 AND b.lease_until>=clock_timestamp()
  AND b.graph_version=4 AND t.operation='defects.propose' AND t.state='succeeded' AND a.state='ready'
  AND a.object_version IS NOT NULL AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
  AND r.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',p.id,'sha256',p.sha256),jsonb_build_object('artifact_id',a.id,'sha256',a.sha256))`,l.TenantID,l.RunID,l.Token,proposal.TaskID,proposal.Artifact.ArtifactID,proposal.Artifact.SHA256).Scan(&report.ArtifactID,&report.SHA256,&report.Bytes,&report.Bucket,&report.Key,&report.VersionID,&report.MediaType)
 return report,err
}

func applyConservativeProfile(pixels,inputBytes int64)(json.RawMessage,error){
 if pixels<1||pixels>200000000||inputBytes<1{return nil,errors.New("invalid conservative resource dimensions")}
 ram:=pixels*128+(256<<20);if ram<1<<30{ram=1<<30};output:=pixels*12+(2<<20)
 return canonical.Marshal(struct{Backend string `json:"backend"`;Precision string `json:"precision"`;Tile int `json:"tile_size"`;Estimate ResourceEstimate `json:"resource_estimate"`}{"cpu","float32",512,ResourceEstimate{RAMBytes:ram,ScratchBytes:inputBytes+output,OutputBytes:output,PixelCount:pixels}})
}

func (c ApplyCoordinator) conservativeDefectParams(ctx context.Context,l ApplyLease,proposal applyOutput,itemID uuid.UUID)([]byte,[]review.ArtifactRef,[]uuid.UUID,int64,int64,error){
 fail:=func(err error)([]byte,[]review.ArtifactRef,[]uuid.UUID,int64,int64,error){return nil,nil,nil,0,0,err}
 report,err:=c.Store.conservativeProposalReport(ctx,l,proposal);if err!=nil{return fail(err)}
 raw,err:=readApplyJSON(ctx,c.Objects,c.Bucket,report);if err!=nil{return fail(err)}
 var document struct{Proposal review.ArtifactRef `json:"proposal"`;Params struct{Image review.ArtifactRef `json:"image"`} `json:"params"`;Protection review.MaskRef `json:"protection"`}
 if json.Unmarshal(raw,&document)!=nil||document.Proposal!=proposal.Artifact.ref()||!document.Protection.Protected{return fail(errors.New("invalid conservative proposal report"))}
 image,err:=c.Store.Artifact(ctx,l.TenantID,document.Params.Image);if err!=nil{return fail(err)}
 working,err:=readWorking(ctx,c.Objects,c.Bucket,image);if err!=nil{return fail(err)}
 if document.Protection.Canvas!=working.Canvas||!bytes.Equal(document.Protection.TransformChain,working.TransformChain){return fail(errors.New("conservative protection geometry mismatch"))}
 resolved,err:=c.Store.Outputs(ctx,l,"style.resolve","resolved_style");if err!=nil{return fail(err)}
 var selected applyOutput;for _,candidate:=range resolved{if candidate.ItemID==itemID{if selected.TaskID!=uuid.Nil{return fail(errors.New("duplicate conservative resolved binding"))};selected=candidate}}
 if selected.TaskID==uuid.Nil{return fail(errors.New("conservative resolved binding missing"))}
 proposalRaw,err:=readApplyJSON(ctx,c.Objects,c.Bucket,proposal.Artifact);if err!=nil{return fail(err)}
 var candidate struct{Image review.ArtifactRef `json:"image"`;Pixels review.ArtifactRef `json:"source_pixels"`;Labels review.ArtifactRef `json:"label_map"`}
 if json.Unmarshal(proposalRaw,&candidate)!=nil||candidate.Image!=image.ref()||candidate.Pixels!=working.Pixels||candidate.Labels.ID==uuid.Nil{return fail(errors.New("conservative proposal source mismatch"))}
 // Spell each wire key explicitly: the automatic contract has no human review.
 params,err:=canonical.Marshal(map[string]any{"policy":conservativeApplyPolicy,"image":image.ref(),"proposal":proposal.Artifact.ref(),"proposal_report":report.ref(),"resolved":selected.Artifact.ref()})
 if err!=nil{return fail(err)}
 inputs:=append(refsForImage(image.ref(),working),proposal.Artifact.ref(),report.ref(),selected.Artifact.ref(),candidate.Labels,document.Protection.Artifact)
 return params,inputs,[]uuid.UUID{proposal.TaskID,selected.TaskID},working.Canvas.Width*working.Canvas.Height,working.Canvas.Width*working.Canvas.Height*20+(4<<20),nil
}

func (c ApplyCoordinator) materializeConservativeDust(ctx context.Context,l ApplyLease,reg Registry,items []applyInput,geometry bool,personLock string,masks func(review.WorkingImage,string)(review.MaskRef,[]review.MaskRef,error))(bool,error){
 colored,err:=c.Store.Outputs(ctx,l,"image.color","working_image");if err!=nil||len(colored)!=len(items){return false,errors.New("conservative dust sources missing")}
 resolved,err:=c.Store.Outputs(ctx,l,"style.resolve","resolved_style");if err!=nil||len(resolved)!=len(items){return false,errors.New("conservative dust resolved sources missing")}
 proposals,err:=c.Store.Outputs(ctx,l,"sensor.dust","dust_candidates");if err!=nil{return false,err}
 groups,err:=compileApplyDustGroups(items);if err!=nil||len(proposals)!=len(groups){return false,errors.New("conservative dust group binding mismatch")}
 byItem:=map[uuid.UUID]applyOutput{};byResolved:=map[uuid.UUID]applyOutput{};byGroup:=map[int]applyOutput{}
 for _,p:=range colored{if _,ok:=byItem[p.ItemID];ok{return false,errors.New("duplicate conservative dust source")};byItem[p.ItemID]=p}
 for _,p:=range resolved{if _,ok:=byResolved[p.ItemID];ok{return false,errors.New("duplicate conservative dust resolved source")};byResolved[p.ItemID]=p}
 for _,p:=range proposals{if p.Ordinal<0||p.Ordinal>=len(groups){return false,errors.New("invalid conservative dust ordinal")};if _,ok:=byGroup[p.Ordinal];ok{return false,errors.New("duplicate conservative dust group")};byGroup[p.Ordinal]=p}
 itemGroup:=map[uuid.UUID]applyOutput{};for ordinal,g:=range groups{for _,item:=range g.Items{itemGroup[item.ItemID]=byGroup[ordinal]}}
 maskOp,maskRole:="mask.person","person_mask_ref";if geometry{maskOp,maskRole="mask.transform","mask_transform_result"}
 personMasks,err:=c.Store.Outputs(ctx,l,maskOp,maskRole);if err!=nil||len(personMasks)!=len(items){return false,errors.New("conservative dust person protection missing")}
 byPerson:=map[uuid.UUID]applyOutput{};for _,p:=range personMasks{if _,exists:=byPerson[p.ItemID];exists{return false,errors.New("duplicate conservative dust person binding")};byPerson[p.ItemID]=p}
 for _,item:=range items{
  if ok,e:=c.Store.Renew(ctx,l);e!=nil||!ok{return false,errors.New("conservative dust lease lost")}
  out,ok:=byItem[item.ItemID];resolvedOut,found:=byResolved[item.ItemID];personOut,personFound:=byPerson[item.ItemID];if !ok||!found||!personFound{return false,errors.New("conservative dust item binding missing")}
  working,e:=readWorking(ctx,c.Objects,c.Bucket,out.Artifact);if e!=nil{return false,e}
  raw,e:=readApplyJSON(ctx,c.Objects,c.Bucket,personOut.Artifact);if e!=nil{return false,e}
  var person review.MaskRef;if geometry{person,e=review.ParseTransformedMask(raw)}else{person,e=review.ParseMaskRef(raw)}
  if e!=nil||person.Selector!="person"||person.ModelLockID==nil||*person.ModelLockID!=personLock||person.Canvas!=working.Canvas||!bytes.Equal(person.TransformChain,working.TransformChain){return false,errors.New("conservative dust person geometry mismatch")}
  person.Protected=true
  _,protected,e:=masks(working,"");if e!=nil{return false,e};protected=append(protected,person);if len(protected)>64{return false,errors.New("conservative dust protection limit")}
  inputs:=append(refsForImage(out.Artifact.ref(),working),resolvedOut.Artifact.ref());prereq:=[]uuid.UUID{out.TaskID,resolvedOut.TaskID,personOut.TaskID};for _,mask:=range protected{inputs=append(inputs,mask.Artifact)}
  var proposal *review.ArtifactRef;if p,ok:=itemGroup[item.ItemID];ok{v:=p.Artifact.ref();proposal=&v;inputs=append(inputs,v);prereq=append(prereq,p.TaskID)}
  params,e:=canonical.Marshal(map[string]any{"policy":conservativeApplyPolicy,"image":out.Artifact.ref(),"proposal":proposal,"resolved":resolvedOut.Artifact.ref(),"protected":protected});if e!=nil{return false,e}
  key:=fmt.Sprintf("sensor.dust_auto_repair/%04d",item.Ordinal);sum:=sha256.Sum256(params);id:=nodeID(l.RunID,key)
  plan:=applyNodePlan{id,l.Phase,item.Ordinal,item.ItemID,item.AssetID,item.VersionID,key,"sensor.dust_auto_repair",inputs,prereq,hex.EncodeToString(sum[:]),params}
  profile,e:=applyConservativeProfile(working.Canvas.Width*working.Canvas.Height,working.Canvas.Width*working.Canvas.Height*(12+int64(len(protected))*4)+(4<<20));if e!=nil{return false,e}
  if e=c.materializeNode(ctx,l,reg,plan,profile);e!=nil{return false,e}
 }
 ok,err:=c.Store.Advance(ctx,l,"");if err!=nil||!ok{return false,errors.New("conservative dust advance rejected")};return true,nil
}
