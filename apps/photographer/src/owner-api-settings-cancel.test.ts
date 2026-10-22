import {expect,it,vi} from 'vitest';
import {OwnerAPI} from './owner-api';
const tenant='11111111-1111-4111-8111-111111111111';
it.each(['profile','tenant','style'] as const)('forwards %s cancellation and rejects late transport success',async kind=>{
 let finish!:(value:unknown)=>void;const request=vi.fn((..._args:unknown[])=>new Promise(done=>{finish=done;})),api=new OwnerAPI({request} as never,tenant),controller=new AbortController();
 const patch={name:'Frozen',retention:{processing_days:4,exports_days:30,temporary_hours:24}};
 const profile={id:tenant,email:'owner@example.test',locale:'en',theme:'system',timezone:'UTC',revision:'7'} as const;
 const settings={id:tenant,name:'Studio',role:'admin',revision:'7',quotas:{storage_bytes:1048576,source_files:1000,max_active_gpu_tasks:1},retention:{processing_days:7,exports_days:30,temporary_hours:24}} as const;
 const invoke=()=>kind==='profile'?api.updateMe(profile,{locale:'ru'},controller.signal):kind==='tenant'?api.updateTenant(settings,patch,controller.signal):api.createStyle(tenant,'Name','Description','stable-key',controller.signal);
 const pending=invoke();patch.retention.processing_days=9;expect(request.mock.calls[0]?.[1]).toMatchObject({signal:controller.signal});
 if(kind==='tenant')expect(request.mock.calls[0]?.[1]).toMatchObject({headers:{'If-Match':'"7"'},body:{name:'Frozen',retention:{processing_days:4,exports_days:30,temporary_hours:24}}});
 if(kind==='style')expect(request.mock.calls[0]?.[1]).toMatchObject({idempotencyKey:'stable-key',body:{name:'Name',description:'Description',project_id:tenant}});
 controller.abort();finish({id:tenant});await expect(pending).rejects.toMatchObject({name:'AbortError'});await expect(invoke()).rejects.toMatchObject({name:'AbortError'});expect(request).toHaveBeenCalledTimes(1);
});
