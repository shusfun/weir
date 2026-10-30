// MiSub 4952c16 node-transformer.js, MIT; unchanged comparator and flag helper.
function stripLeadingEmoji(name) {
    return String(name || '')
        .replace(/^[\uD83C][\uDDE6-\uDDFF][\uD83C][\uDDE6-\uDDFF]\s*/g, '')
        .trim();
}

export function makeComparator(sortCfg) {
    const keys = sortCfg.keys || [];
    const nameIgnoreEmoji = sortCfg.nameIgnoreEmoji !== false;

    // 预先构建 customOrder 索引 Map，将 O(n) 查找优化为 O(1)
    const customOrderMaps = keys.map((k) => {
        let orderList = k?.customOrder;
        if (!Array.isArray(orderList) || orderList.length === 0) {
            if (k?.key === 'region') {
                orderList = [
                    '香港',
                    '台湾',
                    '日本',
                    '新加坡',
                    '美国',
                    '韩国',
                    '英国',
                    '德国',
                    '法国',
                    '加拿大',
                ];
            } else if (k?.key === 'protocol') {
                orderList = ['vless', 'trojan', 'vmess', 'hysteria2', 'ss', 'ssr', 'anytls'];
            }
        }
        if (!Array.isArray(orderList)) return null;
        const map = new Map();
        orderList.forEach((v, i) => map.set(String(v), i));
        return map;
    });

    const cmpStr = (a, b) =>
        String(a || '').localeCompare(String(b || ''), undefined, {
            numeric: true,
            sensitivity: 'base',
        });
    const cmpNum = (a, b) => {
        const an = Number(a),
            bn = Number(b);
        if (Number.isNaN(an) && Number.isNaN(bn)) return 0;
        if (Number.isNaN(an)) return 1;
        if (Number.isNaN(bn)) return -1;
        return an - bn;
    };

    /**
     * IP 地址比较器
     * 支持 IPv4 用于数字排序，其他作为字符串排序
     */
    const cmpIp = (a, b) => {
        const ip4Regex = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/;
        const ma = String(a).match(ip4Regex);
        const mb = String(b).match(ip4Regex);

        if (ma && mb) {
            for (let i = 1; i <= 4; i++) {
                const diff = parseInt(ma[i]) - parseInt(mb[i]);
                if (diff !== 0) return diff;
            }
            return 0;
        }
        // 如果其中一个是 IPv4，让 IPv4 排在前面 (可选优化)
        if (ma) return -1;
        if (mb) return 1;

        // 否则按字符串排序
        return cmpStr(a, b);
    };

    return (ra, rb) => {
        for (let i = 0; i < keys.length; i++) {
            const k = keys[i];
            if (!k?.key) continue;
            const key = String(k.key);
            const order = String(k.order || 'asc').toLowerCase() === 'desc' ? -1 : 1;
            const orderMap = customOrderMaps[i];

            let va, vb;
            if (key === 'name') {
                va = nameIgnoreEmoji ? stripLeadingEmoji(ra.name) : ra.name;
                vb = nameIgnoreEmoji ? stripLeadingEmoji(rb.name) : rb.name;
                const r = cmpStr(va, vb);
                if (r !== 0) return r * order;
                continue;
            }
            if (key === 'region') {
                va = ra.regionZh || ra.region;
                vb = rb.regionZh || rb.region;
                if (orderMap) {
                    const ia = orderMap.get(String(va || ''));
                    const ib = orderMap.get(String(vb || ''));
                    const raIdx = ia === undefined ? Number.MAX_SAFE_INTEGER : ia;
                    const rbIdx = ib === undefined ? Number.MAX_SAFE_INTEGER : ib;
                    if (raIdx !== rbIdx) return (raIdx - rbIdx) * order;
                }
                const r = cmpStr(va, vb);
                if (r !== 0) return r * order;
                continue;
            }
            if (key === 'group') {
                const r = cmpStr(ra.group || '', rb.group || '');
                if (r !== 0) return r * order;
                continue;
            }
            if (key === 'port') {
                const r = cmpNum(ra.port, rb.port);
                if (r !== 0) return r * order;
                continue;
            }
            if (key === 'server') {
                const r = cmpIp(ra.server, rb.server);
                if (r !== 0) return r * order;
                continue;
            }

            va = ra[key];
            vb = rb[key];

            if (orderMap) {
                const ia = orderMap.get(String(va || ''));
                const ib = orderMap.get(String(vb || ''));
                const raIdx = ia === undefined ? Number.MAX_SAFE_INTEGER : ia;
                const rbIdx = ib === undefined ? Number.MAX_SAFE_INTEGER : ib;
                if (raIdx !== rbIdx) return (raIdx - rbIdx) * order;
            }

            const r = cmpStr(va, vb);
            if (r !== 0) return r * order;
        }
        return 0;
    };
}

