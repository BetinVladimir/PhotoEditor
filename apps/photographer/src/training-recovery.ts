import type {OwnerAPI} from './owner-api';
import {scopeUUID} from './workspace-access';
import type {TrainingSet,TrainingPair} from './catalog-types';
import type {TrainingAttemptStore,TrainingRecovery} from './training-attempt-store';
const revision=(value:unknown):value is string=>typeof value==='string'&&/^[1-9][0-9]{0,18}$/.test(value)&&BigInt(value)<=9223372036854775807n;
function validPair(pair:TrainingPair,set:string){
 return !!pair&&scopeUUID(pair.id)&&pair.training_set_id===set
  &&scopeUUID(pair.before_asset_version_id)&&scopeUUID(pair.after_asset_version_id)
  &&pair.before_asset_version_id!==pair.after_asset_version_id
  &&typeof pair.session_id==='string'&&pair.session_id===pair.session_id.trim()
  &&[...pair.session_id].length>0&&[...pair.session_id].length<=120
  &&typeof pair.confirmed==='boolean'&&typeof pair.included==='boolean'
  &&typeof pair.validation_status==='string'&&pair.validation_status.length>0
  &&(pair.ignore_mask_artifact_id===undefined||scopeUUID(pair.ignore_mask_artifact_id))
  &&revision(pair.revision)&&typeof pair.created_at==='string'&&Number.isFinite(Date.parse(pair.created_at));
}
export async function readTrainingSet(api:OwnerAPI,id:string,style:string,signal:AbortSignal){
 signal.throwIfAborted();
 if(!scopeUUID(id)||!scopeUUID(style))throw new Error('TRAINING_SCOPE_INVALID');
 let cursor:string|undefined,first:TrainingSet|undefined;const seen=new Set<string>(),cursors=new Set<string>(),items:TrainingPair[]=[];
 do{const page=await api.trainingSet(id,cursor,signal);signal.throwIfAborted();
  if(!page||page.id!==id||page.style_id!==style||!Array.isArray(page.pairs?.items)||!revision(page.revision)||page.pairs.revision!==page.revision||first&&page.revision!==first.revision)throw new Error('TRAINING_SET_INVALID');
  first??=page;for(const pair of page.pairs.items){if(!validPair(pair,id)||seen.has(pair.id))throw new Error('TRAINING_PAIR_INVALID');seen.add(pair.id);items.push(pair);}
  const next=page.pairs.next_cursor??undefined;if(next&&(typeof next!=='string'||next===cursor||cursors.has(next)||cursors.size>=50)||items.length>5000)throw new Error('TRAINING_PAGINATION_INVALID');
  if(next)cursors.add(next);cursor=next;
 }while(cursor);
 return {...first!,pairs:{...first!.pairs,items,next_cursor:null}};
}
export async function resumeTraining(api:OwnerAPI,style:string,store:TrainingAttemptStore,state:TrainingRecovery,signal:AbortSignal,current:()=>boolean){
 const check=()=>{signal.throwIfAborted();if(!current())throw new DOMException('Training scope changed','AbortError');};check();
 if(!scopeUUID(style)||!state||state.style_id!==style)throw new Error('TRAINING_SCOPE_INVALID');
 let pending=state.pending;
 if(!pending)throw new Error('TRAINING_PENDING_MISSING');
 const stored=await store.read();check();
 if(stored.style_id!==style)throw new Error('TRAINING_SCOPE_INVALID');
 if(!stored.pending||stored.pending.key!==pending.key||JSON.stringify(stored.pending.command)!==JSON.stringify(pending.command))throw new Error('TRAINING_JOURNAL_CONFLICT');
 state=stored;pending=stored.pending;
 if(!pending.ticket){
  const cmd=pending.command,path=cmd.kind==='create'?`/api/v1/tenants/{t}/styles/${encodeURIComponent(style)}/training-sets`:cmd.kind==='pair'?`/api/v1/tenants/{t}/training-sets/${encodeURIComponent(cmd.set_id)}/pairs`:`/api/v1/tenants/{t}/styles/${encodeURIComponent(style)}/analyses`;
  const result=await api.mutate<{id?:string;run_id?:string}>(path,structuredClone(cmd.body),pending.key,'POST',undefined,signal);
  const ticket=cmd.kind==='analyze'?result?.run_id:result?.id;if(!scopeUUID(ticket))throw new Error('TRAINING_RECEIPT_INVALID');
  // Acceptance belongs to the exact durable command even when its UI flight
  // ended while the server replied. Persist before permitting UI publication.
  state=await store.receipt(pending.key,ticket,cmd);check();pending=state.pending!;
 }
 if(pending.command.kind==='analyze'){const result=await store.finish(pending.key);check();return{state:result,set:undefined};}
 const set=await readTrainingSet(api,pending.command.kind==='create'?pending.ticket!:pending.command.set_id,style,signal);check();
 const pairCommand=pending.command;
 if(pairCommand.kind==='pair'&&!set.pairs.items.some(pair=>pair.id===pending!.ticket&&pair.before_asset_version_id===pairCommand.body.before_asset_version_id&&pair.after_asset_version_id===pairCommand.body.after_asset_version_id&&pair.session_id===pairCommand.body.session_id&&pair.confirmed===true&&pair.included===true))throw new Error('TRAINING_PAIR_RECEIPT_MISSING');
 state=await store.finish(pending.key);check();return{state,set};
}
