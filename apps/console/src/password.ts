export function accountPasswordError(password: string): string {
  const characters = Array.from(password).length;
  const bytes = new TextEncoder().encode(password).length;
  if (
    characters < 6 ||
    bytes > 72 ||
    !/[A-Z]/.test(password) ||
    !/[a-z]/.test(password) ||
    !/[0-9]/.test(password)
  ) {
    return "密码至少 6 位，且须同时包含大写字母、小写字母和数字。";
  }
  return "";
}
