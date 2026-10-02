import {describe,it,expect} from 'vitest';
import {matchCommand,type CommandKeyEvent} from './commands';
const key:CommandKeyEvent={key:'s',ctrlKey:true,metaKey:false,shiftKey:false,altKey:false,repeat:false,isComposing:false,defaultPrevented:false};
describe('shared command dispatch',()=>{
 it('recognizes save and refresh on Windows and macOS',()=>{
  expect(matchCommand(key)).toBe('capture.save');
  expect(matchCommand({...key,key:'R',ctrlKey:false,metaKey:true,shiftKey:true})).toBe('capture.refresh');
 });
 it('does not dispatch unmodified keys, repeats, composition or handled events',()=>{
  for(const override of [{ctrlKey:false},{repeat:true},{isComposing:true},{defaultPrevented:true},{altKey:true},{shiftKey:true}])
   expect(matchCommand({...key,...override})).toBeUndefined();
 });
});

it('comparison shortcuts do not intercept editing or repeat/composition',()=>{
 const event={key:'ArrowRight',ctrlKey:true,metaKey:false,altKey:false,shiftKey:false,repeat:false,isComposing:false,defaultPrevented:false};
 expect(matchCommand(event)).toBe('compare.next');expect(matchCommand({...event,key:'ArrowLeft'})).toBe('compare.previous');expect(matchCommand({...event,key:'Enter'})).toBe('compare.accept');expect(matchCommand({...event,key:'Backspace'})).toBe('compare.reject');
 expect(matchCommand({...event,editing:true})).toBeUndefined();expect(matchCommand({...event,repeat:true})).toBeUndefined();expect(matchCommand({...event,isComposing:true})).toBeUndefined();expect(matchCommand({...event,key:'s',editing:true})).toBe('capture.save');
});
