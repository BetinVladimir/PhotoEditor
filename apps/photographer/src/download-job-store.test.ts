import {APIError} from "@photoeditor/api-client";
import {expect,it} from 'vitest';
import {DownloadJobStore,clearDownloadJobs} from './download-job-store';
it('isolates account and tenant and invalidates stores on logout',async()=>{
 const data=new Map<string,string>();const storage={getItem:async(k:string)=>data.get(k)??null,setItem:async(k:string,v:string)=>{data.set(k,v)},removeItem:async(k:string)=>{data.delete(k)},getAllKeys:async()=>[...data.keys()]};
 const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
 const store=new DownloadJobStore(storage,'https://server.test',id(1),id(2));
 await store.write(id(3));expect(await store.read()).toBe(id(3));
 expect(await new DownloadJobStore(storage,'https://server.test',id(4),id(2)).read()).toBeNull();
 expect(await new DownloadJobStore(storage,'https://server.test',id(1),id(5)).read()).toBeNull();
 await clearDownloadJobs(storage,'https://server.test');
 await expect(store.read()).rejects.toThrow('DOWNLOAD_STORE_EXPIRED');expect([...data.keys()].every(key=>key.startsWith('photo.download-job-generation.v1:'))).toBe(true);
});

it('reuses pending start after store reconstruction and acknowledges only exact receipt',async()=>{
 const data=new Map<string,string>();const storage={getItem:async(k:string)=>data.get(k)??null,setItem:async(k:string,v:string)=>{data.set(k,v)},removeItem:async(k:string)=>{data.delete(k)}};
 const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
 const first=new DownloadJobStore(storage,'https://retry.test',id(1),id(2));const pending=await first.begin(id(3),id(4));
 const restarted=new DownloadJobStore(storage,'https://retry.test',id(1),id(2));
 expect(await restarted.readAttempt()).toEqual(pending);expect(await restarted.begin(id(5),id(6))).toEqual(pending);
 await expect(restarted.complete({...pending,key:id(7)},id(8))).rejects.toThrow('DOWNLOAD_ATTEMPT_CONFLICT');
 await restarted.complete(pending,id(8));expect(await restarted.read()).toBe(id(8));expect(await restarted.readAttempt()).toBeNull();
});

it('retains ambiguous failures but releases an exact definitive refusal',async()=>{
 const data=new Map<string,string>();const storage={getItem:async(k:string)=>data.get(k)??null,setItem:async(k:string,v:string)=>{data.set(k,v)},removeItem:async(k:string)=>{data.delete(k)}};
 const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
 const store=new DownloadJobStore(storage,'https://refusal.test',id(1),id(2));const pending=await store.begin(id(3),id(4));
 expect(await store.refuse(pending,new Error('connection reset'))).toBe(false);
 expect(await store.readAttempt()).toEqual(pending);
 expect(await store.refuse(pending,new APIError(503,'UNAVAILABLE'))).toBe(false);
 await expect(store.refuse({...pending,key:id(8)},new APIError(409,'SNAPSHOT_STALE'))).rejects.toThrow('DOWNLOAD_ATTEMPT_CONFLICT');
 expect(await store.refuse(pending,new APIError(409,'SNAPSHOT_STALE'))).toBe(true);
 expect(await store.readAttempt()).toBeNull();
});

it('rejects a journal writer when another browser context advances persisted logout generation',async()=>{
 const data=new Map<string,string>(),id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
 const storage={getItem:async(k:string)=>data.get(k)??null,setItem:async(k:string,v:string)=>{data.set(k,v)},removeItem:async(k:string)=>{data.delete(k)}};
 const origin='https://cross-tab-download.test',store=new DownloadJobStore(storage,origin,id(1),id(2));
 await store.read();data.set('photo.download-job-generation.v1:'+JSON.stringify(origin),id(9));
 await expect(store.begin(id(3),id(4))).rejects.toThrow('DOWNLOAD_STORE_EXPIRED');
 expect([...data.keys()].some(key=>key.startsWith('photo.download-job.v1:'))).toBe(false);
});
