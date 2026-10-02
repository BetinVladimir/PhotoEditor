import {describe,it,expect,vi} from 'vitest';
import {APIError} from '@photoeditor/api-client';
import {loadCaptureShots} from './capture-shot-load';
import type {OwnerAPI} from './owner-api';
function setup(error?:Error,revision='3'){
 const captureShotList=error?vi.fn().mockRejectedValue(error):vi.fn().mockResolvedValue({session_id:'session/id',revision:'2',document:null,requirements:[]});
 const get=vi.fn().mockResolvedValue({revision});
 return {captureShotList,get,api:{captureShotList,get} as unknown as Pick<OwnerAPI,'captureShotList'|'get'>};
}
describe('capture ShotList consent recovery',()=>{
 it('keeps valid lists and never fetches a replacement revision',async()=>{const{api,get}=setup();expect((await loadCaptureShots(api,'session/id')).blocked).toBe(false);expect(get).not.toHaveBeenCalled()});
 it('returns only an empty blocked replacement with the authoritative revision',async()=>{const{api,get}=setup(new APIError(409,'CONSENT_REVOKED'));const signal=new AbortController().signal;expect(await loadCaptureShots(api,'session/id',signal)).toEqual({list:{session_id:'session/id',revision:'3',document:null,requirements:[]},blocked:true});expect(get).toHaveBeenCalledWith('/api/v1/tenants/{t}/shoot-sessions/session%2Fid',signal)});
 it('does not recover other errors or incorrect status',async()=>{for(const error of [new APIError(403,'FORBIDDEN'),new APIError(503,'DEPENDENCY_UNAVAILABLE'),new APIError(403,'CONSENT_REVOKED'),new Error('network')]){const{api,get}=setup(error);await expect(loadCaptureShots(api,'session')).rejects.toBe(error);expect(get).not.toHaveBeenCalled()}});
 it('rejects invalid CAS revisions',async()=>{for(const revision of ['0','03','-1','9007199254740992']){const{api}=setup(new APIError(409,'CONSENT_REVOKED'),revision);await expect(loadCaptureShots(api,'session')).rejects.toThrow('Invalid session revision')}});
});
