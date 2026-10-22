import {expect,it} from 'vitest';
import {PresetMetadataAttemptStore,clearPresetMetadataAttempts,type PresetMetadataAttempt} from './preset-metadata-attempt-store';
import type {ExportPreset} from './catalog-types';
import {exportPresetConfigHash} from './export-preset-response';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const lock=async<T>(_origin:string,action:()=>Promise<T>)=>action();
function fixture(origin:string){const data=new Map<string,string>();const storage={getItem:async(key:string)=>data.get(key)??null,setItem:async(key:string,value:string)=>{data.set(key,value)},removeItem:async(key:string)=>{data.delete(key)},getAllKeys:async()=>[...data.keys()]};const store=new PresetMetadataAttemptStore(storage,origin,id(1),id(2),id(3),lock);const record:PresetMetadataAttempt={version:1,key:id(4),preset:id(3),revision:'1',mode:'copyright'};return {data,storage,store,record}}
it('recovers immutable metadata CAS and refuses any changed key, revision, mode or preset',async()=>{
 const f=fixture('https://metadata-immutable.test');await f.store.write(f.record);
 expect(await new PresetMetadataAttemptStore(f.storage,'https://metadata-immutable.test',id(1),id(2),id(3),lock).read()).toEqual(f.record);
 for(const patch of [{key:id(5)},{revision:'2'},{mode:'approved_exif' as const},{preset:id(8)}])await expect(async()=>f.store.write({...f.record,...patch})).rejects.toThrow();
 expect(await f.store.read()).toEqual(f.record);
});
it('clears only an exact accepted revision and metadata policy receipt',async()=>{
 const f=fixture('https://metadata-receipt.test');await f.store.write(f.record);
 const config={format:'png' as const,icc_id:'srgb_v1' as const,subsampling:'444' as const,alpha:'preserve' as const,metadata_policy:'copyright' as const,naming_template:'{name}'};
 const result:ExportPreset={id:id(3),name:'Print',revision:'2',config,config_hash:exportPresetConfigHash(config),created_at:'2026-10-17T00:00:00Z'};
 for(const invalid of [{...result,id:id(8)},{...result,revision:'3'},{...result,config:{metadata_policy:'strip'}}])expect(()=>f.store.clearAccepted(f.record,invalid as ExportPreset)).toThrow('PRESET_METADATA_RECEIPT_INVALID');
 await f.store.clearAccepted(f.record,result);expect(await f.store.read()).toBeNull();
});
it('logout clears metadata commands and fences retained stores and old writes',async()=>{
 const origin='https://metadata-logout.test',f=fixture(origin);await f.store.write(f.record);
 await clearPresetMetadataAttempts(f.storage,origin,lock);await expect(async()=>f.store.write(f.record)).rejects.toThrow('EXPIRED');
 expect(await new PresetMetadataAttemptStore(f.storage,origin,id(1),id(2),id(3),lock).read()).toBeNull();
});
it('bounds raw journal parsing and rejects malformed or different-resource records',async()=>{
 const f=fixture('https://metadata-invalid.test');
 for(const raw of ['x'.repeat(2049),'[]','null',JSON.stringify({...f.record,preset:id(8)}),JSON.stringify({...f.record,revision:'01'}),JSON.stringify({...f.record,extra:true})]){f.data.set(f.store.storageKey,raw);await expect(f.store.read()).rejects.toThrow('INVALID')}
});
it('isolates metadata intents by account, tenant and preset',async()=>{
 const origin='https://metadata-scope.test',f=fixture(origin);await f.store.write(f.record);
 for(const [user,tenant,preset] of [[id(8),id(2),id(3)],[id(1),id(8),id(3)],[id(1),id(2),id(8)]])expect(await new PresetMetadataAttemptStore(f.storage,origin,user,tenant,preset,lock).read()).toBeNull();
});
it('refuses an established writer after another context changes the persisted logout generation',async()=>{
 const origin='https://metadata-cross-context.test',f=fixture(origin);await f.store.read();
 f.data.set('photo.preset-metadata-generation.v1:'+JSON.stringify(origin),id(8));
 await expect(async()=>f.store.write(f.record)).rejects.toThrow('EXPIRED');expect(f.data.has(f.store.storageKey)).toBe(false);
});
it('pins fresh full baseline, refuses changed baseline and rejects altered accepted configuration before removal',async()=>{
 const f=fixture('https://metadata-baseline.test');const config={format:'png' as const,icc_id:'srgb_v1' as const,subsampling:'444' as const,alpha:'preserve' as const,metadata_policy:'strip' as const,naming_template:'{name}'};
 const baseline:ExportPreset={id:id(3),name:'Print',revision:'1',config,config_hash:exportPresetConfigHash(config),created_at:'2026-10-17T00:00:00Z'};
 const record={...f.record,baseline};await f.store.write(record);expect((await f.store.read())?.baseline).toEqual(baseline);
 await expect(async()=>f.store.write({...record,baseline:{...baseline,name:'Other'}})).rejects.toThrow('CONFLICT');
 const altered={...baseline,revision:'2',config:{...config,metadata_policy:'copyright' as const,naming_template:'altered'}};altered.config_hash=exportPresetConfigHash(altered.config);
 expect(()=>f.store.clearAccepted(record,altered)).toThrow('PRESET_METADATA_RECEIPT_INVALID');expect(await f.store.read()).toEqual(record);
 const accepted={...baseline,revision:'2',config:{...config,metadata_policy:'copyright' as const}};accepted.config_hash=exportPresetConfigHash(accepted.config);await f.store.clearAccepted(record,accepted);expect(await f.store.read()).toBeNull();
});
