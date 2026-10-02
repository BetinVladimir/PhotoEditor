// At most one refresh runs; any burst during it requests one trailing refresh.
export class CoalescedRefresh {
 private running=false;
 private pending=false;
 constructor(private work:()=>Promise<void>,private signal:AbortSignal){}
 async request():Promise<void>{
  if(this.signal.aborted)return;
  this.pending=true;
  if(this.running)return;
  this.running=true;
  try{
   while(this.pending&&!this.signal.aborted){this.pending=false;await this.work()}
  }finally{this.running=false}
 }
}
