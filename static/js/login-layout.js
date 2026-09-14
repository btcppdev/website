// Progressive enhancement: without JavaScript, email-link and password forms
// remain ordinary server forms, including their original CSRF and return fields.
(() => {
  const emailForm = document.getElementById('auth-email-start');
  const email = document.getElementById('auth-email');
  const next = document.getElementById('auth-email-continue');
  const options = document.getElementById('auth-email-options');
  const passwordForm = document.getElementById('auth-password-form');
  if (!emailForm || !email || !next || !options || !passwordForm) return;
  options.hidden = true;
  const passwordEmail = passwordForm.elements.namedItem('Email');
  passwordEmail.closest('label').hidden = true;
  passwordEmail.required = false;
  const syncEmail = () => { passwordEmail.value = email.value; };
  email.addEventListener('input', syncEmail);
  passwordForm.addEventListener('submit', (event) => {
    syncEmail();
    if (!email.reportValidity()) event.preventDefault();
  });
  emailForm.addEventListener('submit', (event) => {
    if (!options.hidden) return;
    event.preventDefault();
    syncEmail();
    options.hidden = false;
    next.hidden = true;
    options.querySelector('button').focus();
  });

  const method = document.getElementById('login').dataset.preferredMethod;
  const labels = {email_link: 'an email link', password: 'a password', passkey: 'a passkey', nostr: 'Nostr', github: 'GitHub', discord: 'Discord', gitlab: 'GitLab', mlh: 'MLH'};
  if (!Object.hasOwn(labels, method)) return;
  const standard = document.getElementById('auth-standard');
  const preferred = document.getElementById('auth-preferred');
  const slot = document.getElementById('auth-preferred-slot');
  const restores = [];
  const move = (node) => {
    const marker = document.createComment('original login method position');
    node.before(marker);
    restores.push(() => marker.replaceWith(node));
    slot.append(node);
  };
  if (method === 'email_link' || method === 'password') {
    move(emailForm);
    options.hidden = false;
    if (method === 'password') {
      move(options);
      options.querySelector('details').open = true;
      next.hidden = true;
    } else {
      next.textContent = 'Email me a sign-in link →';
    }
  } else {
    const selector = method === 'passkey' ? '[data-passkey-login]' : method === 'nostr' ? '[data-nostr-login]' : `[data-login-provider="${method}"]`;
    const control = standard.querySelector(selector);
    // A formerly used provider may no longer be configured. Show normal choices.
    if (!control) return;
    move(control);
  }
  document.getElementById('auth-preferred-label').textContent = labels[method];
  standard.hidden = true;
  preferred.hidden = false;
  document.getElementById('auth-use-another').addEventListener('click', () => {
    restores.forEach(restore => restore());
    preferred.hidden = true;
    standard.hidden = false;
    options.hidden = true;
    options.querySelector('details').open = false;
    next.textContent = 'Continue with email →';
    next.hidden = false;
    email.focus();
  });
})();
