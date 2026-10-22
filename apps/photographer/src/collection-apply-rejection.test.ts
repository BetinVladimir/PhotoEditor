import {expect,it} from 'vitest';
import {APIError} from '@photoeditor/api-client';
import {collectionApplyDefinitelyRejected} from './collection-apply-rejection';
it.each(['REVISION_CONFLICT','STYLE_NOT_ACCEPTED','SNAPSHOT_STALE','STYLE_MODEL_BINDING_MISSING','PERSON_MASK_CAPABILITY_UNAVAILABLE','RESTORATION_CAPABILITY_UNAVAILABLE','CREATIVE_CAPABILITY_UNAVAILABLE','REGION_MASK_REQUIRED','REGION_MASK_GEOMETRY_UNSUPPORTED','OVERRIDE_MASK_INVALID'])('recognizes rolled-back admission %s',code=>expect(collectionApplyDefinitelyRejected(new APIError(409,code))).toBe(true));
it('keeps uncertain identities and idempotency conflicts',()=>{
 for(const error of [new Error('offline'),new APIError(409,'IDEMPOTENCY_CONFLICT'),new APIError(503,'DEPENDENCY_UNAVAILABLE'),new APIError(403,'FORBIDDEN'),new APIError(409,'UNKNOWN'),new APIError(422,'QUOTA_EXCEEDED')])expect(collectionApplyDefinitelyRejected(error)).toBe(false);
 expect(collectionApplyDefinitelyRejected(new APIError(422,'VALIDATION_FAILED'))).toBe(true);
});

it('recognizes rolled-back region mask limit while preserving an unknown limit',()=>{
 expect(collectionApplyDefinitelyRejected(new APIError(422,'REGION_MASK_LIMIT_EXCEEDED'))).toBe(true);
 expect(collectionApplyDefinitelyRejected(new APIError(422,'UNKNOWN_LIMIT'))).toBe(false);
});
