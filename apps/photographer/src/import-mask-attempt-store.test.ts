import {it,expect} from 'vitest';
import {ImportMaskAttemptStore,clearImportMaskAttempts} from './import-mask-attempt-store';
const id=(n:number)=>`${n.toString(16).padStart(8,'0')}-0000-4000-8000-000000000001`;
const memory=()=>{const data=new Map<string,string>();return {getItem:async(k:string)=>data.get(k)??null,setItem:async(k:string,v:string)=>{data.set(k,v)},removeItem:async(k:string)=>{data.delete(k)},getAllKeys:async()=>[...data.keys()]};};
const lock=async<T>(_origin:string,action:()=>Promise<T>)=>action();
const record=()=>({version:1 as const,key:id(6),body:{action:'revoke' as const,expected_epoch:1,entry_id:id(7),target_version_id:id(8),override_artifact_id:id(9),mask_artifact_id:id(10)}});
it('restores only the same principal and clears only exact successor receipt',async()=>{
 const storage=memory(),make=(user=id(1))=>new ImportMaskAttemptStore(storage,'https://mask.test',user,id(2),id(3),id(4),lock);
 const first=make(),saved=record();await first.write(saved);saved.body.expected_epoch=5;
 expect((await make().read())?.body.expected_epoch).toBe(1);expect(await make(id(11)).read()).toBeNull();
 await expect(first.write(saved)).rejects.toThrow('IMPORT_MASK_JOURNAL_CONFLICT');
 await expect(first.clear(id(6),{workflow_id:id(4),entry_id:id(7),status:'revoked',epoch:3})).rejects.toThrow();
 await first.clear(id(6),{workflow_id:id(4),entry_id:id(7),status:'revoked',epoch:2});expect(await make().read()).toBeNull();
});
it('fences pre-logout stores and removes pending decisions',async()=>{
 const storage=memory(),origin='https://mask-logout.test',store=new ImportMaskAttemptStore(storage,origin,id(1),id(2),id(3),id(4),lock);
 await store.write(record());await clearImportMaskAttempts(storage,origin,lock);
 await expect(store.write(record())).rejects.toThrow('IMPORT_MASK_JOURNAL_EXPIRED');
 expect(await new ImportMaskAttemptStore(storage,origin,id(1),id(2),id(3),id(4),lock).read()).toBeNull();
});

it('fences another instance after acknowledgement and refuses resurrection',async()=>{
 const storage=memory(),make=()=>new ImportMaskAttemptStore(storage,'https://mask-race.test',id(1),id(2),id(3),id(4),lock);
 const first=make(),other=make(),saved=record();await first.write(saved);await other.read();
 await first.clear(saved.key,{workflow_id:id(4),entry_id:id(7),status:'revoked',epoch:2});
 await expect(other.write(saved)).rejects.toThrow('IMPORT_MASK_JOURNAL_EXPIRED');
 await expect(make().write(saved)).rejects.toThrow('IMPORT_MASK_JOURNAL_EXPIRED');
 expect(await make().read()).toBeNull();
});
it('ignores acknowledged stale bytes after interrupted removal',async()=>{
 const storage=memory(),store=new ImportMaskAttemptStore(storage,'https://mask-crash.test',id(1),id(2),id(3),id(4),lock),saved=record();
 await store.write(saved);await store.clear(saved.key,{workflow_id:id(4),entry_id:id(7),status:'revoked',epoch:2});
 await storage.setItem(store.key,JSON.stringify(saved));
 const fresh=new ImportMaskAttemptStore(storage,'https://mask-crash.test',id(1),id(2),id(3),id(4),lock);
 expect(await fresh.read()).toBeNull();
 await fresh.write({...saved,key:id(12),body:{...saved.body,expected_epoch:2}});
 expect((await fresh.read())?.key).toBe(id(12));
});

it('retires only a consumed epoch for the exact matching authoritative status',async()=>{
 const storage=memory(),store=new ImportMaskAttemptStore(storage,'https://mask-superseded.test',id(1),id(2),id(3),id(4),lock),saved=record();await store.write(saved);
 const status={entry_id:id(7),target_version_id:id(8),override_artifact_id:id(9),mask_artifact_id:id(10),status:'revoked' as const,epoch:2};
 await expect(store.clearSuperseded(saved.key,[{...status,epoch:1,status:'granted'}])).rejects.toThrow();
 await expect(store.clearSuperseded(saved.key,[{...status,entry_id:id(11),epoch:3}])).rejects.toThrow();
 expect(await store.read()).not.toBeNull();await store.clearSuperseded(saved.key,[status]);expect(await store.read()).toBeNull();
 await expect(store.write(saved)).rejects.toThrow('IMPORT_MASK_JOURNAL_EXPIRED');
});

it('retires a same-epoch no-op only when the desired status already holds',async()=>{
 const storage=memory(),store=new ImportMaskAttemptStore(storage,'https://mask-noop.test',id(1),id(2),id(3),id(4),lock),saved=record();await store.write(saved);
 await store.clearSuperseded(saved.key,[{entry_id:id(7),target_version_id:id(8),override_artifact_id:id(9),mask_artifact_id:id(10),status:'revoked',epoch:1}]);
 expect(await store.read()).toBeNull();
});
