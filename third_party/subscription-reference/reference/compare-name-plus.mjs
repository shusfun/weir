// Local fixed-source oracle only. Never loaded by the Go application.
import fs from 'node:fs';
import vm from 'node:vm';
const input = JSON.parse(fs.readFileSync(0, 'utf8'));
const source = fs.readFileSync(new URL('./node-cleaner.js', import.meta.url), 'utf8').replace(/^export /gm, '');
const sandbox = {input, URL};
vm.runInNewContext(source + '\nresult=fixNodeUrlEncoding(input.link,{plusAsSpace:input.enabled});', sandbox, {timeout:1000});
process.stdout.write(JSON.stringify(sandbox.result));
