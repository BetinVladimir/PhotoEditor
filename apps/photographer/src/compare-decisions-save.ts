import {APIError} from '@photoeditor/api-client';
import type {Collection,CollectionItem} from './catalog-types';
export type CompareDecisionResult={versionId:string;status:'saved'|'conflict'|'failed'|'cancelled'|'not_attempted'};
/** Existing Apply moderation CAS; no new requests after an uncertain outcome or cancellation. */
export async function saveCompareDecisions(collection:Collection,items:readonly CollectionItem[],decisions:Readonly<Record<string,'accept'|'reject'>>,save:(collection:Collection,item:CollectionItem,decision:'accept'|'reject')=>Promise<CollectionItem>,signal:AbortSignal){
 let current=structuredClone(collection);const revision=Number(current.revision);
 if(!current.source_run_id||current.state==='archived'||!Number.isSafeInteger(revision)||revision<1||String(revision)!==current.revision)throw new Error('COMPARE_SAVE_INVALID');
 const entries=Object.entries(decisions);if(entries.length<1||entries.length>6)throw new Error('COMPARE_SAVE_INVALID');
 const jobs=entries.map(([versionId,decision])=>{
  const matches=items.filter(item=>item.asset_version_id===versionId);
  if(matches.length!==1||matches[0].collection_id!==current.id||matches[0].moderation_state!=='pending_review'||!['accept','reject'].includes(decision))throw new Error('COMPARE_SAVE_INVALID');
  return {versionId,decision,item:structuredClone(matches[0])};
 });
 const results:CompareDecisionResult[]=[],updated:CollectionItem[]=[];let stopped=false;
 for(const job of jobs){
  if(signal.aborted){results.push({versionId:job.versionId,status:'cancelled'});continue;}
  if(stopped){results.push({versionId:job.versionId,status:'not_attempted'});continue;}
  if(Number(current.revision)>=Number.MAX_SAFE_INTEGER){results.push({versionId:job.versionId,status:'failed'});stopped=true;continue;}
  try{
   const item=await save(structuredClone(current),job.item,job.decision);
   if(item.id!==job.item.id||item.collection_id!==current.id||item.asset_id!==job.item.asset_id||item.asset_version_id!==job.versionId||item.moderation_state!==(job.decision==='accept'?'accepted':'rejected'))throw new Error('COMPARE_SAVE_RESPONSE_INVALID');
   updated.push(structuredClone(item));current={...current,revision:String(Number(current.revision)+1)};results.push({versionId:job.versionId,status:'saved'});
  }catch(error){if(error instanceof APIError&&[401,403,404].includes(error.status))throw error;results.push({versionId:job.versionId,status:error instanceof APIError&&[409,412].includes(error.status)?'conflict':'failed'});stopped=true;}
 }
 return {collection:current,items:updated,results};
}
