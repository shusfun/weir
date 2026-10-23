// 对照测试专用：把 JS 内建能力（URL、js-yaml、JSON、编码、正则等）包装成可按 JSON 调用的函数，
// 用来校验 Go 端 jscompat 语义层。这不是上游代码。
import yaml from 'js-yaml';

export function urlParts(input) {
    try {
        const u = new URL(input);
        return {
            href: u.href,
            protocol: u.protocol,
            username: u.username,
            password: u.password,
            hostname: u.hostname,
            port: u.port,
            pathname: u.pathname,
            search: u.search,
            hash: u.hash,
            params: [...u.searchParams],
        };
    } catch {
        return null;
    }
}

export function urlSetHash(input, hash) {
    const u = new URL(input);
    u.hash = hash;
    return u.href;
}

export function urlDeleteParam(input, name) {
    const u = new URL(input);
    u.searchParams.delete(name);
    return u.toString();
}

export function searchParams(init, setName, setValue) {
    const p = new URLSearchParams(init);
    const before = [...p];
    if (setName !== undefined) p.set(setName, setValue);
    return { before, get: p.get('a'), has: p.has('a'), out: p.toString() };
}

export const yamlDump = (v, opts) =>
    yaml.dump(v, { indent: 2, lineWidth: -1, noRefs: true, ...(opts || {}) });

export function yamlLoad(text) {
    try {
        return { ok: yaml.load(text) };
    } catch {
        return { error: true };
    }
}

export const jsonStringify = (v, indent) => JSON.stringify(v, null, indent);

export function jsonParse(s) {
    try {
        return { ok: JSON.parse(s) };
    } catch {
        return { error: true };
    }
}

export function codec(s) {
    const out = { enc: encodeURIComponent(s), encURI: encodeURI(s), esc: escape(s), unesc: unescape(s) };
    try {
        out.dec = decodeURIComponent(s);
    } catch {
        out.dec = null;
    }
    try {
        out.atob = atob(s);
    } catch (e) {
        out.atob = 'ERR:' + e.message;
    }
    out.b64 = btoa(unescape(encodeURIComponent(s)));
    out.trim = s.trim();
    out.upper = s.toUpperCase();
    out.lower = s.toLowerCase();
    out.len = s.length;
    out.pad = s.padStart(12, 'ab');
    out.num = Number(s);
    out.int = parseInt(s);
    out.int16 = parseInt(s, 16);
    out.float = parseFloat(s);
    return out;
}

export const numberToString = (n) => String(n);

export const localeSort = (arr) =>
    [...arr].sort((a, b) => a.localeCompare(b, 'en-US', { numeric: true, sensitivity: 'base' }));

export function regex(s, pattern, flags, repl, limit) {
    let re;
    try {
        re = new RegExp(pattern, flags);
    } catch {
        return { invalid: true };
    }
    const m = s.match(new RegExp(pattern, flags.replace(/g/g, '')));
    return {
        test: new RegExp(pattern, flags).test(s),
        match: m ? { groups: [...m], prefix: s.slice(0, m.index), named: m.groups ? { ...m.groups } : null } : null,
        all: s.match(new RegExp(pattern, flags.includes('g') ? flags : flags + 'g')),
        replace: s.replace(re, repl),
        split: s.split(new RegExp(pattern, flags), limit),
        search: (() => { const i = s.search(new RegExp(pattern, flags)); return i < 0 ? null : s.slice(0, i); })(),
    };
}
