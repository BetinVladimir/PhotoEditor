import {readDeadline} from './read-deadline';
import {freezeSignCreate,readSigningTicket,type SignCreate,type SigningJobTicket} from '@photoeditor/api-client';
import type {SigningAttemptStore,SigningAttempt} from './signing-attempt-store';

type SigningAPI={signExport(exportID:string,request:SignCreate,key:string,signal?:AbortSignal):Promise<SigningJobTicket>};

/** Persist before sending. A network/storage failure retains the immutable
 * attempt; the next invocation must use its saved key, never a replacement. */
export async function submitSigning(store:SigningAttemptStore,api:SigningAPI,request:SignCreate,newKey:string,signal?:AbortSignal):Promise<SigningJobTicket>{
 const body=freezeSignCreate(request);
 if(typeof newKey!=='string'||!/^[\x21-\x7e]{1,128}$/.test(newKey))throw new Error('SIGNING_REQUEST_INVALID');
 signal?.throwIfAborted();
 const previous=await readDeadline(signal,child=>store.read().then(value=>{child.throwIfAborted();return value}));
 signal?.throwIfAborted();
 if(previous!==null&&previous.body.signing_policy_id!==body.signing_policy_id)throw new Error('SIGNING_JOURNAL_CONFLICT');
 const attempt:SigningAttempt=previous??{version:1,key:newKey,body,ticket:null};
 if(attempt.ticket!==null)return readSigningTicket(attempt.ticket);
 await readDeadline(signal,child=>store.write(attempt).then(()=>child.throwIfAborted()));
 signal?.throwIfAborted();
 const ticket=readSigningTicket(await readDeadline(signal,child=>api.signExport(store.exportID,freezeSignCreate(attempt.body),attempt.key,child),15000));
 signal?.throwIfAborted();
 await readDeadline(signal,child=>store.write({...attempt,ticket}).then(()=>child.throwIfAborted()));
 signal?.throwIfAborted();
 return ticket;
}
