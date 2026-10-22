import {it,expect} from 'vitest';
import {readGenerationResolution} from './editor-generation-resolution';
const key='11111111-1111-4111-8111-111111111111',run='22222222-2222-4222-8222-222222222222';
it('binds abandonment to the original key',()=>{expect(readGenerationResolution({status:'abandoned',key},key)).toEqual({kind:'abandoned',receipt:{status:'abandoned',key}})});
it('preserves an already accepted run',()=>{expect(readGenerationResolution({status:'queued',run_id:run},key)).toEqual({kind:'accepted',receipt:{status:'queued',run_id:run}})});
it.each([null,{}, {status:'abandoned',key:'other'},{status:'abandoned',key,extra:true},{status:'queued',run_id:'invalid'}])('refuses ambiguous resolution %j',value=>{expect(()=>readGenerationResolution(value,key)).toThrow('GENERATION_RESOLUTION_INVALID')});
