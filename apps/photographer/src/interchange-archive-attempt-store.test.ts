import {expect,it} from 'vitest';
import {InterchangeArchiveAttemptStore,clearInterchangeArchiveAttempts,type InterchangeArchiveAttempt} from './interchange-archive-attempt-store';
import {archiveRequestSHA} from './interchange-archive-upload';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const request=()=>({filename:'archive.zip',bytes:22,sha256:'a'.repeat(64)});
function storage(){const values=new Map<string,string>();return {getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v)},removeItem:async(k:string)=>{values.delete(k)},getAllKeys:async()=>[...values.keys()]};}
const lock=async<T>(_origin:string,action:()=>Promise<T>)=>action();
const store=(s:ReturnType<typeof storage>,user=id(9))=>new InterchangeArchiveAttemptStore(s,'https://archive.test',user,id(1),id(2),request(),lock);
const pending=():InterchangeArchiveAttempt=>({version:1,key:'original',body:request(),checkpoint:null});
function uploading():InterchangeArchiveAttempt{return {...pending(),checkpoint:{receipt:{upload_id:id(3),state:'uploading',part_bytes:16777216,parts_total:1,request_sha256:archiveRequestSHA(id(2),request())},parts:[{part_no:1,etag:'receipt'}],stage:'uploading'}};}
it('persists exact intent then monotonic owned part progress',async()=>{
 const s=storage(),journal=store(s);await journal.write(pending());await journal.write(uploading());
 const restored=await store(s).read();expect(restored).toEqual(uploading());restored!.checkpoint!.parts[0].etag='changed';expect(await journal.read()).toEqual(uploading());
 await expect(journal.write(pending())).rejects.toThrow();await expect(journal.write({...uploading(),key:'replacement'})).rejects.toThrow();
 const changed=uploading();changed.checkpoint!.parts[0].etag='changed';await expect(journal.write(changed)).rejects.toThrow();
 await expect(journal.clear('original')).rejects.toThrow();
 const queued=uploading();queued.checkpoint!.stage='queued';await journal.write(queued);await expect(journal.write(uploading())).rejects.toThrow();await journal.clear('original');expect(await journal.read()).toBeNull();
});
it('isolates principals and fences mounted stores on logout',async()=>{
 const s=storage(),a=store(s),b=store(s,id(8));await a.write(pending());expect(await b.read()).toBeNull();
 await clearInterchangeArchiveAttempts(s,'https://archive.test',lock);await expect(a.read()).rejects.toThrow();await expect(a.write(pending())).rejects.toThrow();expect(await store(s).read()).toBeNull();
});

it('makes durable abort intent irreversible for the uploader',async()=>{
 const journal=store(storage()),record=uploading();await journal.write(record);record.checkpoint!.stage='aborting';await journal.write(record);
 await expect(journal.write(uploading())).rejects.toThrow();expect((await journal.read())?.checkpoint?.stage).toBe('aborting');
});

it('clears only the exact aborting attempt after explicit terminal confirmation',async()=>{
 const journal=store(storage()),record=uploading();await journal.write(record);
 await expect(journal.clearAborted('original',id(3),record)).rejects.toThrow();
 record.checkpoint!.stage='aborting';await journal.write(record);
 await expect(journal.clearAborted('foreign',id(3),record)).rejects.toThrow();await expect(journal.clearAborted('original',id(4),record)).rejects.toThrow();
 expect((await journal.read())?.key).toBe('original');await journal.clearAborted('original',id(3),record);expect(await journal.read()).toBeNull();
});

it('refuses terminal clear when the expected full checkpoint changed',async()=>{
 const journal=store(storage()),record=uploading();record.checkpoint!.stage='aborting';await journal.write(record);
 const expected=structuredClone(record);expected.checkpoint!.parts[0].etag='stale';
 await expect(journal.clearAborted('original',id(3),expected)).rejects.toThrow('INTERCHANGE_ARCHIVE_JOURNAL_CONFLICT');
 expect(await journal.read()).toEqual(record);await journal.clearAborted('original',id(3),record);expect(await journal.read()).toBeNull();
});
