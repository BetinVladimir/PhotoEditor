export type PreparedReviewRef={artifact_id:string;sha256:string};
export type PreparedReviewCandidate={id:string;decision:'pending';bbox?:{x:number;y:number;width:number;height:number};sensor_x?:number;sensor_y?:number;radius_sensor_px?:number;observed_frames?:number;eligible_frames?:number;score?:number;recurrence?:number};
export type PreparedReviewItem={entry_id:string;target_version_id:string;kind:'defects'|'dust';proposal:PreparedReviewRef;base:PreparedReviewRef;document:Record<string,unknown>&{candidates:PreparedReviewCandidate[]};decision_status?:'granted'|'revoked';decision_epoch:number};
export type PreparedReviewDecision={action:'grant'|'revoke';kind:'defects'|'dust';entry_id:string;target_version_id:string;proposal:PreparedReviewRef;base:PreparedReviewRef;label_map?:PreparedReviewRef;expected_epoch:number;decision?:'accept'|'reject'|'skip';accepted_candidate_ids?:string[];rejected_candidate_ids?:string[]};
export type PreparedReviewReceipt={workflow_id:string;entry_id:string;kind:'defects'|'dust';target_version_id:string;proposal_id:string;status:'granted'|'revoked';epoch:number};
export type SavedPreparedReview={entry_id:string;target_version_id:string;kind:'defects'|'dust';proposal:PreparedReviewRef;base:PreparedReviewRef;label_map:PreparedReviewRef|null;decision:'accept'|'reject'|'skip';accepted_candidate_ids:string[];rejected_candidate_ids:string[];status:'granted'|'revoked';epoch:number};
export type DustCrop={x:number;y:number;width:number;height:number};
export type DustCropMarker={left:number;top:number;width:number;height:number};

const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const sha=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{64}$/.test(v);
const record=(v:unknown):v is Record<string,unknown>=>!!v&&typeof v==='object'&&!Array.isArray(v);
const safeEpoch=(v:unknown):v is number=>typeof v==='number'&&Number.isSafeInteger(v)&&v>=0&&v<9007199254740991;
const safeInt=(v:unknown):v is number=>typeof v==='number'&&Number.isSafeInteger(v);
const validRef=(v:unknown):v is PreparedReviewRef=>record(v)&&Object.keys(v).length===2&&uuid(v.artifact_id)&&sha(v.sha256);
const invalid=()=>new Error('INTERCHANGE_PREPARED_REVIEW_INVALID');

// This projects sensor coordinates only onto the RAW crop schematic. It does
// not claim alignment with a processed photo or its orientation/transform.
export function projectDustCandidateToCrop(crop:DustCrop,candidate:PreparedReviewCandidate):DustCropMarker{
 const x=candidate.sensor_x,y=candidate.sensor_y,r=candidate.radius_sensor_px;
 if(!safeInt(crop.x)||!safeInt(crop.y)||!safeInt(crop.width)||!safeInt(crop.height)||crop.x<0||crop.y<0||crop.width<8||crop.height<8||
  typeof x!=='number'||!Number.isFinite(x)||typeof y!=='number'||!Number.isFinite(y)||typeof r!=='number'||!Number.isFinite(r)||r<=0||r>32||
  x<crop.x||x>=crop.x+crop.width||y<crop.y||y>=crop.y+crop.height)throw invalid();
 const left=Math.max(0,x-r-crop.x),top=Math.max(0,y-r-crop.y);
 const right=Math.min(crop.width,x+r-crop.x),bottom=Math.min(crop.height,y+r-crop.y);
 return {left:100*left/crop.width,top:100*top/crop.height,width:100*(right-left)/crop.width,height:100*(bottom-top)/crop.height};
}

export function readPreparedReviews(value:unknown,workflow:string):PreparedReviewItem[]{
 if(!uuid(workflow)||!record(value)||value.workflow_id!==workflow||!Array.isArray(value.items)||value.items.length>50)throw invalid();
 const items:PreparedReviewItem[]=[],seen=new Set<string>();
 for(const raw of value.items){
  if(!record(raw)||!uuid(raw.entry_id)||!uuid(raw.target_version_id)||(raw.kind!=='defects'&&raw.kind!=='dust')||!validRef(raw.proposal)||!validRef(raw.base)||!record(raw.document)||!Array.isArray(raw.document.candidates)||raw.document.candidates.length>4096||!safeEpoch(raw.decision_epoch)||
   (raw.decision_status!==undefined&&raw.decision_status!=='granted'&&raw.decision_status!=='revoked')||((raw.decision_status===undefined)!==(raw.decision_epoch===0)))throw invalid();
  const key=JSON.stringify([raw.entry_id,raw.target_version_id,raw.kind,raw.proposal.artifact_id]);
  if(seen.has(key))throw invalid();seen.add(key);
  if(raw.kind==='defects'){
   if(raw.document.version!=='defect_candidates_v1'||!validRef(raw.document.image)||!validRef(raw.document.label_map)||
    raw.document.image.artifact_id!==raw.base.artifact_id||raw.document.image.sha256!==raw.base.sha256||!record(raw.document.canvas)||
    !Number.isInteger(raw.document.canvas.width)||!Number.isInteger(raw.document.canvas.height)||
    (raw.document.canvas.width as number)<8||(raw.document.canvas.height as number)<8)throw invalid();
  }else{
   const d=raw.document,crop=d.raw_crop;
   if(d.algorithm!=='sensor_dust_log_v1'||typeof d.camera_id!=='string'||!/^raw-[0-9a-f]{64}$/.test(d.camera_id)||
    !safeInt(d.sensor_width)||!safeInt(d.sensor_height)||d.sensor_width<8||d.sensor_height<8||!record(crop)||
    !safeInt(crop.x)||!safeInt(crop.y)||!safeInt(crop.width)||!safeInt(crop.height)||crop.x<0||crop.y<0||crop.width<8||crop.height<8||
    crop.x+crop.width>d.sensor_width||crop.y+crop.height>d.sensor_height||
    !Array.isArray(d.frames)||d.frames.length<5||d.frames.length>64||!d.frames.every(validRef)||
    new Set(d.frames.map(frame=>(frame as PreparedReviewRef).artifact_id)).size!==d.frames.length||
    !d.frames.some(frame=>validRef(frame)&&frame.artifact_id===raw.base.artifact_id&&frame.sha256===raw.base.sha256)||
    !Array.isArray(d.warnings)||d.warnings.length>1||!d.warnings.every(w=>typeof w==='string')||
    (d.candidates.length===0)!==(d.warnings.length===1&&d.warnings[0]==='no_repeated_dust'))throw invalid();
  }
  const candidates:PreparedReviewCandidate[]=[],candidateIDs=new Set<string>();
  for(const candidate of raw.document.candidates){
   if(!record(candidate)||!sha(candidate.id)||candidate.decision!=='pending'||candidateIDs.has(candidate.id))throw invalid();
   candidateIDs.add(candidate.id);
   if(raw.kind==='defects'){
    if(!record(candidate.bbox)||!Number.isInteger(candidate.bbox.x)||!Number.isInteger(candidate.bbox.y)||!Number.isInteger(candidate.bbox.width)||!Number.isInteger(candidate.bbox.height)||
     (candidate.bbox.width as number)<=0||(candidate.bbox.height as number)<=0||
     (candidate.bbox.x as number)<0||(candidate.bbox.y as number)<0||
     (candidate.bbox.x as number)+(candidate.bbox.width as number)>(raw.document.canvas as Record<string,number>).width||
     (candidate.bbox.y as number)+(candidate.bbox.height as number)>(raw.document.canvas as Record<string,number>).height||
     typeof candidate.score!=='number'||!Number.isFinite(candidate.score))throw invalid();
   }else if(typeof candidate.sensor_x!=='number'||!Number.isFinite(candidate.sensor_x)||typeof candidate.sensor_y!=='number'||!Number.isFinite(candidate.sensor_y)||typeof candidate.radius_sensor_px!=='number'||!Number.isFinite(candidate.radius_sensor_px)||candidate.radius_sensor_px<=0||candidate.radius_sensor_px>32||
    !safeInt(candidate.observed_frames)||!safeInt(candidate.eligible_frames)||candidate.eligible_frames!==raw.document.frames.length||candidate.observed_frames<1||candidate.observed_frames>candidate.eligible_frames||
    typeof candidate.recurrence!=='number'||!Number.isFinite(candidate.recurrence)||candidate.recurrence<0.8||candidate.recurrence>1||Math.abs(candidate.recurrence-candidate.observed_frames/candidate.eligible_frames)>1e-12||
    candidate.sensor_x<(raw.document.raw_crop as Record<string,number>).x||candidate.sensor_x>=(raw.document.raw_crop as Record<string,number>).x+(raw.document.raw_crop as Record<string,number>).width||
    candidate.sensor_y<(raw.document.raw_crop as Record<string,number>).y||candidate.sensor_y>=(raw.document.raw_crop as Record<string,number>).y+(raw.document.raw_crop as Record<string,number>).height)throw invalid();
   candidates.push(candidate as unknown as PreparedReviewCandidate);
  }
  items.push({...raw,document:{...raw.document,candidates}} as PreparedReviewItem);
 }
 return items;
}

export function freezePreparedReviewDecision(item:PreparedReviewItem,action:'grant'|'revoke',decision:'accept'|'reject'|'skip'='accept',accepted:readonly string[]=[],rejected:readonly string[]=[]):PreparedReviewDecision{
 const label=item.kind==='defects'?item.document.label_map:undefined;
 if(item.kind==='defects'&&!validRef(label)||item.kind==='dust'&&label!==undefined||!safeEpoch(item.decision_epoch))throw invalid();
 if(action==='revoke'&&item.decision_status!=='granted')throw invalid();
 const base={action,kind:item.kind,entry_id:item.entry_id,target_version_id:item.target_version_id,proposal:{...item.proposal},base:{...item.base},...(label?{label_map:{...(label as PreparedReviewRef)}}:{}),expected_epoch:item.decision_epoch};
 if(action==='revoke')return base;
 const ids=new Set(item.document.candidates.map(candidate=>candidate.id));
 const a=[...accepted].sort(),r=[...rejected].sort();
 if(a.length+r.length>4096||decision!=='accept'&&(a.length>0||r.length>0)||a.some((id,i)=>!ids.has(id)||i>0&&id===a[i-1])||r.some((id,i)=>!ids.has(id)||i>0&&id===r[i-1]||a.includes(id)))throw invalid();
 return {...base,decision,accepted_candidate_ids:a,rejected_candidate_ids:r};
}

export function readPreparedReviewReceipt(value:unknown,workflow:string,request:PreparedReviewDecision):PreparedReviewReceipt{
 if(!record(value)||Object.keys(value).length!==7||value.workflow_id!==workflow||value.entry_id!==request.entry_id||value.kind!==request.kind||value.target_version_id!==request.target_version_id||
  value.proposal_id!==request.proposal.artifact_id||value.status!==(request.action==='grant'?'granted':'revoked')||!safeEpoch(value.epoch)||value.epoch<1)throw invalid();
 return value as unknown as PreparedReviewReceipt;
}

export function readSavedPreparedReviews(value:unknown,workflow:string):SavedPreparedReview[]{
 if(!uuid(workflow)||!record(value)||value.workflow_id!==workflow||!Array.isArray(value.items)||value.items.length>500)throw invalid();
 const rows:SavedPreparedReview[]=[],seen=new Set<string>();
 for(const raw of value.items){
  if(!record(raw)||!uuid(raw.entry_id)||!uuid(raw.target_version_id)||(raw.kind!=='defects'&&raw.kind!=='dust')||!validRef(raw.proposal)||!validRef(raw.base)||
   (raw.kind==='defects'?!validRef(raw.label_map):raw.label_map!==null)||!['accept','reject','skip'].includes(raw.decision as string)||
   !Array.isArray(raw.accepted_candidate_ids)||!Array.isArray(raw.rejected_candidate_ids)||raw.accepted_candidate_ids.length+raw.rejected_candidate_ids.length>4096||
   ![...raw.accepted_candidate_ids,...raw.rejected_candidate_ids].every(sha)||
   (raw.status!=='granted'&&raw.status!=='revoked')||!safeEpoch(raw.epoch)||raw.epoch<1)throw invalid();
  const key=JSON.stringify([raw.entry_id,raw.target_version_id,raw.kind,raw.proposal.artifact_id]);
  if(seen.has(key))throw invalid();seen.add(key);rows.push(raw as unknown as SavedPreparedReview);
 }
 return rows;
}

export function freezeSavedPreparedRevoke(saved:SavedPreparedReview):PreparedReviewDecision{
 if(saved.status!=='granted'||!safeEpoch(saved.epoch)||saved.epoch<1||!validRef(saved.proposal)||!validRef(saved.base)||
  (saved.kind==='defects'?!validRef(saved.label_map):saved.label_map!==null))throw invalid();
 return {action:'revoke',kind:saved.kind,entry_id:saved.entry_id,target_version_id:saved.target_version_id,
  proposal:{...saved.proposal},base:{...saved.base},...(saved.label_map?{label_map:{...saved.label_map}}:{}),expected_epoch:saved.epoch};
}
