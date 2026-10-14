import {APIError} from '@photoeditor/api-client';
import type {RunRetryAttempt} from './run-retry-attempt-store';
import type {RunCancelAttempt} from './run-cancel-attempt-store';

// A saved attempt may be removed only when the server explicitly says the
// transaction was rejected. Transport errors and generic HTTP failures can
// represent a committed mutation whose response was lost.
export function runMutationDefinitelyRejected(action:'retry'|'cancel',error:unknown):boolean{
 if(!(error instanceof APIError))return false;
 if(error.code==='VALIDATION_FAILED'&&error.status===422)return true;
 if(error.status!==409)return false;
 return action==='retry'
  ?error.code==='REVISION_CONFLICT'||error.code==='RUN_NOT_RETRYABLE'
  :error.code==='RUN_NOT_CANCELLABLE'||error.code==='RUN_STATE_INVALID';
}

type Journal<T>={read():Promise<T|null>;acknowledge(key:string):Promise<void>;discard(key:string):Promise<void>};
type SavedAction={action:'retry'|'cancel';key:string;created_at:number;body?:{expected_revision:string};journal:Journal<RunRetryAttempt>|Journal<RunCancelAttempt>};

/** Replay only persisted requests. The server ledger decides whether they committed. */
export async function recoverPendingRunMutations<T>(input:{
 retry:Journal<RunRetryAttempt>;
 cancel:Journal<RunCancelAttempt>;
 signal:AbortSignal;
 send:(action:'retry'|'cancel',key:string,body:{expected_revision:string}|undefined,signal:AbortSignal)=>Promise<T>;
 accept:(value:T)=>void;
}):Promise<number>{
 const [retry,cancel]=await Promise.all([input.retry.read(),input.cancel.read()]);
 input.signal.throwIfAborted();
 const pending:SavedAction[]=[];
 if(retry)pending.push({action:'retry',key:retry.key,created_at:retry.created_at,body:{expected_revision:retry.expected_revision},journal:input.retry});
 if(cancel)pending.push({action:'cancel',key:cancel.key,created_at:cancel.created_at,journal:input.cancel});
 pending.sort((a,b)=>a.created_at-b.created_at||a.action.localeCompare(b.action));
 let recovered=0;
 for(const action of pending){
  input.signal.throwIfAborted();
  try{
   const result=await input.send(action.action,action.key,action.body,input.signal);
   input.signal.throwIfAborted();
   await action.journal.acknowledge(action.key);
   input.signal.throwIfAborted();
   input.accept(result);
   recovered++;
  }catch(error){
   if(input.signal.aborted)throw error;
   if(!runMutationDefinitelyRejected(action.action,error))throw error;
   await action.journal.discard(action.key);
  }
 }
 return recovered;
}
