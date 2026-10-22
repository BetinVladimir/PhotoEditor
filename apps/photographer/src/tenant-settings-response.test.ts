import {expect,it,vi} from 'vitest';
import {OwnerAPI} from './owner-api';
import {readTenantSettings} from './tenant-settings-response';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const row=()=>({id:id(1),name:'Studio',role:'admin' as const,revision:'9223372036854775806',quotas:{storage_bytes:2**50,source_files:1000000,max_active_gpu_tasks:16},retention:{processing_days:365,exports_days:365,temporary_hours:168}});
it('preserves the actual maximum numeric quota and exact int64 revision, with detached policies',()=>{
 const wire=row(),result=readTenantSettings(wire,id(1));wire.quotas.storage_bytes=1048576;expect(result.quotas.storage_bytes).toBe(2**50);expect(result.revision).toBe('9223372036854775806');
});
it('projects historical bootstrap metadata without reinterpreting custom values or effective GPU allowance',()=>{
 const legacy={...row(),quotas:{storage_bytes:10737418240,source_files:10000,active_runs:9,active_gpu_tasks:7},retention:{deleted_days:91,artifact_days:83}};
 const result=readTenantSettings(legacy,id(1));
 expect(result.quotas).toEqual({storage_bytes:10737418240,source_files:10000,max_active_gpu_tasks:1});
 expect(result.retention).toEqual({processing_days:7,exports_days:30,temporary_hours:24});
 expect(legacy.quotas.active_gpu_tasks).toBe(7);expect(legacy.retention.deleted_days).toBe(91);
 expect(readTenantSettings({...legacy,quotas:{...legacy.quotas,max_active_gpu_tasks:3},retention:{...legacy.retention,processing_days:19,exports_days:42,temporary_hours:48}},id(1))).toMatchObject({quotas:{max_active_gpu_tasks:3},retention:{processing_days:19,exports_days:42,temporary_hours:48}});
});
it('keeps stored metadata outside editable policy and refuses malformed known historical metadata',()=>{
 expect(readTenantSettings({...row(),quotas:{...row().quotas,other:{note:'stored metadata'}}},id(1))).toEqual(row());
 for(const raw of [{...row(),quotas:{...row().quotas,active_gpu_tasks:'7'}},{...row(),retention:{...row().retention,deleted_days:-1}},{...row(),retention:{...row().retention,artifact_days:1.5}}])expect(()=>readTenantSettings(raw,id(1))).toThrow('TENANT_SETTINGS_INVALID');
});
it('refuses cross tenant, absent policies, unsafe or out of contract quotas and malformed revision',()=>{
 for(const raw of [{...row(),id:id(2)},{...row(),quotas:null},{...row(),retention:{}},{...row(),revision:'01'},{...row(),revision:'9223372036854775808'},{...row(),quotas:{...row().quotas,storage_bytes:9007199254740992}},{...row(),quotas:{...row().quotas,source_files:1.5}},{...row(),retention:{...row().retention,temporary_hours:169}}])expect(()=>readTenantSettings(raw,id(1))).toThrow();
});
it('mounts strict decoding on GET and binds CAS response to captured revision and body',async()=>{
 const request=vi.fn().mockResolvedValue(row()),api=new OwnerAPI({request} as never,id(1));expect(await api.tenantSettings()).toEqual(row());
 request.mockResolvedValue({...row(),name:'Updated',revision:'9223372036854775807'});await api.updateTenant(row(),{name:' Updated '});expect(request.mock.calls[1][1]).toMatchObject({method:'PATCH',headers:{'If-Match':'"9223372036854775806"'},body:{name:'Updated'}});
 request.mockResolvedValue({...row(),name:'Updated'});await expect(api.updateTenant(row(),{name:'Updated'})).rejects.toThrow('RECEIPT');
 request.mockResolvedValue({...row(),id:id(2)});await expect(api.tenantSettings()).rejects.toThrow();
});
it('freezes CAS policies before transport and refuses late cancellation without publishing malformed receipts',async()=>{
 let resolve!:(v:unknown)=>void;const request=vi.fn((_path:string,_options?:Record<string,unknown>)=>new Promise<unknown>(r=>{resolve=r})),api=new OwnerAPI({request} as never,id(1));
 const before=row(),patch={retention:{processing_days:7,exports_days:30,temporary_hours:24}},pending=api.updateTenant(before,patch);patch.retention.processing_days=999;before.revision='1';
 resolve({...row(),revision:'9223372036854775807',retention:{processing_days:7,exports_days:30,temporary_hours:24}});expect((await pending).retention.processing_days).toBe(7);expect(request.mock.calls[0][1]!.body).toEqual({retention:{processing_days:7,exports_days:30,temporary_hours:24}});
 const abort=new AbortController(),late=api.tenantSettings(abort.signal);abort.abort();resolve({});await expect(late).rejects.toMatchObject({name:'AbortError'});
 const count=request.mock.calls.length;await expect(api.tenantSettings(abort.signal)).rejects.toMatchObject({name:'AbortError'});expect(request).toHaveBeenCalledTimes(count);
});
it('refuses invalid CAS input before transport',async()=>{const request=vi.fn(),api=new OwnerAPI({request} as never,id(1));for(const patch of [{},{name:' '},{quotas:{...row().quotas,storage_bytes:2**51}}])await expect(api.updateTenant(row(),patch)).rejects.toThrow();expect(request).not.toHaveBeenCalled();});
