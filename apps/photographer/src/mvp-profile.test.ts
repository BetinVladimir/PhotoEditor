import {describe,expect,it} from 'vitest';
import {isDeferredInMvp,mvpProfileEnabled} from './mvp-profile';

describe('MVP profile',()=>{
  it('defers interchange, signing and style training',()=>{
    for(const p of ['/interchange/new','/ru/interchange/prepared/1','/en/exports/sign','/bg/styles/abc/training','/interchange'])
      expect(isDeferredInMvp(p,true)).toBe(true);
  });
  it('keeps core routes',()=>{
    for(const p of ['/','/en/login','/ru/projects/1','/en/styles','/en/styles/abc','/en/exports','/bg/deliveries/new','/en/editor/1'])
      expect(isDeferredInMvp(p,true)).toBe(false);
  });
  it('full profile enables everything',()=>{
    expect(isDeferredInMvp('/en/interchange/new',false)).toBe(false);
    expect(mvpProfileEnabled('full')).toBe(false);
    expect(mvpProfileEnabled(undefined)).toBe(true);
  });
});
