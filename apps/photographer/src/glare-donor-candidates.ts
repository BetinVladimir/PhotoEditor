import type {ArtifactRef,V2VersionBinding} from './catalog-types';

export type GlareDonorCandidateQuery={shoot_session_id:string;snapshot_id:string;base:V2VersionBinding;source_observation_id:string;after:string|null};
export type GlareSourceCandidateQuery={shoot_session_id:string;snapshot_id:string;base:V2VersionBinding;after:string|null};
export type GlareSourceCandidate={observation_id:string;source_faces:ArtifactRef};
export type GlareSourceCandidatePage={items:GlareSourceCandidate[];next_cursor:string|null};
export type GlareMaskPairQuery={snapshot_id:string;base:V2VersionBinding;after:string|null};
export type GlareMaskPair={glasses_mask:ArtifactRef;protected:ArtifactRef;target_pixel:ArtifactRef;protected_pixel:ArtifactRef};
export type GlareMaskPairPage={items:GlareMaskPair[];next_cursor:string|null};
export type GlareDonorCandidate={observation_id:string;asset_id:string;asset_version_id:string;source:ArtifactRef;donor_faces:ArtifactRef};
export type GlareDonorCandidatePage={source_faces:ArtifactRef;items:GlareDonorCandidate[];next_cursor:string|null};
export function verifyGlareDonorCandidateBinding(candidate:GlareDonorCandidate,binding:V2VersionBinding):V2VersionBinding{
 if(!binding||binding.asset_version_id!==candidate.asset_version_id||binding.source.artifact_id!==candidate.source.artifact_id||binding.source.sha256!==candidate.source.sha256||!sha(binding.canvas_hash))throw new Error('GLARE_DONOR_VERSION_CHANGED');
 return structuredClone(binding);
}
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const sha=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{64}$/.test(v);
const fields=(v:unknown,keys:string[]):v is Record<string,unknown>=>!!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).length===keys.length&&keys.every(k=>Object.prototype.hasOwnProperty.call(v,k));
const ref=(v:unknown):v is ArtifactRef=>fields(v,['artifact_id','sha256'])&&uuid(v.artifact_id)&&sha(v.sha256);
const binding=(v:unknown):v is V2VersionBinding=>fields(v,['asset_version_id','source','canvas_hash'])&&uuid(v.asset_version_id)&&ref(v.source)&&sha(v.canvas_hash);
export function freezeGlareDonorCandidateQuery(v:GlareDonorCandidateQuery):GlareDonorCandidateQuery{
 if(!fields(v,['shoot_session_id','snapshot_id','base','source_observation_id','after'])||!uuid(v.shoot_session_id)||!uuid(v.snapshot_id)||!binding(v.base)||!uuid(v.source_observation_id)||v.after!==null&&!uuid(v.after))throw new Error('GLARE_DONOR_CANDIDATE_QUERY_INVALID');
 return structuredClone(v);
}
export function freezeGlareSourceCandidateQuery(v:GlareSourceCandidateQuery):GlareSourceCandidateQuery{
 if(!fields(v,['shoot_session_id','snapshot_id','base','after'])||!uuid(v.shoot_session_id)||!uuid(v.snapshot_id)||!binding(v.base)||v.after!==null&&!uuid(v.after))throw new Error('GLARE_SOURCE_CANDIDATE_QUERY_INVALID');
 return structuredClone(v);
}
export function readGlareSourceCandidatePage(v:unknown,query:GlareSourceCandidateQuery):GlareSourceCandidatePage{
 if(!fields(v,['items','next_cursor'])||!Array.isArray(v.items)||v.items.length>100||v.next_cursor!==null&&!uuid(v.next_cursor))throw new Error('GLARE_SOURCE_CANDIDATE_PAGE_INVALID');
 let previous=query.after??'';
 for(const item of v.items){if(!fields(item,['observation_id','source_faces'])||!uuid(item.observation_id)||!ref(item.source_faces)||item.observation_id<=previous)throw new Error('GLARE_SOURCE_CANDIDATE_PAGE_INVALID');previous=item.observation_id;}
 if(v.next_cursor!==null&&(v.items.length!==100||v.next_cursor!==previous))throw new Error('GLARE_SOURCE_CANDIDATE_PAGE_INVALID');
 return structuredClone(v) as GlareSourceCandidatePage;
}
export async function collectGlareSourceCandidates(fetch:(query:GlareSourceCandidateQuery,signal?:AbortSignal)=>Promise<GlareSourceCandidatePage>,query:GlareSourceCandidateQuery,signal?:AbortSignal){
 let current=freezeGlareSourceCandidateQuery(query);const items:GlareSourceCandidate[]=[];
 for(let i=0;i<256;i++){
  if(signal?.aborted)throw new Error('GLARE_SOURCE_CANDIDATES_CANCELLED');
  const page=readGlareSourceCandidatePage(await fetch(current,signal),current);
  if(signal?.aborted)throw new Error('GLARE_SOURCE_CANDIDATES_CANCELLED');
  items.push(...page.items);if(page.next_cursor===null)return items;
  current={...current,after:page.next_cursor};
 }
 throw new Error('GLARE_SOURCE_CANDIDATE_PAGE_LIMIT');
}
export function freezeGlareMaskPairQuery(v:GlareMaskPairQuery):GlareMaskPairQuery{
 if(!fields(v,['snapshot_id','base','after'])||!uuid(v.snapshot_id)||!binding(v.base)||v.after!==null&&!uuid(v.after))throw new Error('GLARE_MASK_PAIR_QUERY_INVALID');
 return structuredClone(v);
}
export function readGlareMaskPairPage(v:unknown,query:GlareMaskPairQuery):GlareMaskPairPage{
 if(!fields(v,['items','next_cursor'])||!Array.isArray(v.items)||v.items.length>100||v.next_cursor!==null&&!uuid(v.next_cursor))throw new Error('GLARE_MASK_PAIR_PAGE_INVALID');
 let previous=query.after??'';
 for(const item of v.items){
  if(!fields(item,['glasses_mask','protected','target_pixel','protected_pixel'])||![item.glasses_mask,item.protected,item.target_pixel,item.protected_pixel].every(ref))throw new Error('GLARE_MASK_PAIR_PAGE_INVALID');
  const ids=[item.glasses_mask.artifact_id,item.protected.artifact_id,item.target_pixel.artifact_id,item.protected_pixel.artifact_id];
  if(new Set(ids).size!==ids.length||item.glasses_mask.artifact_id<=previous)throw new Error('GLARE_MASK_PAIR_PAGE_INVALID');
  previous=item.glasses_mask.artifact_id;
 }
 if(v.next_cursor!==null&&(v.items.length!==100||v.next_cursor!==previous))throw new Error('GLARE_MASK_PAIR_PAGE_INVALID');
 return structuredClone(v) as GlareMaskPairPage;
}
export async function collectGlareMaskPairs(fetch:(query:GlareMaskPairQuery,signal?:AbortSignal)=>Promise<GlareMaskPairPage>,query:GlareMaskPairQuery,signal?:AbortSignal){
 let current=freezeGlareMaskPairQuery(query);const items:GlareMaskPair[]=[];
 for(let i=0;i<256;i++){
  if(signal?.aborted)throw new Error('GLARE_MASK_PAIRS_CANCELLED');
  const page=readGlareMaskPairPage(await fetch(current,signal),current);
  if(signal?.aborted)throw new Error('GLARE_MASK_PAIRS_CANCELLED');
  items.push(...page.items);if(page.next_cursor===null)return items;
  current={...current,after:page.next_cursor};
 }
 throw new Error('GLARE_MASK_PAIR_PAGE_LIMIT');
}
export function readGlareDonorCandidatePage(v:unknown,query:GlareDonorCandidateQuery):GlareDonorCandidatePage{
 if(!fields(v,['source_faces','items','next_cursor'])||!ref(v.source_faces)||!Array.isArray(v.items)||v.items.length>100||v.next_cursor!==null&&!uuid(v.next_cursor))throw new Error('GLARE_DONOR_CANDIDATE_PAGE_INVALID');
 let previous=query.after??'';
 for(const item of v.items){
  if(!fields(item,['observation_id','asset_id','asset_version_id','source','donor_faces'])||!uuid(item.observation_id)||!uuid(item.asset_id)||!uuid(item.asset_version_id)||item.asset_version_id===query.base.asset_version_id||!ref(item.source)||!ref(item.donor_faces)||item.observation_id<=previous)throw new Error('GLARE_DONOR_CANDIDATE_PAGE_INVALID');
  previous=item.observation_id;
 }
 if(v.next_cursor!==null&&(v.items.length!==100||v.next_cursor!==previous))throw new Error('GLARE_DONOR_CANDIDATE_PAGE_INVALID');
 return structuredClone(v) as GlareDonorCandidatePage;
}
export async function collectGlareDonorCandidates(fetch:(query:GlareDonorCandidateQuery,signal?:AbortSignal)=>Promise<GlareDonorCandidatePage>,query:GlareDonorCandidateQuery,signal?:AbortSignal){
 let current=freezeGlareDonorCandidateQuery(query),sourceFaces:ArtifactRef|null=null;const items:GlareDonorCandidate[]=[];
 for(let i=0;i<256;i++){
  if(signal?.aborted)throw new Error('GLARE_DONOR_CANDIDATES_CANCELLED');
  const page=readGlareDonorCandidatePage(await fetch(current,signal),current);
  if(signal?.aborted)throw new Error('GLARE_DONOR_CANDIDATES_CANCELLED');
  if(sourceFaces&&(sourceFaces.artifact_id!==page.source_faces.artifact_id||sourceFaces.sha256!==page.source_faces.sha256))throw new Error('GLARE_DONOR_CANDIDATES_CHANGED');
  sourceFaces=page.source_faces;items.push(...page.items);
  if(page.next_cursor===null)return {source_faces:sourceFaces,items};
  current={...current,after:page.next_cursor};
 }
 throw new Error('GLARE_DONOR_CANDIDATE_PAGE_LIMIT');
}
