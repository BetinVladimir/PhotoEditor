import {it,expect,vi} from 'vitest';
import {OwnerAPI} from './owner-api';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`,sha='a'.repeat(64);
const source=()=>({asset_version_id:id(1),source:{artifact_id:id(2),sha256:sha},canvas_hash:sha});
const calls=[(api:OwnerAPI,p:string,s:AbortSignal)=>api.renderPolicy(p,s),(api:OwnerAPI,p:string,s:AbortSignal)=>api.createRenderInputSnapshot(p,{sources:[source()]},'same-key',s),(api:OwnerAPI,p:string,s:AbortSignal)=>api.queueRenderBatch(p,{input_snapshot_id:id(3),policy_sha:sha,selection:[{source:source(),recipe:{artifact_id:id(4),sha256:sha}}]},'same-key',s)];
it('rejects malformed project and preaborted scopes before any Render transport',async()=>{
 for(const invoke of calls){const request=vi.fn();const api=new OwnerAPI({request} as never,id(10)),c=new AbortController();await expect(invoke(api,'bad',c.signal)).rejects.toThrow();c.abort();await expect(invoke(api,id(11),c.signal)).rejects.toThrow();expect(request).not.toHaveBeenCalled();}
});
it('refuses late Render responses after cancellation even when transport ignores the signal',async()=>{
 for(const invoke of calls){let resolve!:(v:unknown)=>void;const request=vi.fn(()=>new Promise(r=>{resolve=r}));const api=new OwnerAPI({request} as never,id(10)),c=new AbortController();const pending=invoke(api,id(11),c.signal);const rejected=expect(pending).rejects.toThrow();expect(request).toHaveBeenCalledTimes(1);c.abort();resolve({});await rejected;}
});
