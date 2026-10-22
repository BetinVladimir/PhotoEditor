import {describe,it,expect} from 'vitest';
import {generationResultState,GenerationTerminalError} from './editor-generation-result';
describe('generation result authority',()=>{
 it.each(['completed','waiting_review','completed_with_errors'])('reads available outputs for %s',status=>{expect(generationResultState(status)).toBe('outputs')});
 it.each(['failed','cancelled'])('requires explicit acknowledgement for %s',status=>{expect(generationResultState(status)).toBe('terminal')});
 it.each(['draft','queued','running','cancelling'])('keeps the accepted attempt for %s',status=>{expect(generationResultState(status)).toBe('pending')});
 it('does not treat an unknown status as terminal evidence',()=>{expect(()=>generationResultState('future_status')).toThrow('GENERATION_RUN_STATE_INVALID')});
 it('retains the authoritative terminal status',()=>{expect(new GenerationTerminalError('cancelled').status).toBe('cancelled')});
});
