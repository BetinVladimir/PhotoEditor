import {describe,it,expect} from 'vitest';
import {CaptureDraftStore,clearCaptureDrafts,clearCaptureSessionDrafts,parseCaptureDraft,type CaptureDraft} from './capture-draft-store';
const draft:CaptureDraft={version:1,revision:'9007199254740991',saved_at:1000,requirements:[{id:'12345678-1234-1234-1234-123456789abc',label:'Draft',person_ids:[],min_confirmed_frames:1,min_quality:.7}]};
describe('Capture device draft storage',()=>{
 it('validates bounds, version, expiry and duplicate bindings',()=>{
  expect(parseCaptureDraft(JSON.stringify(draft),1000)).toEqual(draft);
  for(const value of [{...draft,revision:'9223372036854775808'},{...draft,version:2},{...draft,saved_at:1001},{...draft,requirements:[draft.requirements[0],draft.requirements[0]]},{...draft,requirements:[{...draft.requirements[0],min_quality:2}]}])expect(parseCaptureDraft(JSON.stringify(value),1000)).toBeNull();
  expect(parseCaptureDraft(JSON.stringify(draft),1000+8*86400000)).toBeNull();
  expect(parseCaptureDraft('{')).toBeNull();
 });
 it('isolates origin, user, tenant and session',()=>{
  const storage={getItem:async()=>null,setItem:async()=>{},removeItem:async()=>{}};
  const base=new CaptureDraftStore(storage,'server','user','tenant','session').key;
  for(const scope of [['other','user','tenant','session'],['server','other','tenant','session'],['server','user','other','session'],['server','user','tenant','other']])expect(new CaptureDraftStore(storage,...scope as [string,string,string,string]).key).not.toBe(base);
 });
 it('serializes a discard after an in-flight write across store instances',async()=>{
  let value:string|null=null;let release!:()=>void;
  const gate=new Promise<void>(resolve=>{release=resolve});
  const storage={getItem:async()=>value,setItem:async(_key:string,next:string)=>{await gate;value=next},removeItem:async()=>{value=null}};
  const first=new CaptureDraftStore(storage,'server','user','tenant','session');
  const second=new CaptureDraftStore(storage,'server','user','tenant','session');
  const write=first.write(draft);const discard=second.clear();release();await Promise.all([write,discard]);
  expect(value).toBeNull();
 });
 it('removes invalid data instead of restoring it',async()=>{
  let value:string|null='invalid';const storage={getItem:async()=>value,setItem:async()=>{},removeItem:async()=>{value=null}};
  expect(await new CaptureDraftStore(storage,'server','user','tenant','session').read()).toBeNull();expect(value).toBeNull();
 });
 it('purges only the signed-out server and fences stale writers',async()=>{
  const values=new Map<string,string>();
  const storage={getAllKeys:async()=>[...values.keys()],getItem:async(key:string)=>values.get(key)??null,setItem:async(key:string,value:string)=>{values.set(key,value)},removeItem:async(key:string)=>{values.delete(key)}};
  const old=new CaptureDraftStore(storage,'purge-server','user','tenant','session');
  const foreign=new CaptureDraftStore(storage,'another-server','user','tenant','session');
  await old.write(draft);await foreign.write(draft);values.set('photo.locale','ru');
  const purge=clearCaptureDrafts(storage,'purge-server');const late=old.write(draft);
  await Promise.all([purge,late]);
  expect(values.has(old.key)).toBe(false);expect(values.has(foreign.key)).toBe(true);expect(values.get('photo.locale')).toBe('ru');
  expect(await old.read()).toBeNull();
  const renewed=new CaptureDraftStore(storage,'purge-server','user','tenant','session');await renewed.write(draft);expect(values.has(renewed.key)).toBe(true);
 });

 it('invalidates only the denied session while preserving other sessions',async()=>{
  const values=new Map<string,string>();
  const storage={getAllKeys:async()=>[...values.keys()],getItem:async(key:string)=>values.get(key)??null,setItem:async(key:string,value:string)=>{values.set(key,value)},removeItem:async(key:string)=>{values.delete(key)}};
  const denied=new CaptureDraftStore(storage,'acl-server','user','tenant','denied');
  const other=new CaptureDraftStore(storage,'acl-server','user','tenant','other');
  await denied.write(draft);await other.write(draft);
  await clearCaptureSessionDrafts(storage,'acl-server','tenant','denied');
  await denied.write(draft);await other.write({...draft,revision:'2'});
  expect(values.has(denied.key)).toBe(false);expect(JSON.parse(values.get(other.key)!).revision).toBe('2');expect(await denied.read()).toBeNull();
 });

});

it('retains full int64 string revisions without converting them to a Number',()=>{
 for(const revision of ['9007199254740992','9223372036854775807'])expect(parseCaptureDraft(JSON.stringify({...draft,revision}),1000)?.revision).toBe(revision);
});
it('discard fences another live store and a stale clear cannot delete a renewed draft',async()=>{
 let value:string|null=null;const storage={getItem:async()=>value,setItem:async(_key:string,next:string)=>{value=next},removeItem:async()=>{value=null}};
 const first=new CaptureDraftStore(storage,'closed-server','user','tenant','session'),other=new CaptureDraftStore(storage,'closed-server','user','tenant','session');await first.write(draft);await first.clear();await other.write(draft);expect(value).toBeNull();const renewed=new CaptureDraftStore(storage,'closed-server','user','tenant','session');await renewed.write({...draft,revision:'2'});await other.clear();expect(JSON.parse(value!).revision).toBe('2');
});
