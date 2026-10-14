import {it,expect,vi} from 'vitest';
import {OwnerAPI} from './owner-api';
import {renderInputSnapshotRequest,renderInputSnapshotDigest,renderInputSnapshotResult} from './render-input-snapshot';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`,sha='a'.repeat(64);
const request=()=>({sources:[{asset_version_id:id(1),source:{artifact_id:id(2),sha256:sha},canvas_hash:sha}]});
it('pins exact source inventory and validates returned digest/count',async()=>{
 let resolve!:(v:unknown)=>void;const transport=vi.fn((..._args:unknown[])=>new Promise(r=>{resolve=r}));const api=new OwnerAPI({request:transport} as never,'tenant/id'),signal=new AbortController().signal;
 const body=request(),digest=renderInputSnapshotDigest(body),pending=api.createRenderInputSnapshot('project/id',body,'snapshot-key',signal);body.sources[0].source.sha256='b'.repeat(64);
 expect(transport.mock.calls[0]).toEqual(['/api/v1/tenants/tenant%2Fid/projects/project%2Fid/render-input-snapshots',expect.objectContaining({body:request(),idempotencyKey:'snapshot-key',signal})]);
 resolve({id:id(3),digest,item_count:1});expect(await pending).toEqual({id:id(3),digest,item_count:1});
});
it('rejects duplicate versions, extra fields and mismatched snapshot evidence',()=>{
 expect(()=>renderInputSnapshotRequest({sources:[request().sources[0],request().sources[0]]})).toThrow();
 expect(()=>renderInputSnapshotRequest({...request(),policy_sha:sha} as never)).toThrow();
 const digest=renderInputSnapshotDigest(request());
 expect(()=>renderInputSnapshotResult({id:id(3),digest:'b'.repeat(64),item_count:1},digest,1)).toThrow();
 expect(()=>renderInputSnapshotResult({id:id(3),digest,item_count:2},digest,1)).toThrow();
});

it('matches the independent Python canonical digest and normalizes Go UUID encoding',()=>{
 expect(renderInputSnapshotDigest(request())).toBe('bf8ecfdcf3f55e3a2d5bda992df5cd8113884308bcc0bb295731896a798a03e2');
 const upper=request();upper.sources[0].asset_version_id='AAAAAAAA-1111-1111-1111-111111111111';
 expect(renderInputSnapshotRequest(upper).sources[0].asset_version_id).toBe('aaaaaaaa-1111-1111-1111-111111111111');
 expect(renderInputSnapshotDigest(upper)).toBe(renderInputSnapshotDigest(renderInputSnapshotRequest(upper)));
});
