// Behaviour cases adapted from retained MiGate 0998448 topologyGraph.test.ts.
// Transpile only these pure modules; no production Node.js runtime is introduced.
const fs=require('fs'),path=require('path'),assert=require('node:assert/strict');
const ts=require('../apps/console/node_modules/typescript');
const root=path.join(__dirname,'../apps/console/src/lib');
const moduleURL=source=>'data:text/javascript;base64,'+Buffer.from(ts.transpile(source,{module:ts.ModuleKind.ESNext,target:ts.ScriptTarget.ES2022})).toString('base64');
(async()=>{
 const model=moduleURL(fs.readFileSync(path.join(root,'topology-model.ts'),'utf8'));
 const graphURL=moduleURL(fs.readFileSync(path.join(root,'topology-graph.ts'),'utf8').replace(/['"]\.\/topology-model['"]/g,JSON.stringify(model)));
 const {buildTopologyGraph,buildInboundTagLookup}=await import(graphURL);
 const viewURL=moduleURL(fs.readFileSync(path.join(root,'topology-view.ts'),'utf8'));const {topologyView}=await import(viewURL);
 const ins=[{id:7,remark:'edge',protocol:'vless',enabled:true,clients:[{id:11,email:'synthetic',enabled:true}]}];
 const outs=[{id:0,tag:'direct',protocol:'freedom',enabled:true},{id:2,tag:'deny',protocol:'blackhole',enabled:true}];
 let g=buildTopologyGraph(ins,outs,[]);
 assert.equal(buildInboundTagLookup(ins).get('inbound-7-vless').id,7);
 assert.equal(g.nodes.find(n=>n.id==='client:11').data.meta.find(m=>m.label==='上行').value,'未知');
 assert.ok(g.edges.some(e=>e.data.kind==='client-inherits'));
 assert.ok(g.edges.some(e=>e.data.kind==='default-direct'));
 g=buildTopologyGraph(ins,outs,[{id:8,inbound_id:7,client_id:11,outbound_id:2,enabled:true}]);
 const rule=g.edges.find(e=>e.data.ruleId===8);assert.equal(rule.source,'client:11');assert.equal(rule.target,'outbound:deny');assert.equal(rule.data.enabled,true);
 assert.equal(rule.animated,false,'configuration must not pretend observed flowing traffic');
 assert.ok(g.edges.some(e=>e.source==='client:11'&&e.data.kind==='default-direct'),'conditional route still has non-match fallback');
 g=buildTopologyGraph(ins,outs,[{id:8,inbound_id:7,client_id:11,outbound_id:2,enabled:false}]);
 assert.equal(g.edges.find(e=>e.data.ruleId===8).data.enabled,false);
 g=buildTopologyGraph(ins,outs,[{id:8,inbound_id:7,client_id:999,outbound_id:99,enabled:true}]);
 assert.ok(g.nodes.some(n=>n.data.kind==='missing-client'));assert.ok(g.nodes.some(n=>n.data.kind==='missing-outbound'));
 assert.equal(g.edges.find(e=>e.data.ruleId===8).data.enabled,false);
 g=buildTopologyGraph(ins,[...outs,{id:3,tag:'sing-only',protocol:'tuic',enabled:true}],[{id:9,inbound_id:7,outbound_id:3,enabled:true}]);
 assert.equal(g.edges.find(e=>e.data.ruleId===9).data.invalidTarget,true);
 assert.equal(g.edges.find(e=>e.data.ruleId===9).data.enabled,false);
 const many=[{...ins[0],clients:Array.from({length:42},(_,i)=>({id:i+1,email:'synthetic-'+i,enabled:true}))}];
 const full=buildTopologyGraph(many,outs,[{id:20,inbound_id:7,client_id:2,outbound_id:2,enabled:true}]);
 const collapsed=topologyView(full,false,'');assert.equal(collapsed.nodes.filter(n=>n.data.kind==='client').length,2);
 assert.ok(collapsed.nodes.some(n=>n.id==='client:2'));assert.equal(collapsed.nodes.find(n=>n.id==='client-summary:inbound:7').data.meta[0].value,'41');
 assert.equal(topologyView(full,true,'').nodes.length,full.nodes.length);assert.equal(full.nodes.length,45,'view must not mutate original graph');
 const searched=topologyView(full,false,'synthetic-40');assert.equal(searched.nodes.filter(n=>n.data.kind==='client').length,1);assert.ok(searched.nodes.some(n=>n.id==='client:41'));
 for(const e of collapsed.edges){assert.ok(collapsed.nodes.some(n=>n.id===e.source));assert.ok(collapsed.nodes.some(n=>n.id===e.target));}
 console.log('Retained topology relationships: exact tags, client routes, disabled state, missing targets, core mismatch, fallback and unknown traffic passed');
})().catch(e=>{console.error(e.message.slice(0,300));process.exitCode=1;});
