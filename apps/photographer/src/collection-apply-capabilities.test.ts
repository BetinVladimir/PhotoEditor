import {expect,it,vi} from 'vitest';
import {readApplyCapabilities} from './collection-apply-capabilities';
import {OwnerAPI} from './owner-api';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`,now=Date.parse('2026-10-17T00:00:00Z');
const scope={tenant:id(1),project:id(2),collection:id(3),revision:'7',version:id(4),styleSHA:'a'.repeat(64)};
const result=()=>({schema_version:1,tenant_id:scope.tenant,project_id:scope.project,collection_id:scope.collection,source_revision:'7',style_version_id:scope.version,style_sha256:scope.styleSHA,available:true,reason:null,backends:['mps'],expires_at:new Date(now+15000).toISOString()});
it('binds advisory capability to exact tenant/project/collection/style/revision and short freshness',()=>{
 expect(readApplyCapabilities(result(),scope,now).backends).toEqual(['mps']);
 for(const change of [{tenant_id:id(9)},{project_id:id(9)},{collection_id:id(9)},{source_revision:'8'},{style_version_id:id(9)},{style_sha256:'b'.repeat(64)},{expires_at:new Date(now).toISOString()},{expires_at:new Date(now+21000).toISOString()},{available:false,reason:null},{reason:'CREATIVE_CAPABILITY_UNAVAILABLE'},{backends:['cuda','cuda']},{extra:true},{expires_at:'October 17, 2026 00:00:15 GMT'}])expect(()=>readApplyCapabilities({...result(),...change},scope,now)).toThrow();
 expect(readApplyCapabilities({...result(),available:false,reason:'PERSON_MASK_CAPABILITY_UNAVAILABLE',backends:[]},scope,now).available).toBe(false);
});
it('forwards frozen source/style query and rejects noncooperative cancellation',async()=>{
 let finish!:(value:unknown)=>void;const request=vi.fn((..._args:unknown[])=>new Promise(done=>{finish=done;})),api=new OwnerAPI({request} as never,scope.tenant),controller=new AbortController();
 const pending=api.collectionApplyCapabilities({id:scope.collection,project_id:scope.project,revision:'7'},{id:scope.version,config_hash:scope.styleSHA},controller.signal);
 expect(request.mock.calls[0]?.[0]).toContain(`source_revision=7&style_version_id=${scope.version}`);controller.abort();finish(result());await expect(pending).rejects.toMatchObject({name:'AbortError'});
 await expect(api.collectionApplyCapabilities({id:scope.collection,project_id:scope.project,revision:'7'},{id:scope.version,config_hash:scope.styleSHA},controller.signal)).rejects.toMatchObject({name:'AbortError'});expect(request).toHaveBeenCalledTimes(1);
});
