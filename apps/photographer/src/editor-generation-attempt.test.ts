import {it,expect,vi} from 'vitest';
import {EditorGenerationAttempt,readEditorGenerationAttempt} from './editor-generation-attempt';
import type {EditorDraft,MaskRef} from './catalog-types';
const draft=()=>({asset_version_id:'source',revision:'9',params:{},source_sha256:'a'.repeat(64),mask_refs:[{protected:true,artifact:{artifact_id:'guard',sha256:'b'.repeat(64)}}]}) as EditorDraft;
const mask=()=>({artifact:{artifact_id:'mask',sha256:'c'.repeat(64)},canvas:{width:10,height:10},transform_chain:{},selector:'object',confidence:1,protected:false}) as MaskRef;
it('owns frozen saved draft, mask and key across lost-response retry',async()=>{
 const saved=draft(),selected=mask(),attempt=new EditorGenerationAttempt(saved,selected,'chosen prompt',7);
 const send=vi.fn().mockRejectedValueOnce(new Error('lost')).mockResolvedValueOnce({run_id:'33333333-3333-4333-8333-333333333333',status:'queued'});
 saved.revision='20';selected.artifact.artifact_id='changed';
 await expect(attempt.submit(send)).rejects.toThrow('lost');await attempt.submit(send);
 expect(send.mock.calls[0]).toEqual(send.mock.calls[1]);expect(send.mock.calls[0][0].revision).toBe('9');expect(send.mock.calls[0][1].artifact.artifact_id).toBe('mask');expect(send.mock.calls[0].slice(2)).toEqual(['chosen prompt',7,1,attempt.key]);
});
it('does not expose internal body to a transport that mutates its arguments',async()=>{
 const attempt=new EditorGenerationAttempt(draft(),mask(),'chosen',1);let count=0;
 const send=vi.fn(async(saved:EditorDraft,selected:MaskRef)=>{expect(saved.revision).toBe('9');expect(selected.artifact.artifact_id).toBe('mask');saved.revision='100';selected.artifact.artifact_id='mutated';if(count++===0)throw new Error('lost');return{run_id:'33333333-3333-4333-8333-333333333333',status:'queued'}});
 await expect(attempt.submit(send)).rejects.toThrow();await attempt.submit(send);
});
it('shares one pending transport and retains accepted run for output-read retries',async()=>{
 const attempt=new EditorGenerationAttempt(draft(),mask(),'chosen',1);let finish!:(value:{run_id:string;status:string})=>void;
 const send=vi.fn(()=>new Promise<{run_id:string;status:string}>(resolve=>{finish=resolve}));
 const first=attempt.submit(send),second=attempt.submit(send);expect(send).toHaveBeenCalledOnce();finish({run_id:'33333333-3333-4333-8333-333333333333',status:'queued'});
 const receipt=await first;await second;receipt.run_id='mutated';expect(await attempt.submit(send)).toEqual({run_id:'33333333-3333-4333-8333-333333333333',status:'queued'});expect(send).toHaveBeenCalledOnce();
});
it('retains original key after malformed acceptance instead of allowing a new operation',async()=>{
 const attempt=new EditorGenerationAttempt(draft(),mask(),'chosen',1),send=vi.fn().mockResolvedValueOnce({run_id:'',status:'queued'}).mockResolvedValueOnce({run_id:'33333333-3333-4333-8333-333333333333',status:'queued'});
 await expect(attempt.submit(send)).rejects.toThrow('GENERATION_RECEIPT_INVALID');await attempt.submit(send);expect(send.mock.calls[0][5]).toBe(send.mock.calls[1][5]);
});

function durable(){
 const saved=draft(),selected=mask();saved.asset_version_id='11111111-1111-4111-8111-111111111111';selected.artifact.artifact_id='22222222-2222-4222-8222-222222222222';saved.mask_refs=[];
 return new EditorGenerationAttempt(saved,selected,'frozen',7);
}
it('restores uncertain generation with original key and frozen body',async()=>{
 const initial=durable(),expected=initial.snapshot;
 const restored=EditorGenerationAttempt.restore(JSON.stringify(expected)),send=vi.fn().mockResolvedValue({run_id:'33333333-3333-4333-8333-333333333333',status:'queued'});
 await restored.submit(send);expect(send.mock.calls[0]).toEqual([expected.input.draft,expected.input.mask,'frozen',7,1,expected.key]);
});
it('restores accepted run without invoking generation transport',async()=>{
 const initial=durable(),receipt={run_id:'33333333-3333-4333-8333-333333333333',status:'queued'};
 await initial.submit(vi.fn().mockResolvedValue(receipt));
 const restored=EditorGenerationAttempt.restore(JSON.stringify(initial.snapshot)),send=vi.fn();expect(await restored.submit(send)).toEqual(receipt);expect(send).not.toHaveBeenCalled();
});
it('snapshot copies cannot mutate private attempt or decoded restored state',()=>{
 const initial=durable(),snapshot=initial.snapshot,raw=JSON.stringify(snapshot);snapshot.input.mask.artifact.artifact_id='changed';
 const read=readEditorGenerationAttempt(raw);read.input.draft.revision='100';
 expect(initial.snapshot.input.mask.artifact.artifact_id).not.toBe('changed');expect(EditorGenerationAttempt.restore(raw).snapshot.input.draft.revision).toBe('9');
});
it.each(['version','key','revision','source','mask','prompt','seed','receipt'])('refuses corrupted durable %s instead of manufacturing a fresh attempt',field=>{
 const record=durable().snapshot;
 switch(field){
 case 'version':(record as unknown as {version:number}).version=2;break;
 case 'key':record.key='invalid';break;
 case 'revision':record.input.draft.revision='9007199254740992';break;
 case 'source':record.input.draft.asset_version_id='invalid';break;
 case 'mask':record.input.mask.artifact.sha256='invalid';break;
 case 'prompt':record.input.prompt=' ';break;
 case 'seed':record.input.seed=-1;break;
 case 'receipt':record.receipt={run_id:'invalid',status:'queued'};break;
 }
 expect(()=>EditorGenerationAttempt.restore(JSON.stringify(record))).toThrow('GENERATION_JOURNAL_INVALID');
});

it.each([{run_id:'not-a-uuid',status:'queued'},{run_id:'33333333-3333-4333-8333-333333333333',status:'BAD'},{run_id:'33333333-3333-4333-8333-333333333333',status:'queued',unexpected:true}])('does not cache a receipt that durable recovery would reject: %j',async invalid=>{
 const attempt=durable(),send=vi.fn().mockResolvedValueOnce(invalid).mockResolvedValueOnce({run_id:'33333333-3333-4333-8333-333333333333',status:'queued'});
 await expect(attempt.submit(send)).rejects.toThrow('GENERATION_RECEIPT_INVALID');expect(attempt.accepted).toBeNull();
 await attempt.submit(send);expect(send.mock.calls[0][5]).toBe(send.mock.calls[1][5]);expect(()=>readEditorGenerationAttempt(JSON.stringify(attempt.snapshot))).not.toThrow();
});
