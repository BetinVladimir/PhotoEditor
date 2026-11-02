import {readFileSync} from "node:fs";
import {createHash} from "node:crypto";
import {expect,it} from "vitest";
import {canonicalize,documentSha256,sha256Hex} from "./video-timeline";
type Case={input:unknown;canonical:string;sha256:string};
const cases:Case[]=JSON.parse(readFileSync(new URL("../../../docs/video/vectors/canonical.json",import.meta.url),"utf8"));
it("canonical bytes and sha256 match the shared vector",()=>{
 expect(cases.length).toBe(2);
 for(const c of cases){expect(new TextDecoder().decode(canonicalize(c.input))).toBe(c.canonical);expect(documentSha256(c.input)).toBe(c.sha256);}
});
it("sha256Hex agrees with node:crypto across padding boundaries",()=>{
 for(const n of [0,1,55,56,57,63,64,65,119,120,1000]){const b=Uint8Array.from({length:n},(_,i)=>(i*7+3)&255);expect(sha256Hex(b)).toBe(createHash("sha256").update(b).digest("hex"));}
});
