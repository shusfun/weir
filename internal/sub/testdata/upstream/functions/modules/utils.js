// 对照测试用节选：上游 functions/modules/utils.js 依赖存储与配置模块，
// 这里只保留订阅引擎用到的 base64EncodeUtf8（逐字复制自 MiSub 4952c16）。
export function base64EncodeUtf8(str) {
    if (!str) return '';
    try {
        const bytes = new TextEncoder().encode(str);
        const binString = Array.from(bytes, (b) => String.fromCharCode(b)).join('');
        return btoa(binString);
    } catch (e) {
        console.error('[Utils] base64EncodeUtf8 failed:', e);
        return '';
    }
}
