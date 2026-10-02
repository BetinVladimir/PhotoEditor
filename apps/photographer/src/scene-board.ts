import {canonicalJSON} from '@photoeditor/api-client';
import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
export type SceneFrameBinding={ordinal:number;versionId:string;sourceSha:string};
export type SceneBoardGroup={id:string;ordinals:number[];frames:SceneFrameBinding[];reference:SceneFrameBinding;confidence:number;corrections:{artifact_id:string;sha256:string};outliers:null};
export type SceneBoard={snapshotId:string;snapshotDigest:string;policySha:string;groups:SceneBoardGroup[]};
const uuid=(value:unknown):value is string=>typeof value==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(value)&&value!=='00000000-0000-0000-0000-000000000000';
const hash=(value:unknown):value is string=>typeof value==='string'&&/^[0-9a-f]{64}$/.test(value);
function object(value:unknown,keys:string[]):Record<string,unknown>{if(!value||typeof value!=='object'||Array.isArray(value)||Object.keys(value).length!==keys.length||keys.some(key=>!Object.hasOwn(value,key)))throw new Error('SCENE_BOARD_INVALID');return value as Record<string,unknown>;}
/** Consume an already verified immutable ScenePlan and the matching frozen snapshot inventory. */
export function buildSceneBoard(analysis:unknown,binding:{snapshotId:string;snapshotDigest:string;policySha:string;frames:readonly SceneFrameBinding[]}):SceneBoard{
 const plan=object(analysis,['schema_version','snapshot_id','snapshot_digest','policy_sha','groups']);
 if(plan.schema_version!==1||!uuid(binding.snapshotId)||!hash(binding.snapshotDigest)||!hash(binding.policySha)||plan.snapshot_id!==binding.snapshotId||plan.snapshot_digest!==binding.snapshotDigest||plan.policy_sha!==binding.policySha||!Array.isArray(plan.groups)||plan.groups.length<1||plan.groups.length>5000||binding.frames.length<1||binding.frames.length>5000)throw new Error('SCENE_BOARD_INVALID');
 const frames=new Map<number,SceneFrameBinding>(),versions=new Set<string>();
 for(const frame of binding.frames){if(!Number.isInteger(frame.ordinal)||frame.ordinal<0||frame.ordinal>=binding.frames.length||frames.has(frame.ordinal)||!uuid(frame.versionId)||versions.has(frame.versionId)||!hash(frame.sourceSha))throw new Error('SCENE_BOARD_INVALID');frames.set(frame.ordinal,{...frame});versions.add(frame.versionId);}
 const observed=new Set<number>(),ids=new Set<string>();
 const groups=plan.groups.map(value=>{
  const group=object(value,['id','ordinals','reference_ordinal','confidence','corrections']);
  const ref=object(group.corrections,['artifact_id','sha256']);
  if(!uuid(group.id)||ids.has(group.id)||!Array.isArray(group.ordinals)||group.ordinals.length<1||group.ordinals.length>5000||typeof group.confidence!=='number'||!Number.isFinite(group.confidence)||group.confidence<0||group.confidence>1||!uuid(ref.artifact_id)||!hash(ref.sha256)||typeof group.reference_ordinal!=='number')throw new Error('SCENE_BOARD_INVALID');
  ids.add(group.id);let previous=-1;
  const members=group.ordinals.map(ordinal=>{if(typeof ordinal!=='number'||!Number.isInteger(ordinal)||ordinal<=previous||observed.has(ordinal)||!frames.has(ordinal))throw new Error('SCENE_BOARD_INVALID');previous=ordinal;observed.add(ordinal);return {...frames.get(ordinal)!};});
  const reference=members.find(frame=>frame.ordinal===group.reference_ordinal);if(!reference)throw new Error('SCENE_BOARD_INVALID');
  return {id:group.id,ordinals:members.map(frame=>frame.ordinal),frames:members,reference:{...reference},confidence:group.confidence,corrections:{artifact_id:ref.artifact_id,sha256:ref.sha256},outliers:null};
 });
 if(observed.size!==frames.size)throw new Error('SCENE_BOARD_INVALID');
 // Stable presentation follows frozen source order, independent of transport ordering.
 groups.sort((a,b)=>a.ordinals[0]-b.ordinals[0]);
 return {snapshotId:binding.snapshotId,snapshotDigest:binding.snapshotDigest,policySha:binding.policySha,groups};
}

/** Validate the authenticated API projection and exact canonical plan SHA before presentation. */
export function readSceneBoardDocument(value:unknown,analysisId:string,sessionId:string):SceneBoard {
 const document=object(value,['session_id','analysis_id','snapshot_id','snapshot_digest','policy_sha','plan_sha','plan','frames']);
 if(!uuid(analysisId)||!uuid(sessionId)||document.analysis_id!==analysisId||document.session_id!==sessionId||!hash(document.plan_sha)||!Array.isArray(document.frames)||document.frames.length<1||document.frames.length>5000)throw new Error('SCENE_BOARD_INVALID');
 const bytes=new TextEncoder().encode(canonicalJSON(document.plan));
 if(bytes.byteLength>8388608||bytesToHex(sha256(bytes))!==document.plan_sha)throw new Error('SCENE_BOARD_INVALID');
 const frames=document.frames.map(value=>{const frame=object(value,['ordinal','version_id','source_sha']);return {ordinal:frame.ordinal,versionId:frame.version_id,sourceSha:frame.source_sha} as SceneFrameBinding;});
 return buildSceneBoard(document.plan,{snapshotId:document.snapshot_id as string,snapshotDigest:document.snapshot_digest as string,policySha:document.policy_sha as string,frames});
}
