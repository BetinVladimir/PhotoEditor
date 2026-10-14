import {expect,it} from 'vitest';
import {toggleSemanticDecision,type SemanticChoices} from './semantic-decision-selection';
it('bounds queued selections to100 while preserving existing decisions',()=>{
 let choices:SemanticChoices={};for(let i=0;i<101;i++)choices=toggleSemanticDecision(choices,String(i),'approve');
 expect(Object.keys(choices)).toHaveLength(100);expect(choices['100']).toBeUndefined();
 const original=choices;choices=toggleSemanticDecision(choices,'0','reject');expect(choices['0']).toBe('reject');expect(original['0']).toBe('approve');expect(Object.keys(choices)).toHaveLength(100);
 choices=toggleSemanticDecision(choices,'0','reject');expect(Object.keys(choices)).toHaveLength(99);choices=toggleSemanticDecision(choices,'100','approve');expect(choices['100']).toBe('approve');expect(Object.keys(choices)).toHaveLength(100);
});
