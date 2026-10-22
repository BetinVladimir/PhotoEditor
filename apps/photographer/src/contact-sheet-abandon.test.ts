import {expect,it,vi} from 'vitest';
import {abandonContactSheet} from './contact-sheet-abandon';
import type {ContactSheetAttempt} from './contact-sheet-attempt-store';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const attempt:ContactSheetAttempt={schema_version:1,collection_id:id(3),key:id(4),request:{selection_snapshot_id:id(5),locale:'en',columns:4,rows:5},ticket:null};
it('uses the immutable admission body and key for either serialized resolution',async()=>{
 const request=vi.fn().mockResolvedValueOnce({status:'abandoned',key:attempt.key,selection_snapshot_id:id(5)}).mockResolvedValueOnce({run_id:id(6),status:'queued'});
 expect(await abandonContactSheet({request} as never,id(2),attempt,new AbortController().signal)).toEqual({status:'abandoned'});
 expect(request.mock.calls[0][1]).toMatchObject({method:'POST',body:attempt.request,idempotencyKey:attempt.key});
 expect(await abandonContactSheet({request} as never,id(2),attempt,new AbortController().signal)).toEqual({status:'accepted',ticket:{run_id:id(6),status:'queued'}});
});
it('refuses substituted snapshot/key and malformed accepted receipts',async()=>{
 for(const raw of [{status:'abandoned',key:id(7),selection_snapshot_id:id(5)},{status:'abandoned',key:attempt.key,selection_snapshot_id:id(8)},{run_id:id(6),status:'succeeded'},{run_id:id(6),status:'queued',secret:'unexpected'}]){
  await expect(abandonContactSheet({request:vi.fn().mockResolvedValue(raw)} as never,id(2),attempt,new AbortController().signal)).rejects.toThrow();
 }
});
it('does not call transport after cancellation',async()=>{const request=vi.fn(),controller=new AbortController();controller.abort();await expect(abandonContactSheet({request} as never,id(2),attempt,controller.signal)).rejects.toThrow();expect(request).not.toHaveBeenCalled();});
