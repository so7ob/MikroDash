// Voucher credentials use MikroDash's empty-username API, never RouterOS CHAP.
export type LoginMode = 'voucher' | 'account';
export function loginCredentials(mode: LoginMode, username: string, password: string): { username: string; password: string } {
  return mode === 'voucher' ? { username: '', password: username.trim() } : { username, password };
}
export function voucherCode(raw: string): string {
  let code = raw.trim();
  if (/^https?:\/\//i.test(code)) {
    try {
      const url = new URL(code);
      if (url.username || url.password) return '';
      const entries = ['voucher', 'code', 'username'].flatMap(key => url.searchParams.getAll(key));
      if (entries.length !== 1) return '';
      code = entries[0]!.trim();
    } catch { return ''; }
  } else if (/^[a-z][a-z0-9+.-]*:/i.test(code)) return '';
  return code && code.length <= 128 && !/[\x00-\x20\x7f]/.test(code) ? code : '';
}
