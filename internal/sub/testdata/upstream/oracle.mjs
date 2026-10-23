// MiSub 对照执行器：读取 Go 测试生成的用例，在 Node 中调用上游原始函数，输出规范化结果。
// 用法：node oracle.mjs <cases.json>，结果以 JSON 数组写到 stdout。
import { readFileSync } from 'node:fs';
import { pathToFileURL } from 'node:url';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));

// 上游运行在 Cloudflare Workers（默认区域 en-US），固定区域避免随本机系统区域变化
const nativeLocaleCompare = String.prototype.localeCompare;
String.prototype.localeCompare = function (that, locales, options) {
    return nativeLocaleCompare.call(this, that, locales ?? 'en-US', options);
};
// 保持 stdout 只输出结果
const toStderr = (...args) => process.stderr.write(args.map(String).join(' ') + '\n');
console.log = toStderr;
console.info = toStderr;
console.debug = () => {};
console.warn = () => {};
console.error = () => {};

function decode(v) {
    if (Array.isArray(v)) return v.map(decode);
    if (v && typeof v === 'object') {
        if (v.$u === 1 && Object.keys(v).length === 1) return undefined;
        if ('$num' in v) return Number(v.$num);
        if ('$url' in v) return new URL(v.$url);
        if ('$usp' in v) return new URLSearchParams(v.$usp);
        if ('$map' in v) return new Map(v.$map.map(([k, val]) => [decode(k), decode(val)]));
        if ('$set' in v) return new Set(decode(v.$set));
        if ('$re' in v) {
            const m = v.$re.match(/^\/(.*)\/([a-z]*)$/s);
            return new RegExp(m[1], m[2]);
        }
        const out = {};
        for (const [k, val] of Object.entries(v)) out[k] = decode(val);
        return out;
    }
    return v;
}

function describe(v) {
    if (v === undefined) return '{"$u":1}';
    if (v === null) return 'null';
    if (typeof v === 'number') {
        if (!Number.isFinite(v)) return `{"$num":"${String(v)}"}`;
        return JSON.stringify(v);
    }
    if (typeof v === 'string' || typeof v === 'boolean') return JSON.stringify(v);
    if (typeof v === 'function') return '{"$fn":1}';
    if (Array.isArray(v)) return '[' + Array.from(v, (x) => describe(x)).join(',') + ']';
    if (v instanceof Map)
        return '{"$map":[' + [...v].map(([k, val]) => '[' + describe(k) + ',' + describe(val) + ']').join(',') + ']}';
    if (v instanceof Set) return '{"$set":' + describe([...v]) + '}';
    if (v instanceof RegExp) return '{"$re":' + JSON.stringify(String(v)) + '}';
    if (v instanceof Date) return '{"$date":"' + v.toISOString() + '"}';
    return (
        '{' +
        Object.keys(v)
            .map((k) => JSON.stringify(k) + ':' + describe(v[k]))
            .join(',') +
        '}'
    );
}

const moduleCache = new Map();
async function load(mod) {
    if (!moduleCache.has(mod)) {
        moduleCache.set(mod, await import(pathToFileURL(join(here, 'functions', mod)).href));
    }
    return moduleCache.get(mod);
}

const cases = JSON.parse(readFileSync(process.argv[2], 'utf8'));
const results = [];
for (const c of cases) {
    try {
        const mod = await load(c.mod);
        const fn = c.prop ? mod[c.fn]?.[c.prop] : mod[c.fn];
        let result;
        const args = decode(c.args || []);
        if (typeof fn === 'function') {
            result = await fn(...args);
        } else {
            result = fn;
        }
        results.push(c.returnArgs ? describe([result, args]) : describe(result));
    } catch (e) {
        results.push(describe({ $throw: String(e && e.message) }));
    }
}
process.stdout.write(JSON.stringify(results));
