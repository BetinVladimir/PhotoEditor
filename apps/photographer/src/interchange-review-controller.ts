import {canonicalJSON} from '@photoeditor/api-client';
import type {InterchangeReviewAttempt,InterchangeReviewAttemptStore} from './interchange-review-attempt-store';
import {readInterchangeDecisionResult,readInterchangeCloseResult,type InterchangeInspection} from './interchange-job';
import {semanticJournalLock,type SemanticJournalLock} from './semantic-attempt-store';
const executions=new Map<string,Promise<unknown>>();
export async function executeInterchangeReview(options:{api:{decideInterchangeReview(project:string,inspection:InterchangeInspection,decisions:InterchangeReviewAttempt['decisions'],key:string,signal:AbortSignal):Promise<unknown>;closeInterchangeReview(project:string,inspection:InterchangeInspection,key:string,signal:AbortSignal):Promise<unknown>};journal:InterchangeReviewAttemptStore;tenant:string;project:string;workflow:string;record:InterchangeReviewAttempt;signal:AbortSignal;requireExisting?:boolean;onAttempt?:(record:InterchangeReviewAttempt)=>void;lock?:SemanticJournalLock}):Promise<void>{
 const {api,journal,tenant,project,workflow,signal,onAttempt,requireExisting}=options;
 const original=structuredClone(options.record),lock=options.lock??semanticJournalLock;
 journal.assertScope(tenant,project,workflow);
 const previous=executions.get(journal.key)??Promise.resolve();
 const next=previous.catch(()=>{}).then(()=>lock('interchange-review-execution:'+journal.key,async()=>{
  signal.throwIfAborted();journal.assertScope(tenant,project,workflow);
  let saved=await journal.read();signal.throwIfAborted();
  if(!saved&&(requireExisting||original.receipt!==null))throw new Error('INTERCHANGE_REVIEW_JOURNAL_CONFLICT');
  if(saved&&canonicalJSON({...saved,receipt:null})!==canonicalJSON({...original,receipt:null}))throw new Error('INTERCHANGE_REVIEW_JOURNAL_CONFLICT');
  if(!saved){await journal.write(original);signal.throwIfAborted();saved=await journal.read();}
  if(!saved)throw new Error('INTERCHANGE_REVIEW_JOURNAL_CONFLICT');
  let owned=structuredClone(saved),expected=canonicalJSON(owned);
  const fence=async()=>{signal.throwIfAborted();journal.assertScope(tenant,project,workflow);const current=await journal.read();signal.throwIfAborted();if(!current||canonicalJSON(current)!==expected)throw new Error('INTERCHANGE_REVIEW_JOURNAL_CONFLICT');};
  onAttempt?.(structuredClone(owned));await fence();
  if(owned.receipt===null){
   const inspection=structuredClone(owned.inspection);
   const result=owned.kind==='decide'?await api.decideInterchangeReview(project,inspection,structuredClone(owned.decisions),owned.key,signal):await api.closeInterchangeReview(project,inspection,owned.key,signal);
   await fence();
   // OwnerAPI returns a validated four-field close receipt; a direct
   // transport port may still return the six-field HTTP representation.
   const normalizedClose=result!==null&&typeof result==='object'&&!Array.isArray(result)&&Object.keys(result).sort().join(',')==='revision,run_id,state,workflow_id';
   const closeValue=normalizedClose?{...result as object,outputs:[],error_code:null}:result;
   const receipt=owned.kind==='decide'?readInterchangeDecisionResult(result,owned.inspection,owned.decisions.length):{...readInterchangeCloseResult(closeValue,owned.inspection),outputs:[],error_code:null};
   owned={...owned,receipt};await journal.write(owned);signal.throwIfAborted();expected=canonicalJSON(owned);
   onAttempt?.(structuredClone(owned));await fence();
  }
  await fence();await journal.clear(owned.key,owned);signal.throwIfAborted();journal.assertScope(tenant,project,workflow);
 }));
 executions.set(journal.key,next);
 try{await next;}finally{if(executions.get(journal.key)===next)executions.delete(journal.key);}
}
