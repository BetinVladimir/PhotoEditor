// @vitest-environment jsdom
import {expect,it,vi} from 'vitest';
vi.mock('react-native',()=>({Platform:{OS:'web'},Share:{share:vi.fn()}}));
import {privateShareURL} from './private-share-link';
const id='00000000-0000-4000-8000-000000000001',secret='A'.repeat(43);
it('puts the secret exclusively in an explicit trusted HTTPS portal fragment',()=>{
 const url=new URL(privateShareURL('https://portal.example','ru',id,secret));expect(url.origin).toBe('https://portal.example');expect(url.hash).toContain(secret);expect(url.search).toBe('');expect(url.pathname).not.toContain(secret);
});
it('refuses unsafe portal origins, malformed resource and secret inputs',()=>{
 for(const origin of ['http://portal.example','https://user:pass@portal.example','https://portal.example/path','https://portal.example?x=1','https://portal.example#x'])expect(()=>privateShareURL(origin,'en',id,secret)).toThrow();expect(()=>privateShareURL('https://portal.example','en','wrong',secret)).toThrow();expect(()=>privateShareURL('https://portal.example','en',id,'short')).toThrow();
});
