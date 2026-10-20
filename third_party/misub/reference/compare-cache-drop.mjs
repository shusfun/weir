// Fixed MiSub 4952c16; test-only Node oracle. Production remains Go.
import {isSuspiciousNodeCountDrop} from './node-cache-service.mjs';
let input=''; for await (const chunk of process.stdin) input+=chunk;
console.log(JSON.stringify(JSON.parse(input).map(([a,b])=>isSuspiciousNodeCountDrop(a,b))));
