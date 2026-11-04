const tg = window.Telegram?.WebApp;
const state = { token: '', communities: [], workspaces: [], activeWorkspaceId: null, activeCommunityId: null,
  creatorCommunities: [], selected: null, selectedPlan: null, plans: [], profile: null, membership: null, checkout: null, notificationPrefs: null, view: 'discover' };
const byId = (id) => document.getElementById(id);
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

function node(tag, className, value) {
  const element = document.createElement(tag);
  if (className) element.className = className;
  if (value !== undefined && value !== null) element.textContent = String(value);
  return element;
}

function notice(message, error = false) {
  const box = byId('notice');
  box.textContent = message;
  box.classList.toggle('error', error);
  box.hidden = !message;
}

async function api(path, options = {}) {
  const headers = { Accept: 'application/json', ...(options.headers || {}) };
  if (state.token) headers.Authorization = `Bearer ${state.token}`;
  if (options.body) headers['Content-Type'] = 'application/json';
  const response = await fetch(path, { ...options, headers, cache: 'no-store' });
  if (!response.ok) {
    if (response.status === 401) throw new Error('Your Telegram session expired. Reopen MemberOS from Telegram.');
    const reason = (await response.text()).trim();
    throw new Error(reason && reason.length < 240 ? reason : `Request failed (${response.status}). Please try again.`);
  }
  return response.json();
}

function show(view) {
  state.view = view;
  for (const name of ['discover', 'membership', 'studio', 'operations']) {
    byId(name).hidden = name !== view;
    const tab = byId(`tab-${name}`);
    tab.classList.toggle('active', name === view);
    if (name === view) tab.setAttribute('aria-current', 'page');
    else tab.removeAttribute('aria-current');
  }
  window.scrollTo({ top: 0, behavior: 'auto' });
}

function safeTelegramURL(value) {
  try {
    const parsed = new URL(value);
    return parsed.protocol === 'https:' && ['t.me', 'telegram.me'].includes(parsed.hostname) ? parsed.href : null;
  } catch { return null; }
}

function openTelegramLink(value) {
  const link = safeTelegramURL(value);
  if (!link) { notice('Telegram returned an invalid link. Contact support.', true); return; }
  if (tg?.openTelegramLink) tg.openTelegramLink(link);
  else window.location.assign(link);
}

function date(value) {
  if (!value) return 'Unknown';
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? 'Unknown' : new Intl.DateTimeFormat(undefined, { dateStyle: 'medium' }).format(parsed);
}

function displayStatus(status) {
  return ({ member: 'Access granted', administrator: 'Access granted', creator: 'Access granted',
    left: 'Not in community', kicked: 'Access removed', active: 'Active', cancel_scheduled: 'Ending at paid-through date',
    payment_failed: 'Renewal needs attention', expired: 'Expired', refunded: 'Refunded',
    no_subscription: 'No subscription', unknown: 'Checking access' })[status] || status || 'Unknown';
}


export { tg, state, byId, uuid, node, notice, api, show, openTelegramLink, date, displayStatus };
