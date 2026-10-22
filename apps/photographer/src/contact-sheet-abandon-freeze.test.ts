import {expect,it,vi} from 'vitest';
import {abandonContactSheet} from './contact-sheet-abandon';
import type {ContactSheetAttempt} from './contact-sheet-attempt-store';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const pending=():ContactSheetAttempt=>({schema_version:1,collection_id:id(3),key:id(4),request:{selection_snapshot_id:id(5),locale:'en',columns:4,rows:5},ticket:null});
it('validates against the original identity after the caller mutates its attempt',async()=>{
 const attempt=pending();let finish!:(value:unknown)=>void;
 const request=vi.fn((_path:string,_options:unknown)=>new Promise(resolve=>{finish=resolve;}));
 const result=abandonContactSheet({request} as never,id(2),attempt,new AbortController().signal);
 attempt.key=id(8);attempt.request.selection_snapshot_id=id(9);attempt.request.locale='ru';
 expect(request.mock.calls[0][1]).toMatchObject({idempotencyKey:id(4),body:{selection_snapshot_id:id(5),locale:'en'}});
 finish({status:'abandoned',key:id(4),selection_snapshot_id:id(5)});
 await expect(result).resolves.toEqual({status:'abandoned'});
});
it('does not let an abandoned or substituted receipt erase known acceptance',async()=>{
 const attempt={...pending(),ticket:{run_id:id(6)}};
 for(const response of [{status:'abandoned',key:id(4),selection_snapshot_id:id(5)},{run_id:id(7),status:'queued'}]){
  await expect(abandonContactSheet({request:vi.fn().mockResolvedValue(response)} as never,id(2),attempt,new AbortController().signal)).rejects.toThrow('CONTACT_SHEET_RESOLUTION_INVALID');
 }
});
it('refuses invalid scope before transport',async()=>{
 const request=vi.fn();
 await expect(abandonContactSheet({request} as never,'../other-tenant',pending(),new AbortController().signal)).rejects.toThrow('CONTACT_SHEET_RESOLUTION_INVALID');
 expect(request).not.toHaveBeenCalled();
});
