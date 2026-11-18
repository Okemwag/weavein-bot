const tg = window.Telegram?.WebApp;
const state = { token: '', communities: [], workspaces: [], memberships: [], activeWorkspaceId: null, activeCommunityId: null,
  creatorCommunities: [], selected: null, selectedPlan: null, plans: [], profile: null, membership: null, checkout: null,
  notificationPrefs: null, view: 'discover', studioTab: 'home', addingCommunity: false, returnTo: 'discover' };
const byId = (id) => document.getElementById(id);
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

function node(tag, className, value) {
  const element = document.createElement(tag);
  if (className) element.className = className;
  if (value !== undefined && value !== null) element.textContent = String(value);
  return element;
}

// el builds an element from text and child nodes, skipping empty values.
function el(tag, className, ...children) {
  const element = node(tag, className);
  element.append(...children.filter((child) => child !== null && child !== undefined && child !== false && child !== ''));
  return element;
}

let noticeTimer;
function notice(message, error = false) {
  const box = byId('notice');
  clearTimeout(noticeTimer);
  box.textContent = message;
  box.classList.toggle('error', error);
  box.hidden = !message;
  if (message) noticeTimer = setTimeout(() => { box.hidden = true; }, error ? 8000 : 4000);
  if (message && error) tg?.HapticFeedback?.notificationOccurred?.('error');
}

// The server's text is shown only when it was written for people.
function friendlyError(status, text) {
  if (status === 401) return 'Your session timed out. Close MemberOS and open it again from Telegram.';
  if (status === 429) return 'That was a lot of tries. Please wait a minute and try again.';
  if (status === 403 && (!text || text === 'forbidden')) return "You don't have permission to do that.";
  if (status === 404 && (!text || /not found$/i.test(text))) return "We couldn't find that. It may have been removed.";
  if (/^invalid\b|\binvalid$/i.test(text)) return 'Please check what you entered and try again.';
  if (!text || text.length > 240 || /database|storage|unavailable$/i.test(text)) {
    return 'Something went wrong on our side. Please try again in a moment.';
  }
  return text;
}

async function api(path, options = {}) {
  const headers = { Accept: 'application/json', ...(options.headers || {}) };
  if (state.token) headers.Authorization = `Bearer ${state.token}`;
  if (options.body) headers['Content-Type'] = 'application/json';
  let response;
  try { response = await fetch(path, { ...options, headers, cache: 'no-store' }); }
  catch { throw new Error("We can't reach MemberOS right now. Check your connection and try again."); }
  if (!response.ok) throw new Error(friendlyError(response.status, (await response.text()).trim()));
  return response.json();
}

const views = ['discover', 'membership', 'studio', 'operations'];

// highlightTab marks the section a person is in, which can differ from the view:
// a community opened from Explore still belongs to Explore.
function highlightTab(active) {
  for (const name of views) {
    const tab = byId(`tab-${name}`);
    tab.classList.toggle('active', name === active);
    if (name === active) tab.setAttribute('aria-current', 'page');
    else tab.removeAttribute('aria-current');
  }
}

function show(view) {
  state.view = view;
  for (const name of views) byId(name).hidden = name !== view;
  highlightTab(view);
  setAction(null);
  setBack(null);
  window.scrollTo({ top: 0, behavior: 'auto' });
}

// setAction places a screen's one main action in the sticky bar.
function setAction(action) {
  const bar = byId('action-bar');
  bar.replaceChildren();
  bar.hidden = !action;
  document.body.classList.toggle('has-action', !!action);
  if (!action) return;
  const button = actionButton(action.label, `button full ${action.style || ''}`, action.onClick);
  button.disabled = !!action.disabled;
  bar.append(el('div', 'action-bar-inner', button, action.note ? node('div', 'note', action.note) : null));
}

let backHandler = null;
const onTelegramBack = () => backHandler?.();
tg?.BackButton?.onClick?.(onTelegramBack);

// setBack uses Telegram's own back button and returns an in-page one for other browsers.
function setBack(handler, label = 'Back') {
  backHandler = handler;
  if (handler) tg?.BackButton?.show?.(); else tg?.BackButton?.hide?.();
  if (!handler) return null;
  const button = node('button', 'text-button back-button', `‹ ${label}`);
  button.type = 'button';
  button.addEventListener('click', handler);
  return button;
}

// actionButton runs an async action once, and shows any failure to the person.
function actionButton(label, className, action) {
  const button = node('button', className, label);
  button.type = 'button';
  button.addEventListener('click', async () => {
    if (button.disabled) return;
    button.disabled = true;
    try { await action(); }
    catch (error) { notice(error.message, true); }
    finally { button.disabled = false; }
  });
  return button;
}

function field(label, name, options = {}) {
  const { type = 'text', required = true, value = '', hint, placeholder, min, max, minLength, maxLength } = options;
  const input = node(type === 'textarea' ? 'textarea' : 'input', 'input');
  if (type !== 'textarea') input.type = type;
  input.name = name; input.id = `field-${name}-${Math.random().toString(36).slice(2, 8)}`;
  input.required = required;
  input.value = value ?? '';
  if (placeholder) input.placeholder = placeholder;
  if (min !== undefined) input.min = min;
  if (max !== undefined) input.max = max;
  if (minLength !== undefined) input.minLength = minLength;
  if (maxLength !== undefined) input.maxLength = maxLength;
  const wrapper = el('label', 'field', el('span', '', label, required ? '' : ' (optional)'), input, hint ? node('span', 'hint', hint) : null);
  wrapper.htmlFor = input.id;
  return wrapper;
}

// submitForm wires a form to an async handler that receives its values.
function submitForm(form, handler) {
  form.addEventListener('submit', async (event) => {
    event.preventDefault();
    const submit = form.querySelector('button[type="submit"]');
    if (submit?.disabled) return;
    if (submit) submit.disabled = true;
    try { await handler(Object.fromEntries(new FormData(form))); }
    catch (error) { notice(error.message, true); }
    finally { if (submit) submit.disabled = false; }
  });
}

function submitButton(label, className = 'button full') {
  const button = node('button', className, label);
  button.type = 'submit';
  return button;
}

function fold(title, open, ...content) {
  const details = el('details', 'fold', node('summary', '', title), el('div', 'fold-body', ...content));
  details.open = !!open;
  return details;
}

function openSheet(build) {
  const dialog = node('dialog', 'sheet');
  document.body.append(dialog);
  return new Promise((resolve) => {
    const finish = (value) => { dialog.close(); dialog.remove(); resolve(value); };
    dialog.addEventListener('cancel', (event) => { event.preventDefault(); finish(null); });
    dialog.append(build(finish));
    dialog.showModal();
  });
}

// confirmSheet replaces window.confirm, which is unreliable inside Telegram.
function confirmSheet({ title, body, confirm, cancel = 'Not now', danger = false }) {
  return openSheet((finish) => {
    const yes = node('button', `button full ${danger ? 'danger' : ''}`, confirm); yes.type = 'button';
    yes.addEventListener('click', () => finish(true));
    const no = node('button', 'button ghost full', cancel); no.type = 'button';
    no.addEventListener('click', () => finish(false));
    // A stray tap on Enter must never trigger something destructive.
    if (danger) no.autofocus = true;
    return el('div', 'sheet-body', node('h2', '', title), body ? node('p', '', body) : null, el('div', 'sheet-actions', yes, no));
  });
}

// textSheet replaces window.prompt and returns the text, or null when cancelled.
function textSheet({ title, body, label, placeholder, confirm, minLength = 10, maxLength = 1000 }) {
  return openSheet((finish) => {
    const form = el('form', 'sheet-body');
    const input = field(label, 'answer', { type: 'textarea', minLength, maxLength, placeholder,
      hint: `Please write at least ${minLength} characters.` });
    const no = node('button', 'button ghost full', 'Cancel'); no.type = 'button';
    no.addEventListener('click', () => finish(null));
    form.append(node('h2', '', title), body ? node('p', '', body) : '', input, el('div', 'sheet-actions', submitButton(confirm), no));
    form.addEventListener('submit', (event) => {
      event.preventDefault();
      finish(input.querySelector('textarea').value.trim());
    });
    return form;
  });
}

function safeTelegramURL(value) {
  try {
    const parsed = new URL(value);
    return parsed.protocol === 'https:' && ['t.me', 'telegram.me'].includes(parsed.hostname) ? parsed.href : null;
  } catch { return null; }
}

function openTelegramLink(value) {
  const link = safeTelegramURL(value);
  if (!link) { notice("That link doesn't look right. Please contact the community's support.", true); return; }
  if (tg?.openTelegramLink) tg.openTelegramLink(link);
  else window.location.assign(link);
}

function date(value) {
  if (!value) return 'unknown date';
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? 'unknown date' : new Intl.DateTimeFormat(undefined, { dateStyle: 'medium' }).format(parsed);
}

const stars = (amount) => `${Number(amount).toLocaleString()} ${Number(amount) === 1 ? 'Star' : 'Stars'}`;

function memberStatusLabel(status) {
  return ({ member: 'In the community', restricted: 'In the community', administrator: 'Admin', creator: 'Owner',
    left: 'Not in the community', kicked: 'Removed', active: 'Active', cancel_scheduled: 'Ending soon',
    payment_failed: 'Renewal failed', expired: 'Ended', refunded: 'Refunded', no_subscription: 'Not subscribed',
    unknown: 'Checking' })[status] || 'Checking';
}

function statusPill(status) {
  const tone = { active: 'good', member: 'good', administrator: 'good', creator: 'good', restricted: 'good',
    cancel_scheduled: 'warn', payment_failed: 'warn', kicked: 'bad' }[status] || '';
  return node('span', `pill ${tone}`, memberStatusLabel(status));
}

export { tg, state, byId, uuid, node, el, notice, api, show, highlightTab, setAction, setBack, actionButton, field, submitForm, submitButton,
  fold, confirmSheet, textSheet, openTelegramLink, date, stars, memberStatusLabel, statusPill };
