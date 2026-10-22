import {expect,it,vi} from 'vitest';
import {OwnerAPI} from './owner-api';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
it('rejects malformed scopes before transport across shared UUID call sites',async()=>{
 const request=vi.fn(),api=new OwnerAPI({request} as never,id(1));
 const calls=[()=>api.analysisCapabilities('bad',true),()=>api.rawCapabilities('bad'),()=>api.downloadJob('bad'),()=>api.startDownload('bad',id(2)),()=>api.exportBatchStatus('bad'),()=>api.draft('bad'),()=>api.runOutputs('bad')];
 for(const call of calls)await expect(call()).rejects.toThrow('SCOPE_INVALID');
 expect(request).not.toHaveBeenCalled();
});
it('uses the validated style UUID as a string in both URL and response scope',async()=>{
 const response={schema_version:1,tenant_id:id(1),project_id:id(3),style_id:id(2),geometry_enabled:true,available:true,reason:null,backends:['mps'],expires_at:new Date(Date.now()+15000).toISOString()};
 const request=vi.fn().mockResolvedValue(response),api=new OwnerAPI({request} as never,id(1));
 expect((await api.analysisCapabilities(id(2),true)).style_id).toBe(id(2));
 expect(request.mock.calls[0][0]).toBe(`/api/v1/tenants/${id(1)}/styles/${id(2)}/analysis-capabilities?geometry_enabled=true`);
});
it('refuses malformed tenant scope before sending an otherwise valid resource read',async()=>{
 const request=vi.fn(),api=new OwnerAPI({request} as never,'invalid-tenant');
 await expect(api.downloadJob(id(2))).rejects.toThrow('SCOPE_INVALID');expect(request).not.toHaveBeenCalled();
});
