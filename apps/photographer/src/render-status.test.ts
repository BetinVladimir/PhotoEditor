import {it,expect} from 'vitest';
import {readRenderRunStatus,renderStatusLabel,advanceRenderRunStatus} from './render-status';
const id='11111111-1111-1111-1111-111111111111';
const wire=()=>({id,kind:'bulk',status:'running',revision:'2',counts:{total:3,succeeded:1,failed:0,skipped:0,review:0},stage:'running',progress:1/3,errors:[]});
it('accepts current parent progress and detaches transport data',()=>{
 const value=wire(),result=readRenderRunStatus(value,id);value.counts.succeeded=3;
 expect(result.counts.succeeded).toBe(1);expect(renderStatusLabel(result)).toBe('Processing (33%)');
});
it('rejects another run, child kind and malformed revision/counts/progress',()=>{
 for(const bad of [{...wire(),id:'22222222-2222-2222-2222-222222222222'},{...wire(),kind:'v2'},{...wire(),revision:'01'},{...wire(),progress:NaN},{...wire(),counts:{...wire().counts,succeeded:4}},{...wire(),runtime:'cuda'}])expect(()=>readRenderRunStatus(bad,id)).toThrow('RENDER_STATUS_INVALID');
});

it('keeps the latest observation including revisions above JS safe integer',()=>{
 const current=readRenderRunStatus({...wire(),revision:'9007199254740993',status:'completed',progress:1},id);
 const older={...wire(),revision:'9007199254740992'};
 expect(advanceRenderRunStatus(current,older,id).status).toBe('completed');
 expect(advanceRenderRunStatus(current,{...older,revision:current.revision},id).status).toBe('completed');
 const newer=advanceRenderRunStatus(current,{...wire(),revision:'9007199254740994'},id);
 expect(newer.revision).toBe('9007199254740994');
 expect(advanceRenderRunStatus(null,wire(),id).revision).toBe('2');
 expect(()=>advanceRenderRunStatus(current,{...wire(),id:'22222222-2222-2222-2222-222222222222'},id)).toThrow('RENDER_STATUS_INVALID');
});

it('validates independent child presentation counters without changing the run revision',()=>{
 const presentation={total:3,pending:1,ready:1,failed:1,cancelled:0,unavailable:0};
 const child={...wire(),kind:'v2',presentation};
 expect(readRenderRunStatus(child,id,'v2').presentation).toEqual(presentation);
 for(const bad of [{...presentation,pending:-1},{...presentation,total:4},{...presentation,ready:1.5},{...presentation,extra:0}])expect(()=>readRenderRunStatus({...child,presentation:bad},id,'v2')).toThrow('RENDER_STATUS_INVALID');
 expect(()=>readRenderRunStatus({...wire(),presentation},id)).toThrow('RENDER_STATUS_INVALID');
});
