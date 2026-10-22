import {expect,it,vi} from 'vitest';
import type {OwnerAPI} from './owner-api';
import type {TrainingAttemptStore,TrainingRecovery} from './training-attempt-store';
import {resumeTraining} from './training-recovery';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
it.each([id(2),'not-a-style'])('refuses a mismatched route style %s before reading or producing work',async style=>{
 const state:TrainingRecovery={schema_version:1,style_id:id(1),set_id:null,last_run_id:null,pending:{key:id(3),command:{kind:'create',body:{}},ticket:null}};
 const read=vi.fn(),mutate=vi.fn(),trainingSet=vi.fn();
 await expect(resumeTraining({mutate,trainingSet} as unknown as OwnerAPI,style,{read} as unknown as TrainingAttemptStore,state,new AbortController().signal,()=>true)).rejects.toThrow('TRAINING_SCOPE_INVALID');
 expect(read).not.toHaveBeenCalled();expect(mutate).not.toHaveBeenCalled();expect(trainingSet).not.toHaveBeenCalled();
});
it('an obsolete UI scope cannot read or replay a pending training command',async()=>{
 const read=vi.fn(),mutate=vi.fn();
 const state:TrainingRecovery={schema_version:1,style_id:id(1),set_id:null,last_run_id:null,pending:{key:id(3),command:{kind:'create',body:{}},ticket:null}};
 await expect(resumeTraining({mutate} as unknown as OwnerAPI,id(1),{read} as unknown as TrainingAttemptStore,state,new AbortController().signal,()=>false)).rejects.toMatchObject({name:'AbortError'});
 expect(read).not.toHaveBeenCalled();expect(mutate).not.toHaveBeenCalled();
});

it('refuses a different durable style even when the pending key and command match',async()=>{
 const state:TrainingRecovery={schema_version:1,style_id:id(1),set_id:null,last_run_id:null,pending:{key:id(3),command:{kind:'create',body:{}},ticket:null}};
 const read=vi.fn().mockResolvedValue({...state,style_id:id(2)}),mutate=vi.fn(),trainingSet=vi.fn();
 await expect(resumeTraining({mutate,trainingSet} as unknown as OwnerAPI,id(1),{read} as unknown as TrainingAttemptStore,state,new AbortController().signal,()=>true)).rejects.toThrow('TRAINING_SCOPE_INVALID');
 expect(read).toHaveBeenCalledOnce();expect(mutate).not.toHaveBeenCalled();expect(trainingSet).not.toHaveBeenCalled();
});
