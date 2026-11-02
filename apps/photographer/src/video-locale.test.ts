import {readFileSync} from 'node:fs';
import {expect,it} from 'vitest';
const langs=['en','ru','bg'] as const;
const load=(l:string):Record<string,unknown>=>JSON.parse(readFileSync(new URL(`../../../locales/${l}/common.json`,import.meta.url),'utf8'));
const videoKeys=(l:string)=>Object.keys(load(l)).filter(k=>k.startsWith('video_')).sort();
it('video_ key sets match across en/ru/bg and values are non-empty strings',()=>{
 const en=videoKeys('en');expect(en.length).toBeGreaterThan(0);
 for(const l of langs){const d=load(l);expect(videoKeys(l)).toEqual(en);for(const k of en)expect(typeof d[k]==='string'&&(d[k] as string).trim().length>0,`${l}:${k}`).toBe(true);}
});
it('has a video_error_ key for every DS-24 code',()=>{
 const doc=readFileSync(new URL('../../../docs/video/13-data-structures.md',import.meta.url),'utf8');
 const sec=doc.split('## DS-24')[1].split('\n## ')[0].split('\n### ')[0];
 const codes=[...sec.matchAll(/^\| `([A-Z_]+)` \|/gm)].map(m=>m[1]);
 expect(codes.length).toBe(31);
 const en=load('en');for(const c of codes)expect(en[`video_error_${c.toLowerCase()}`],c).toBeTruthy();
});
