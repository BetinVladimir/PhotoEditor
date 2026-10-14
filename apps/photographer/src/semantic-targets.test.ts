import {it,expect} from 'vitest';
import type {SemanticTargetResource} from '@photoeditor/api-client';
import {mergeSemanticTargets} from './semantic-targets';
it('overlapping pages cannot replace a newer target with stale revision above safe integer range',()=>{
 const newer={id:'target',revision:'9007199254740993'} as SemanticTargetResource;
 const stale={id:'target',revision:'9007199254740992'} as SemanticTargetResource;
 expect(mergeSemanticTargets([newer],[stale])).toEqual([newer]);
 const next={id:'other',revision:'1'} as SemanticTargetResource;
 expect(mergeSemanticTargets([newer],[stale,next])).toEqual([newer,next]);
});

it('supports exactly5000 distinct targets including overlapping updates and refuses overflow',()=>{const full=Array.from({length:5000},(_,i)=>({id:String(i),revision:'1'} as SemanticTargetResource));expect(mergeSemanticTargets([],full)).toHaveLength(5000);expect(mergeSemanticTargets(full,[{id:'0',revision:'2'} as SemanticTargetResource])[0].revision).toBe('2');expect(()=>mergeSemanticTargets(full,[{id:'new',revision:'1'} as SemanticTargetResource])).toThrow('LIMIT');expect(()=>mergeSemanticTargets([],full.concat(full[0]))).toThrow('LIMIT');});
