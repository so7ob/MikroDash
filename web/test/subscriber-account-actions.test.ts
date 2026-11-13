import assert from 'node:assert/strict';
import { test } from 'node:test';
import { filterSubscriberAccounts, type SubscriberAdminAccount } from '../src/pages/subscriber-account-actions';

test('subscriber filters match original Unicode data literally and preserve the protected source list', () => {
  const accounts: SubscriberAdminAccount[] = [
    { id: 'synthetic-a', username: 'ALICE', name: 'Synthetic سحب <script>', status: 'active', statusRevision:'9007199254740993' },
    { id: 'synthetic-b', username: 'synthetic_100%', name: '  Original Unicode: مشترك  ', status: 'suspended', statusRevision:'0' },
    { id: 'synthetic-c', username: 'synthetic-alice', name: 'Other account', status: 'suspended', statusRevision:'0' },
  ];
  const original = structuredClone(accounts);
  assert.deepEqual(filterSubscriberAccounts(accounts, ' alice ', 'all').map(account => account.id), ['synthetic-a', 'synthetic-c']);
  assert.deepEqual(filterSubscriberAccounts(accounts, 'مشترك', 'suspended'), [accounts[1]]);
  assert.deepEqual(filterSubscriberAccounts(accounts, 'سحب', 'active'), [accounts[0]]);
  assert.deepEqual(filterSubscriberAccounts(accounts, '%', 'all'), [accounts[1]]);
  assert.deepEqual(filterSubscriberAccounts(accounts, '_100', 'all'), [accounts[1]]);
  assert.deepEqual(filterSubscriberAccounts(accounts, 'alice', 'active'), [accounts[0]]);
  assert.deepEqual(filterSubscriberAccounts(accounts, 'مشترك', 'active'), []);
  assert.deepEqual(filterSubscriberAccounts(accounts, '', 'all'), accounts);
  assert.deepEqual(filterSubscriberAccounts(accounts, '<script>', 'all'), [accounts[0]]);
  assert.deepEqual(accounts, original);
});


test('credential review freezes exact account metadata without numeric coercion or fallback',async()=>{
 const {credentialReview}=await import('../src/pages/subscriber-credentials');
 const account={id:'synthetic-account',username:'Synthetic اسم',name:'Original <script>',status:'active',statusRevision:'0',credentialRevision:'9007199254740993'};
 const review=credentialReview(account);account.credentialRevision='9007199254740994';account.username='Changed after review';
 assert.deepEqual(review,{accountId:'synthetic-account',username:'Synthetic اسم',name:'Original <script>',revision:'9007199254740993'});assert.equal(Object.isFrozen(review),true);
 for(const credentialRevision of [undefined,0,null,'-1','1e3','9223372036854775808',''] as unknown[]){assert.throws(()=>credentialReview({...account,credentialRevision} as SubscriberAdminAccount));}
});
