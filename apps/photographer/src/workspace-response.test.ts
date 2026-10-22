import {expect,it} from 'vitest';
import {readWorkspaceTenant,readWorkspacePage} from './workspace-response';
const id='00000000-0000-4000-8000-000000000001';
const tenant={id,name:'Studio',role:'owner',revision:'1'};
it('validates current tenant projection and binds new creation to its request',()=>{
 expect(readWorkspaceTenant({...tenant,quotas:{storage_bytes:100,source_files:10,max_active_gpu_tasks:1},retention:{processing_days:7,exports_days:30,temporary_hours:24}},{createdName:'Studio'})).toEqual(tenant);
 for(const change of [{id:'not-a-uuid'},{id:'00000000-0000-0000-0000-000000000000'},{name:''},{name:'x'.repeat(121)},{role:'superuser'},{revision:1},{revision:'0'},{revision:'9223372036854775808'},{quotas:{storage_bytes:1}},{retention:null}])expect(()=>readWorkspaceTenant({...tenant,...change})).toThrow('WORKSPACE_RESPONSE_INVALID');
 expect(()=>readWorkspaceTenant({...tenant,role:'viewer'},{createdName:'Studio'})).toThrow();
 expect(()=>readWorkspaceTenant(tenant,{createdName:'Other'})).toThrow();
 expect(()=>readWorkspaceTenant(tenant,{id:'00000000-0000-4000-8000-000000000002'})).toThrow();
 // Recovery reads current settings; a legitimate later rename or role change is not a new POST.
 expect(readWorkspaceTenant({...tenant,name:'Renamed',role:'viewer',revision:'2'},{id}).role).toBe('viewer');
});
it('rejects malformed/duplicate list rows before publishing page navigation',()=>{
 expect(readWorkspacePage({items:[tenant],next_cursor:null})).toEqual({items:[tenant],next_cursor:null});
 for(const value of [{items:[tenant,tenant]},{items:[{...tenant,role:null}]},{items:[] ,next_cursor:{}},{items:'not-array'},{items:[],extra:1}])expect(()=>readWorkspacePage(value)).toThrow();
});
