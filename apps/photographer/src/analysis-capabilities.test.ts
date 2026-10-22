import {expect,it,vi} from 'vitest';
import {readAnalysisCapabilities} from './analysis-capabilities';
import {OwnerAPI} from './owner-api';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`,scope={tenant:id(1),style:id(2),geometry:true},now=Date.now();
const result=()=>({schema_version:1,tenant_id:scope.tenant,project_id:id(3),style_id:scope.style,geometry_enabled:true,available:true,reason:null,backends:['mps'],expires_at:new Date(now+15000).toISOString()});
it('accepts actual Metal combined availability but rejects wrong authority, stale data and invented fallback',()=>{
 expect(readAnalysisCapabilities(result(),scope,now).backends).toEqual(['mps']);
 for(const patch of [{tenant_id:id(4)},{style_id:id(4)},{project_id:'bad'},{geometry_enabled:false},{expires_at:new Date(now).toISOString()},{backends:['mps','cpu']},{available:false,reason:null},{reason:'GPU_DOWN'},{extra:1}])expect(()=>readAnalysisCapabilities({...result(),...patch},scope,now)).toThrow();
 expect(readAnalysisCapabilities({...result(),available:false,reason:'STYLE_AGGREGATE_CAPABILITY_UNAVAILABLE',backends:[]},scope,now).available).toBe(false);
});
it('cancels before transport and refuses a noncooperative late descriptor',async()=>{
 let finish!:(v:unknown)=>void;const request=vi.fn((..._args:unknown[])=>new Promise(done=>{finish=done})),api=new OwnerAPI({request} as never,scope.tenant),controller=new AbortController();
 const pending=api.analysisCapabilities(scope.style,true,controller.signal);expect(request.mock.calls[0][0]).toContain('geometry_enabled=true');controller.abort();finish(result());await expect(pending).rejects.toMatchObject({name:'AbortError'});await expect(api.analysisCapabilities(scope.style,true,controller.signal)).rejects.toMatchObject({name:'AbortError'});expect(request).toHaveBeenCalledOnce();
});

it('requires a wire timestamp instead of a parseable non-RFC3339 date',()=>{
 const expires=new Date(now+15000).toUTCString();
 expect(()=>readAnalysisCapabilities({...result(),expires_at:expires},scope,now)).toThrow('ANALYSIS_CAPABILITY_INVALID');
});
