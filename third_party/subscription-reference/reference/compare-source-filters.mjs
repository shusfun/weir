// Local fixed-source oracle; never included as an application runtime.
import fs from 'node:fs';
import vm from 'node:vm';
const input=JSON.parse(fs.readFileSync(0,'utf8'));
const source=fs.readFileSync(new URL('./node-cleaner.js',import.meta.url),'utf8').replace(/^export /gm,'');
const sandbox={input,console:{warn(){}}};
vm.runInNewContext(source+'\nresult=applyFilterRules(input.links,{filterInclude:input.include,filterExclude:input.exclude});',sandbox,{timeout:1000});
process.stdout.write(JSON.stringify(sandbox.result));
