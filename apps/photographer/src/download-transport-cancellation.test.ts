import {expect,it,vi} from 'vitest';
import {OwnerAPI} from './owner-api';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;

for(const phase of ['start','status','cancel','intent','legacy-status'] as const)it(`download ${phase} refuses pre-abort and late transport result`,async()=>{
 let settle!:(value:unknown)=>void;
 const request=vi.fn((..._args:unknown[])=>new Promise<unknown>(resolve=>{settle=resolve}));
 const api=new OwnerAPI({request} as never,id(1));
 const invoke=(signal:AbortSignal)=>phase==='start'?api.startDownload(id(2),id(3),'original-key',signal):phase==='status'?api.downloadJob(id(2),signal):phase==='cancel'?api.cancelDownloadJob(id(2),signal):phase==='intent'?api.downloadJobVolume(id(2),1,signal):api.download(id(2),signal);
 const controller=new AbortController();
 const pending=invoke(controller.signal);
 controller.abort();
 // Deliberately invalid DTO: cancellation must win before decoder/publication.
 settle({unexpected:'late response'});
 await expect(pending).rejects.toMatchObject({name:'AbortError'});
 await expect(invoke(controller.signal)).rejects.toMatchObject({name:'AbortError'});
 expect(request).toHaveBeenCalledTimes(1);
 expect(request.mock.calls[0][1]).toMatchObject({signal:controller.signal});
});
