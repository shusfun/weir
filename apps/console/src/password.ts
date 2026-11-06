export function accountPasswordError(password: string): string {
  const characters = Array.from(password).length;
  const bytes = new TextEncoder().encode(password).length;
  if (characters < 6 || bytes > 72) {
    return "密码至少 6 个字符，且不能超过 72 字节。";
  }
  return "";
}
