import type {ArtifactRef,V2VersionBinding} from './catalog-types';
export type RelightCandidateKind='depth'|'person'|'manual_target'|'manual_guard';
export type RelightCandidateQuery={snapshot_id:string;base:V2VersionBinding;kind:RelightCandidateKind;after:string|null};
export type RelightCandidate={ref:ArtifactRef;kind:RelightCandidateKind};
export type RelightCandidatePage={items:RelightCandidate[];next_cursor:string|null};
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const sha=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{64}$/.test(v);
const kinds=['depth','person','manual_target','manual_guard'];
function fields(v:unknown,keys:string[]):v is Record<string,unknown>{return !!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).length===keys.length&&keys.every(key=>Object.prototype.hasOwnProperty.call(v,key));}
function ref(v:unknown):v is ArtifactRef{return fields(v,['artifact_id','sha256'])&&uuid(v.artifact_id)&&sha(v.sha256);}
export function freezeRelightCandidateQuery(v:RelightCandidateQuery):RelightCandidateQuery{
 if(!fields(v,['snapshot_id','base','kind','after'])||!uuid(v.snapshot_id)||!kinds.includes(v.kind)||(v.after!==null&&!uuid(v.after))||!fields(v.base,['asset_version_id','source','canvas_hash'])||!uuid(v.base.asset_version_id)||!ref(v.base.source)||!sha(v.base.canvas_hash))throw new Error('RELIGHT_CANDIDATE_QUERY_INVALID');
 return structuredClone(v);
}
export function readRelightCandidatePage(v:unknown,query:RelightCandidateQuery):RelightCandidatePage{
 if(!fields(v,['items','next_cursor'])||!Array.isArray(v.items)||v.items.length>100||(v.next_cursor!==null&&!uuid(v.next_cursor)))throw new Error('RELIGHT_CANDIDATE_PAGE_INVALID');
 let previous=query.after??'';
 for(const item of v.items){if(!fields(item,['ref','kind'])||!ref(item.ref)||item.kind!==query.kind||item.ref.artifact_id<=previous)throw new Error('RELIGHT_CANDIDATE_PAGE_INVALID');previous=item.ref.artifact_id;}
 if(v.next_cursor!==null&&(v.next_cursor<previous||v.next_cursor===(query.after??'')))throw new Error('RELIGHT_CANDIDATE_PAGE_INVALID');
 return structuredClone(v) as RelightCandidatePage;
}
/** Empty filtered pages may advance. Never silently truncate the candidate set. */
export async function collectRelightCandidates(fetchPage:(query:RelightCandidateQuery,signal?:AbortSignal)=>Promise<RelightCandidatePage>,query:RelightCandidateQuery,signal?:AbortSignal,maxPages=256):Promise<RelightCandidate[]>{
 let current=freezeRelightCandidateQuery(query);const result:RelightCandidate[]=[];
 if(!Number.isInteger(maxPages)||maxPages<1||maxPages>1024)throw new Error('RELIGHT_CANDIDATE_BUDGET_INVALID');
 for(let pageNumber=0;pageNumber<maxPages;pageNumber++){
  if(signal?.aborted)throw new Error('RELIGHT_CANDIDATE_CANCELLED');
  const page=readRelightCandidatePage(await fetchPage(current,signal),current);
  if(signal?.aborted)throw new Error('RELIGHT_CANDIDATE_CANCELLED');
  result.push(...page.items);if(page.next_cursor===null)return result;
  current={...current,after:page.next_cursor};
 }
 throw new Error('RELIGHT_CANDIDATE_PAGE_LIMIT');
}
