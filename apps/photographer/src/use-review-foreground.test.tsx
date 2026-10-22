// @vitest-environment jsdom
import {act,cleanup,renderHook} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
const state=vi.hoisted(()=>({listener:null as null|((value:string)=>void),remove:vi.fn()}));
vi.mock('react-native',()=>({AppState:{currentState:'active',addEventListener:(_name:string,listener:(value:string)=>void)=>{state.listener=listener;return {remove:state.remove};}}}));
import {useReviewForeground} from './use-review-foreground';
afterEach(()=>{cleanup();vi.clearAllMocks();});
it('invalidates mounted review authority on inactive/background and renews generation on foreground',()=>{
 const {result,unmount}=renderHook(useReviewForeground);
 expect(result.current).toEqual({active:true,generation:0});
 act(()=>state.listener?.('inactive'));expect(result.current.active).toBe(false);
 act(()=>state.listener?.('background'));expect(result.current.active).toBe(false);
 act(()=>state.listener?.('active'));expect(result.current).toEqual({active:true,generation:1});
 act(()=>state.listener?.('active'));expect(result.current.generation).toBe(1);
 unmount();expect(state.remove).toHaveBeenCalledOnce();
});
