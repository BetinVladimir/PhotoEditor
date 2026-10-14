import {describe,it,expect} from 'vitest';
import {APIError} from '@photoeditor/api-client';
import {recoverPendingRunMutations,runMutationDefinitelyRejected} from './run-mutation-recovery';
import type {RunRetryAttempt} from './run-retry-attempt-store';
import type {RunCancelAttempt} from './run-cancel-attempt-store';

function journal<T extends {key:string}>(record:T|null){let saved=record;return {read:async()=>saved,acknowledge:async(key:string)=>{if(saved?.key!==key)throw new Error('wrong key');saved=null;},discard:async(key:string)=>{if(saved?.key!==key)throw new Error('wrong key');saved=null;}};}

describe('run mutation recovery',()=>{
 it('discards only explicit noncommitted retry responses',()=>{
  expect(runMutationDefinitelyRejected('retry',new APIError(409,'REVISION_CONFLICT'))).toBe(true);
  expect(runMutationDefinitelyRejected('retry',new APIError(409,'RUN_NOT_RETRYABLE'))).toBe(true);
  expect(runMutationDefinitelyRejected('retry',new APIError(422,'VALIDATION_FAILED'))).toBe(true);
  for(const error of [new Error('connection lost'),new APIError(409,'HTTP_409'),new APIError(409,'IDEMPOTENCY_CONFLICT'),new APIError(403,'FORBIDDEN'),new APIError(500,'INTERNAL')]){
   expect(runMutationDefinitelyRejected('retry',error)).toBe(false);
  }
 });
 it('does not reuse a definitively rejected cancel attempt',()=>{
  expect(runMutationDefinitelyRejected('cancel',new APIError(409,'RUN_NOT_CANCELLABLE'))).toBe(true);
  expect(runMutationDefinitelyRejected('cancel',new APIError(409,'RUN_STATE_INVALID'))).toBe(true);
  expect(runMutationDefinitelyRejected('cancel',new APIError(409,'REVISION_CONFLICT'))).toBe(false);
 });
 it('replays persisted actions in creation order with their original key and body',async()=>{
  const retry=journal<RunRetryAttempt>({version:1,key:'retry-key',expected_revision:'9007199254740993',created_at:2});
  const cancel=journal<RunCancelAttempt>({version:1,key:'cancel-key',created_at:1});
  const sent:Array<[string,string,unknown]>=[],accepted:string[]=[];
  const count=await recoverPendingRunMutations({retry,cancel,signal:new AbortController().signal,
   send:async(action,key,body)=>{sent.push([action,key,body]);return action;},accept:value=>accepted.push(value)});
  expect(count).toBe(2);
  expect(sent).toEqual([['cancel','cancel-key',undefined],['retry','retry-key',{expected_revision:'9007199254740993'}]]);
  expect(accepted).toEqual(['cancel','retry']);
  expect(await retry.read()).toBeNull();expect(await cancel.read()).toBeNull();
 });
 it('keeps an ambiguous action and discards a confirmed rejection',async()=>{
  const retry=journal<RunRetryAttempt>({version:1,key:'retry-key',expected_revision:'1',created_at:1});
  const cancel=journal<RunCancelAttempt>(null),signal=new AbortController().signal;
  await expect(recoverPendingRunMutations({retry,cancel,signal,
   send:async()=>{throw new Error('response lost');},accept:()=>{}})).rejects.toThrow('response lost');
  expect((await retry.read())?.key).toBe('retry-key');
  expect(await recoverPendingRunMutations({retry,cancel,signal,
   send:async()=>{throw new APIError(409,'REVISION_CONFLICT');},accept:()=>{}})).toBe(0);
  expect(await retry.read()).toBeNull();
 });
});
