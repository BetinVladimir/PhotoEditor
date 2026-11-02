import {readFileSync} from 'node:fs';
export function loadVector<T>(name:string):T{
 if(!/^[a-z0-9_]+$/.test(name))throw new Error(`bad vector name: ${name}`);
 return JSON.parse(readFileSync(new URL(`../../../docs/video/vectors/${name}.json`,import.meta.url),'utf8')) as T;
}
