import {it,expect,vi} from 'vitest';
import type {Capabilities} from './catalog-types';
import {admitEditorOperation} from './editor-operation-admission';
const ready=(operation:string)=>({operations:[{operation,status:'ready',available:true,supported_backends:['cpu','cuda','mps'],available_backends:['mps']}]}) as Capabilities;
it.each(['mask.prompt','mask.rasterize','image.generate'])('%s refuses current unavailable capability after earlier save',async(operation)=>{
 let capabilities:Capabilities|undefined=ready(operation);
 const scope={current:()=>true,capabilities:()=>capabilities},submit=vi.fn();
 capabilities=undefined;
 await expect(admitEditorOperation(operation,scope,submit)).rejects.toThrow('EDITOR_OPERATION_UNAVAILABLE');expect(submit).not.toHaveBeenCalled();
});
it.each(['mask.prompt','mask.rasterize','image.generate'])('%s refuses replaced route before transport',async(operation)=>{
 const submit=vi.fn();await expect(admitEditorOperation(operation,{current:()=>false,capabilities:()=>ready(operation)},submit)).rejects.toThrow('EDITOR_ROUTE_CHANGED');expect(submit).not.toHaveBeenCalled();
});
it.each(['mask.prompt','mask.rasterize','image.generate'])('%s never hands a late accepted result to replacement route',async(operation)=>{
 let current=true,finish!:(value:{run_id:string})=>void;
 const submit=vi.fn(()=>new Promise<{run_id:string}>(resolve=>{finish=resolve}));
 const accepted=admitEditorOperation(operation,{current:()=>current,capabilities:()=>ready(operation)},submit);
 current=false;finish({run_id:'old-run'});await expect(accepted).rejects.toThrow('EDITOR_ROUTE_CHANGED');expect(submit).toHaveBeenCalledOnce();
});
it.each(['mask.prompt','mask.rasterize','image.generate'])('%s preserves accepted result when deployment becomes unavailable',async(operation)=>{
 let capabilities:Capabilities|undefined=ready(operation),finish!:(value:{run_id:string})=>void;
 const result={run_id:'accepted-run'},accepted=admitEditorOperation(operation,{current:()=>true,capabilities:()=>capabilities},()=>new Promise(resolve=>{finish=resolve}));
 capabilities=undefined;finish(result);await expect(accepted).resolves.toBe(result);
});
