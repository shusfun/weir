// Local build-time oracle only; unchanged pinned upstream generator functions.
// Imported infrastructure isn't required to exercise the single-node mapper.
import fs from 'node:fs';
import vm from 'node:vm';
const input = JSON.parse(fs.readFileSync(0, 'utf8'));
const functions = {surge:'clashProxyToSurgeResult',loon:'clashProxyToLoonResult',quanx:'buildQxLine',egern:'mapProxy'};
const filename=input.target==='egern'?'./template-renderers/render-egern.js':`./builtin-${input.target}-generator.js`;
const source = fs.readFileSync(new URL(filename, import.meta.url),'utf8')
  .replace(/^import[\s\S]*?;\r?\n/gm,'').replace(/^export /gm,'');
const sandbox = {proxy:input.proxy};
vm.runInNewContext(source+`\nresult=${functions[input.target]}(proxy);`,sandbox,{timeout:1000});
process.stdout.write(JSON.stringify(input.target==='egern'?sandbox.result:typeof sandbox.result==='string'?sandbox.result:sandbox.result?.proxyLine??null));
