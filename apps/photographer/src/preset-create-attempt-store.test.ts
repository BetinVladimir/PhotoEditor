import {expect,it} from 'vitest';
import {PresetCreateAttemptStore,clearPresetCreateAttempts,type PresetCreateAttempt} from './preset-create-attempt-store';
const id='00000000-0000-4000-8000-000000000001';
const lock=async<T>(_origin:string,action:()=>Promise<T>)=>action();
function storage(){const values=new Map<string,string>();return {getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v)},removeItem:async(k:string)=>{values.delete(k)},getAllKeys:async()=>[...values.keys()]};}
function attempt():PresetCreateAttempt{return {schema_version:1,tenant_scope:id,key:crypto.randomUUID(),request:{name:'Web',config:{format:'jpeg',icc_id:'srgb_v1',quality:92,subsampling:'444',alpha:'matte',matte_rgb:[1,1,1],metadata_policy:'strip',naming_template:'{original_name}_edited'}},ticket:null};}
it('retains immutable request and key across remount and refuses replacement',async()=>{
 const db=storage(),origin='https://preset.test';const first=new PresetCreateAttemptStore(db,origin,id,id,id,lock),value=attempt();
 await first.write(value);value.request.config.matte_rgb![0]=0;
 const restored=await new PresetCreateAttemptStore(db,origin,id,id,id,lock).read();expect(restored?.request.config.matte_rgb).toEqual([1,1,1]);
 await expect(first.write({...restored!,key:crypto.randomUUID()})).rejects.toThrow('CONFLICT');
});
it('accepted receipt is monotonic and logout fences existing writers',async()=>{
 const db=storage(),origin='https://preset-logout.test',store=new PresetCreateAttemptStore(db,origin,id,id,id,lock),value=attempt();
 await store.write(value);await store.write({...value,ticket:{preset_id:id}});
 await expect(store.write(value)).rejects.toThrow('CONFLICT');
 await clearPresetCreateAttempts(db,origin,lock);await expect(store.write(value)).rejects.toThrow('EXPIRED');
});
