import assert from 'node:assert/strict';
import { test } from 'node:test';
import { loginCredentials, voucherCode } from '../src/subscriber-login';

test('voucher authentication uses an empty username and leaves account credentials unchanged', () => {
  assert.deepEqual(loginCredentials('voucher', '  synthetic-voucher  ', 'ignored'), { username: '', password: 'synthetic-voucher' });
  assert.deepEqual(loginCredentials('account', 'اسم <user>', ' pass word '), { username: 'اسم <user>', password: ' pass word ' });
});
test('QR extraction reads one code without navigating or accepting ambiguous credentials', () => {
  assert.equal(voucherCode('  ABC-123  '), 'ABC-123');
  assert.equal(voucherCode('https://example.test/?voucher=ABC%2D123'), 'ABC-123');
  assert.equal(voucherCode('https://example.test/?username=قسيمة'), 'قسيمة');
  for (const raw of ['javascript:alert(1)', 'data:text/html,test', 'https://example.test/', 'https://user:pass@example.test/?code=A', 'https://example.test/?code=A&voucher=B', 'https://example.test/?code=A&code=B', 'a\nb', 'a\x00b', 'x'.repeat(129)]) assert.equal(voucherCode(raw), '', raw);
});
