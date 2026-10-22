import {expect,it,vi} from 'vitest';
import {readCapabilities} from './capabilities-response';
import {OwnerAPI} from './owner-api';
const operation={operation:'image.generate',resource_class:'gpu',model_id:null,supported_backends:['cuda'],available_backends:['cuda'],available:true,status:'ready',reason:null};
it('owns a server snapshot and accepts nullable server metadata',()=>{
 const source={operations:[structuredClone(operation)]},result=readCapabilities(source);source.operations[0].available_backends.length=0;
 expect(result.operations[0].available_backends).toEqual(['cuda']);expect(result.operations[0].model_id).toBeUndefined();expect(result.operations[0].reason).toBeUndefined();
});
it('accepts unavailable CUDA-only deployment and qualified Metal operations',()=>{
 expect(readCapabilities({operations:[{...operation,available:false,status:'unqualified',available_backends:[],reason:'no_qualified_worker'}]}).operations[0].available).toBe(false);
 expect(readCapabilities({operations:[{...operation,supported_backends:['mps','cpu'],available_backends:['mps']}]}).operations[0].available).toBe(true);
});
it('rejects malformed or ambiguous snapshots before UI consumption',()=>{
 for(const value of [null,{}, {operations:[operation,operation]}, {operations:[{...operation,available_backends:['mps']}]},{operations:[{...operation,status:'unqualified'}]},{operations:[{...operation,available_backends:null}]},{operations:[{...operation,available_backends:['cuda','cuda']}]},{operations:[null]}])expect(()=>readCapabilities(value)).toThrow('CAPABILITIES_INVALID');
});


it('rejects a ready snapshot naming an unknown execution backend',()=>{
 for(const backend of ['metal','CUDA','webgpu']){
  expect(()=>readCapabilities({operations:[{operation:'image.generate',resource_class:'gpu',
   supported_backends:[backend],available_backends:[backend],available:true,status:'ready'}]})).toThrow('CAPABILITIES_INVALID');
 }
});
it('distinguishes nullable server metadata from missing or contradictory evidence',()=>{
 const {model_id:_model,...withoutModel}=operation,{reason:_reason,...withoutReason}=operation;
 for(const value of [{operations:[withoutModel]},{operations:[withoutReason]},{operations:[{...operation,extra:true}]},{operations:[operation],extra:true},{operations:[{...operation,reason:'no_qualified_worker'}]},{operations:[{...operation,available:false,status:'unqualified',available_backends:[]}]},{operations:[{...operation,available:false,status:'unsupported',available_backends:[],reason:'no_supported_backend'}]}])expect(()=>readCapabilities(value)).toThrow('CAPABILITIES_INVALID');
 expect(readCapabilities({operations:[]})).toEqual({operations:[]});
});
it('does not enable actions from an otherwise valid late reply after cancellation',async()=>{
 let finish!:(value:unknown)=>void;const request=vi.fn(()=>new Promise(done=>{finish=done;})),api=new OwnerAPI({request} as never,'00000000-0000-4000-8000-000000000001'),controller=new AbortController();
 const pending=api.capabilities(controller.signal);controller.abort();finish({operations:[operation]});
 await expect(pending).rejects.toMatchObject({name:'AbortError'});await expect(api.capabilities(controller.signal)).rejects.toMatchObject({name:'AbortError'});expect(request).toHaveBeenCalledTimes(1);
});
