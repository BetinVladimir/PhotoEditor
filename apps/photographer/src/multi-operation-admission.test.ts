import {expect,it,vi} from 'vitest';
import {authorizeMultiOperation} from './multi-operation-admission';
import type {Capabilities} from './catalog-types';
const capability=(ready:boolean):Capabilities=>({operations:[{operation:'optical.panorama',resource_class:'gpu',available:ready,status:ready?'ready':'unqualified',supported_backends:['cpu'],available_backends:ready?['cpu']:[]}]} as Capabilities);
it('rejects unavailable backend even when operation configuration exists',async()=>{
 const api={multiCapabilities:vi.fn().mockResolvedValue({}),capabilities:vi.fn().mockResolvedValue(capability(false))};
 await expect(authorizeMultiOperation(api,'optical.panorama','project',()=>true)).rejects.toThrow('MULTI_OPERATION_UNAVAILABLE');
});
it('accepts server CPU readiness independently of client hardware',async()=>{
 const api={multiCapabilities:vi.fn().mockResolvedValue({}),capabilities:vi.fn().mockResolvedValue(capability(true))};
 await authorizeMultiOperation(api,'optical.panorama','project',()=>true);expect(api.capabilities).toHaveBeenCalledOnce();
});
it('rejects a changed screen after the final availability await',async()=>{
 let current=true;
 const api={multiCapabilities:vi.fn().mockResolvedValue({}),capabilities:vi.fn(async()=>{current=false;return capability(true);})};
 await expect(authorizeMultiOperation(api,'optical.panorama','project',()=>current)).rejects.toThrow('MULTI_ATTEMPT_CANCELLED');
});
it('stops before availability read if cancelled during configuration lookup',async()=>{
 const controller=new AbortController();
 const api={multiCapabilities:vi.fn(async()=>{controller.abort();}),capabilities:vi.fn()};
 await expect(authorizeMultiOperation(api,'optical.panorama','project',()=>true,controller.signal)).rejects.toThrow('MULTI_ATTEMPT_CANCELLED');expect(api.capabilities).not.toHaveBeenCalled();
});
