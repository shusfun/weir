import { readFileSync } from 'node:fs';
import { evaluateDslExpression, renderDslTemplate } from './expression-dsl.mjs';
const samples=JSON.parse(readFileSync(process.argv[2],'utf8'));
for(const sample of samples){const result=sample.template?renderDslTemplate(sample.template,sample.context):evaluateDslExpression(sample.expression,sample.context);if(result!==sample.expected)throw Error(JSON.stringify({sample,result}));}
console.log(samples.length+' upstream DSL examples agree');
