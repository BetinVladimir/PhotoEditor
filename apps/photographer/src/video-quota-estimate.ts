const GIB=1024n**3n,MIB=1024n**2n,TPS=705_600_000n;
function safe(v:bigint):number{
 if(v>BigInt(Number.MAX_SAFE_INTEGER))throw new Error('ESTIMATE_OVERFLOW');
 return Number(v);
}
const big=(n:number)=>{if(!Number.isSafeInteger(n)||n<0)throw new Error('ESTIMATE_INVALID');return BigInt(n);};
/** FN-Q-03: min(bytes*15/100, 4 GiB) + 128 MiB. */
export function derivativeEstimate(bytes:number):number{
 const e=big(bytes)*15n/100n;
 return safe((e<4n*GIB?e:4n*GIB)+128n*MIB);
}
/** FN-Q-04: ceil(durationTicks*(maxrate+audio)*105 / (8*100*TPS)) + 64 MiB. */
export function renderEstimate(durationTicks:number,maxrateBps:number,audioBps:number):number{
 const den=8n*100n*TPS,num=big(durationTicks)*(big(maxrateBps)+big(audioBps))*105n;
 return safe((num+den-1n)/den+64n*MIB);
}
