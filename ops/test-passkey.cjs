// Run with: node --test ops/test-passkey.cjs
// Exercise the browser script with local responses; no credential or network access.
const assert = require('node:assert/strict');
const {readFileSync} = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const {test} = require('node:test');
const source = readFileSync(path.join(__dirname, '../static/js/passkey.js'), 'utf8');

function element(text = '') {
  const classes = new Set(), attributes = {};
  return {
    textContent: text, hidden: true, attributes,
    classList: {toggle(name, on) { on ? classes.add(name) : classes.delete(name); }, contains(name) { return classes.has(name); }},
    setAttribute(name, value) { attributes[name] = value; },
    removeAttribute(name) { delete attributes[name]; },
  };
}

const reply = (body, ok = true) => ({ok, json: async () => JSON.parse(body)});
const challenge = () => reply(JSON.stringify({publicKey: {challenge: 'AQID', user: {id: 'BAUG'}, allowCredentials: [{id: 'BAUG', type: 'public-key'}]}}));
const credential = () => ({
  id: 'example-passkey', rawId: new Uint8Array([4, 5, 6]).buffer, type: 'public-key',
  response: {clientDataJSON: new Uint8Array([1]).buffer, authenticatorData: new Uint8Array([2]).buffer, signature: new Uint8Array([3]).buffer},
  getClientExtensionResults: () => ({}),
});

function setup({responses = [], mode = 'login', get = async () => credential(), supported = true} = {}) {
  const feedback = element(), helper = element('Touch ID, Face ID, or a security key');
  const name = {value: 'My laptop'}, requests = [], redirects = [], stored = new Map();
  const container = {querySelector(selector) { return selector === '[data-passkey-name]' ? name : helper; }};
  const button = Object.assign(element(), {
    dataset: {next: '/dashboard?tab=tickets', passkeyFeedback: 'feedback'}, disabled: false,
    parentElement: {parentElement: container},
    addEventListener(event, callback) { this[event] = callback; },
  });
  vm.runInNewContext(source, {
    window: {PublicKeyCredential: supported ? function () {} : undefined, location: {href: 'https://btcpp.dev/login', assign: url => redirects.push(url)}},
    navigator: {credentials: {get, create: get}},
    document: {
      getElementById: id => id === 'feedback' ? feedback : null,
      querySelectorAll: selector => selector === `[data-passkey-${mode}]` ? [button] : [],
    },
    fetch: async (url, options) => {
      requests.push({url, options});
      const response = responses.shift();
      if (response instanceof Error) throw response;
      assert.ok(response, 'unexpected request');
      return response;
    },
    sessionStorage: {setItem: (key, value) => stored.set(key, value)},
    URL, Uint8Array, atob, btoa,
  });
  return {button, feedback, helper, requests, redirects, stored};
}

function assertSeparateError(app, message) {
  assert.match(app.feedback.textContent, message);
  assert.equal(app.feedback.hidden, false);
  assert.equal(app.feedback.attributes.role, 'alert');
  assert.equal(app.feedback.attributes['aria-live'], 'assertive');
  assert.equal(app.feedback.classList.contains('is-error'), true);
  assert.equal(app.helper.textContent, 'Touch ID, Face ID, or a security key');
  assert.equal(app.button.disabled, false);
  assert.equal(app.button.attributes['aria-busy'], undefined);
}

test('plain 404, HTML, malformed JSON and missing challenge show a friendly separate error', async () => {
  for (const response of [reply('404 page not found', false), reply('<html>Bad gateway</html>', false), reply('{'), reply('null'), reply('{}')]) {
    const app = setup({responses: [response], get: () => assert.fail('must not open a credential prompt')});
    await app.button.click();
    assertSeparateError(app, /temporarily unavailable/);
    assert.doesNotMatch(app.feedback.textContent, /JSON|Unexpected|position|<html>/);
  }
});

test('server availability errors are readable and preserve the helper description', async () => {
  const app = setup({responses: [reply('{"error":"Passkeys are unavailable in this preview."}', false)]});
  await app.button.click();
  assertSeparateError(app, /^Passkeys are unavailable in this preview\.$/);
});

test('network failures and unsupported browsers have useful messages', async () => {
  const app = setup({responses: [new TypeError('Failed to fetch')]});
  await app.button.click();
  assertSeparateError(app, /Check your connection/);
  const unsupported = setup({supported: false});
  await unsupported.button.click();
  assertSeparateError(unsupported, /does not support passkeys/);
  assert.equal(unsupported.requests.length, 0);
});

test('browser cancellation clears the busy state and permits retry', async () => {
  let calls = 0;
  const app = setup({responses: [challenge(), challenge(), reply('{"redirect":"/dashboard"}')], get: async () => {
    assert.equal(app.feedback.classList.contains('is-error'), false);
    assert.equal(app.button.attributes['aria-busy'], 'true');
    if (++calls === 1) throw new DOMException('Technical browser detail', 'NotAllowedError');
    return credential();
  }});
  await app.button.click();
  assertSeparateError(app, /cancelled or timed out/);
  await app.button.click();
  assert.deepEqual(app.redirects, ['/dashboard']);
  assert.equal(app.feedback.classList.contains('is-error'), false);
});

test('unrecognized browser exceptions never expose raw implementation details', async () => {
  const app = setup({responses: [challenge()], get: async () => { throw new Error('Unhelpful implementation detail'); }});
  await app.button.click();
  assertSeparateError(app, /^Could not sign in with your passkey\./);
  assert.doesNotMatch(app.feedback.textContent, /implementation detail/);
});

test('non-JSON verification failure is handled after credential selection', async () => {
  const app = setup({responses: [challenge(), reply('<html>Unavailable</html>', false)]});
  await app.button.click();
  assertSeparateError(app, /^Could not verify your passkey\./);
  assert.equal(app.redirects.length, 0);
});

test('successful login retains credential serialization and the server redirect', async () => {
  const app = setup({responses: [challenge(), reply('{"redirect":"/dashboard?tab=tickets"}')], get: async ({publicKey}) => {
    assert.deepEqual(Array.from(publicKey.challenge), [1, 2, 3]);
    assert.deepEqual(Array.from(publicKey.allowCredentials[0].id), [4, 5, 6]);
    return credential();
  }});
  await app.button.click();
  assert.deepEqual(app.redirects, ['/dashboard?tab=tickets']);
  assert.equal(app.requests[1].options.method, 'POST');
  assert.equal(app.requests[1].options.credentials, 'same-origin');
  assert.equal(JSON.parse(app.requests[1].options.body).rawId, 'BAUG');
  assert.equal(app.helper.textContent, 'Touch ID, Face ID, or a security key');
});

test('registration reauthentication still preserves the chosen passkey name', async () => {
  const app = setup({mode: 'register', responses: [reply('{"reauth_url":"/reauth?next=passkey","error":"Sign in again"}', false)]});
  await app.button.click();
  assert.deepEqual(app.redirects, ['/reauth?next=passkey']);
  assert.equal(app.stored.get('btcpp:resume-passkey-name'), 'My laptop');
});
