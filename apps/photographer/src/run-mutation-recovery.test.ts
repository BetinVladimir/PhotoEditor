import {describe,it,expect} from 'vitest';
import {APIError} from '@photoeditor/api-client';
import {runMutationDefinitelyRejected} from './run-mutation-recovery';

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
});
