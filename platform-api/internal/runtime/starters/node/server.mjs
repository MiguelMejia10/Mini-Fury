import http from 'node:http';
import {performance} from 'node:perf_hooks';
export function createServer(){
 const c={epoch:new Date().toISOString(),total:0,errors:0,le100:0,le300:0,le1000:0};
 return http.createServer({maxHeaderSize:16384,requestTimeout:10000,headersTimeout:5000},(req,res)=>{
 const path=new URL(req.url,'http://localhost').pathname;
 if(path==='/health'){res.end('ok');return}
 if(path==='/mini-fury/metrics'){res.setHeader('Content-Type','application/json');res.end(JSON.stringify(c));return}
 const start=performance.now();const status=path==='/error'?503:path==='/'?200:404;
 res.writeHead(status,{'Content-Type':'application/json'});res.end(JSON.stringify({service:'__APP_NAME__',message:process.env.MESSAGE||''}));
 c.total++;if(status>=500)c.errors++;const ms=performance.now()-start;if(ms<=100)c.le100++;if(ms<=300)c.le300++;if(ms<=1000)c.le1000++;
 });
}
if(process.argv[1]===new URL(import.meta.url).pathname){createServer().listen(8080,'0.0.0.0',()=>console.log('mini_fury_application_started'))}
