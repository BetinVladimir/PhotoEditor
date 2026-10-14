import {expect,it,vi} from 'vitest';
import {collectRelightCandidates,freezeRelightCandidateQuery,readRelightCandidatePage,type RelightCandidateQuery} from './relight-candidates';
const id='11111111-1111-4111-8111-111111111111',other='22222222-2222-4222-8222-222222222222';
const query:RelightCandidateQuery={snapshot_id:id,base:{asset_version_id:id,source:{artifact_id:id,sha256:'a'.repeat(64)},canvas_hash:'b'.repeat(64)},kind:'depth',after:null};
it('continues an empty filtered page and owns the source binding',async()=>{
 const owned=freezeRelightCandidateQuery(query);expect(owned.base).not.toBe(query.base);
 const fetch=vi.fn().mockResolvedValueOnce({items:[],next_cursor:id}).mockResolvedValueOnce({items:[{ref:{artifact_id:other,sha256:'c'.repeat(64)},kind:'depth'}],next_cursor:null});
 const result=await collectRelightCandidates(fetch,query);expect(result).toHaveLength(1);expect(fetch.mock.calls[1][0].after).toBe(id);
});
it('rejects cursor loops, wrong producer kind and truncated inventories',async()=>{
 expect(()=>readRelightCandidatePage({items:[],next_cursor:id},{...query,after:id})).toThrow();
 expect(()=>readRelightCandidatePage({items:[{ref:{artifact_id:id,sha256:'c'.repeat(64)},kind:'person'}],next_cursor:null},query)).toThrow();
 await expect(collectRelightCandidates(vi.fn().mockResolvedValue({items:[],next_cursor:id}),query,undefined,1)).rejects.toThrow('PAGE_LIMIT');
});
it('does not fetch after cancellation',async()=>{
 const abort=new AbortController();abort.abort();const fetch=vi.fn();await expect(collectRelightCandidates(fetch,query,abort.signal)).rejects.toThrow('CANCELLED');expect(fetch).not.toHaveBeenCalled();
});
