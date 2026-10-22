import {expect,it} from 'vitest';
import type {Capabilities} from './catalog-types';
import {operationAvailable,requireOperationAvailable} from './operation-availability';
const ready:Capabilities={operations:[{operation:'image.generate',resource_class:'gpu',supported_backends:['cuda'],available_backends:['cuda'],available:true,status:'ready'}]};
it('accepts remote qualified CUDA without inspecting client OS',()=>expect(operationAvailable(ready,'image.generate')).toBe(true));
it('rejects unknown, loading and lost availability',()=>{expect(operationAvailable(undefined,'image.generate')).toBe(false);expect(operationAvailable(ready,'missing')).toBe(false);expect(()=>requireOperationAvailable({operations:[]},'image.generate')).toThrow('EDITOR_OPERATION_UNAVAILABLE');});
it('rejects ambiguous or inconsistent server availability',()=>{const entry=ready.operations[0];for(const operations of [[entry,entry],[{...entry,status:'unqualified' as const}],[{...entry,available:false}],[{...entry,available_backends:[]}],[{...entry,available_backends:['mps']}]])expect(operationAvailable({operations},'image.generate')).toBe(false);});

it('fails closed for malformed runtime JSON instead of throwing during render',()=>{
 for(const value of [null,{}, {operations:null},{operations:[null]},{operations:[{operation:'image.generate',available:true,status:'ready'}]}])expect(operationAvailable(value as unknown as Capabilities,'image.generate')).toBe(false);
});


it('rejects duplicate or malformed supported and available backend evidence consistently with the server',()=>{
 const entry=ready.operations[0];
 for(const patch of [
  {supported_backends:['cuda','cuda']},
  {available_backends:['cuda','cuda']},
  {supported_backends:['cuda','']},
  {supported_backends:['cuda',null]},
  {supported_backends:['cuda',1]},
  {available_backends:['cuda',null]},
  {available_backends:['cuda',1]},
 ])expect(operationAvailable({operations:[{...entry,...patch}]} as unknown as Capabilities,'image.generate')).toBe(false);
});

it('accepts qualified Metal or remote CUDA solely from unique server backend evidence',()=>{
 const entry=ready.operations[0];
 for(const available_backends of [['mps'],['cuda'],['mps','cuda']]){
  expect(operationAvailable({operations:[{...entry,supported_backends:['cpu','cuda','mps'],available_backends}]},'image.generate')).toBe(true);
 }
});


it('refuses unknown or client OS names as execution backends',()=>{
 const entry=ready.operations[0];
 for(const backend of ['metal','CUDA','macos','webgpu','future-backend']){
  expect(operationAvailable({operations:[{...entry,supported_backends:[backend],available_backends:[backend]}]},'image.generate')).toBe(false);
 }
});
