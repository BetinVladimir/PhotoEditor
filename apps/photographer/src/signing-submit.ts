import {freezeSignCreate,readSigningTicket,type SignCreate,type SigningJobTicket} from '@photoeditor/api-client';
import type {SigningAttemptStore,SigningAttempt} from './signing-attempt-store';

type SigningAPI={signExport(exportID:string,request:SignCreate,key:string,signal?:AbortSignal):Promise<SigningJobTicket>};

/** Persist before sending. A network/storage failure retains the immutable
 * attempt; the next invocation must use its saved key, never a replacement. */
export async function submitSigning(store:SigningAttemptStore,api:SigningAPI,request:SignCreate,newKey:string,signal?:AbortSignal):Promise<SigningJobTicket>{
 const body=freezeSignCreate(request);
 if(typeof newKey!=='string'||!/^[\x21-\x7e]{1,128}$/.test(newKey))throw new Error('SIGNING_REQUEST_INVALID');
 signal?.throwIfAborted();
 const previous=await store.read();
 signal?.throwIfAborted();
 if(previous!==null&&previous.body.signing_policy_id!==body.signing_policy_id)throw new Error('SIGNING_JOURNAL_CONFLICT');
 const attempt:SigningAttempt=previous??{version:1,key:newKey,body,ticket:null};
 if(attempt.ticket!==null)return readSigningTicket(attempt.ticket);
 await store.write(attempt);
 signal?.throwIfAborted();
 const ticket=readSigningTicket(await api.signExport(store.exportID,freezeSignCreate(attempt.body),attempt.key,signal));
 signal?.throwIfAborted();
 await store.write({...attempt,ticket});
 signal?.throwIfAborted();
 return ticket;
}
