import { tg, state, byId, uuid, node, el, notice, api, show, highlightTab, setAction, setBack, actionButton, field, submitForm, submitButton,
  fold, confirmSheet, textSheet, openTelegramLink, policyLinks, date, stars } from './shared.js';
import { describeMembership } from './member_status.js';
import { loadMyMemberships } from './subscriber_home.js';

function renderDiscovery() {
  const tools = byId('community-tools');
  tools.replaceChildren();
  if (state.communities.length > 5) {
    const search = node('input', 'input search'); search.type = 'search'; search.placeholder = 'Search communities';
    search.setAttribute('aria-label', 'Search communities');
    search.addEventListener('input', () => drawCommunities(search.value.trim().toLowerCase()));
    tools.append(search);
  }
  drawCommunities('');
}

function drawCommunities(query) {
  const list = byId('community-list');
  list.replaceChildren();
  if (!state.communities.length) {
    list.append(el('div', 'empty', node('strong', '', 'No communities yet'), node('span', '', 'New communities appear here as soon as they open. Please check back soon.')));
    return;
  }
  const matches = state.communities.filter((item) => !query || `${item.title} ${item.seller} ${item.description}`.toLowerCase().includes(query));
  if (!matches.length) list.append(node('div', 'empty', `Nothing matches “${query}”.`));
  for (const community of matches) {
    const card = node('button', 'community-card'); card.type = 'button';
    card.addEventListener('click', () => selectCommunity(community.id, 'discover'));
    card.append(node('span', 'avatar', (community.title || 'M').slice(0, 1).toUpperCase()),
      el('span', 'grow', node('strong', '', community.title), node('span', 'by', `By ${community.seller}`),
        community.description ? node('span', 'desc', community.description) : null,
        node('span', 'price', `${stars(community.price_stars)} / month`)));
    list.append(card);
  }
}

async function selectCommunity(id, from = state.view) {
  if (!uuid.test(id)) return;
  Object.assign(state, { selected: id, profile: null, plans: [], selectedPlan: null, membership: null, checkout: null, returnTo: from === 'membership' ? 'membership' : 'discover' });
  show('membership');
  highlightTab(state.returnTo);
  byId('membership-head').hidden = true;
  byId('membership-content').replaceChildren(node('p', 'muted', 'Loading community…'));
  drawBack();
  try {
    state.profile = await api(`/api/v1/communities/${encodeURIComponent(id)}`);
    state.plans = (await api(`/api/v1/communities/${encodeURIComponent(id)}/plans`)).plans || [];
    state.selectedPlan = state.plans[0]?.id || null;
    if (state.token) {
      if (!state.notificationPrefs) state.notificationPrefs = await api('/api/v1/me/notification-preferences').catch(() => null);
      await Promise.all([loadMembership(), loadCheckout()]);
    }
    renderMembership();
  } catch (error) {
    byId('membership-content').replaceChildren(node('div', 'empty', error.message));
  }
}

function leaveCommunity() {
  Object.assign(state, { selected: null, profile: null, membership: null, checkout: null });
  if (state.returnTo === 'membership') { show('membership'); loadMyMemberships(selectCommunity); }
  else show('discover');
}

function drawBack() {
  const label = state.returnTo === 'membership' ? 'My memberships' : 'Explore';
  byId('membership-back').replaceChildren(setBack(leaveCommunity, label));
}

async function loadMembership() {
  try { state.membership = await api(`/api/v1/me/membership?community_id=${encodeURIComponent(state.selected)}`); }
  catch (error) { notice(error.message, true); }
}

async function loadCheckout() {
  try { state.checkout = await api(`/api/v1/me/checkout-status?community_id=${encodeURIComponent(state.selected)}`); }
  catch { state.checkout = null; }
}

async function refreshMembership() {
  if (!state.selected || !state.token) return;
  await Promise.all([loadMembership(), loadCheckout()]);
  renderMembership();
}

function renderMembership() {
  const profile = state.profile;
  if (!state.selected || !profile) { loadMyMemberships(selectCommunity); return; }
  byId('membership-head').hidden = true;
  drawBack();
  const status = describeMembership(profile, state.membership);
  const content = byId('membership-content');
  content.replaceChildren(communityHeader(profile));
  if (status.title) content.append(statusCard(status));
  const unfinished = status.action === 'subscribe' && state.checkout?.status === 'pending' && state.checkout.invoice_url;
  if (unfinished) {
    content.append(el('div', 'card tone-warn', node('h2', '', 'You started paying'), node('p', '', 'Your invoice is still open. Finish paying, or choose a plan below to start again.'),
      actionButton('Finish paying', 'button secondary full', () => openInvoice(state.checkout.invoice_url))));
  }
  if (status.action === 'subscribe') renderPlanChoice(content, profile);
  renderDetails(content, profile, status);
  if (state.token) content.append(supportForm());
  // Cancelling stays last on the page, away from the main action.
  if (state.token && profile.mode === 'managed_bot' && state.membership?.subscription_id) renderManage(content);
  setAction(mainAction(profile, status));
}

function communityHeader(profile) {
  return el('div', 'stack',
    el('div', 'community-header', node('span', 'avatar large', (profile.title || 'M').slice(0, 1).toUpperCase()),
      el('div', 'grow', node('h1', '', profile.title), node('span', 'muted', `By ${profile.seller}${profile.category ? ` · ${profile.category}` : ''}`))),
    profile.description ? node('p', 'lead', profile.description) : null);
}

function statusCard(status) {
  const card = el('div', `card tone-${status.tone}`, node('h2', '', status.title), node('p', '', status.detail));
  if (status.canRecheck) card.append(actionButton('I already joined', 'text-button', refreshMembership));
  return card;
}

function renderPlanChoice(content, profile) {
  if (profile.mode !== 'managed_bot') return;
  if (!state.plans.length) {
    content.append(node('div', 'empty', "This community isn't taking new members right now. Please check back soon."));
    return;
  }
  content.append(el('div', 'section-title', node('h2', '', state.plans.length > 1 ? 'Choose a plan' : 'Your plan')));
  const options = node('div', 'stack');
  for (const offer of state.plans) {
    const radio = node('input'); radio.type = 'radio'; radio.name = 'plan'; radio.value = offer.id;
    radio.checked = offer.id === state.selectedPlan;
    radio.addEventListener('change', () => { state.selectedPlan = offer.id; setAction(mainAction(profile, describeMembership(profile, state.membership))); });
    const amount = el('span', 'amount', stars(offer.price_stars), node('small', '', ' / month'));
    options.append(el('label', 'option', radio, el('span', 'grow', el('span', 'head', node('span', '', offer.name), amount),
      offer.description ? node('p', '', offer.description) : null, offer.benefits ? node('p', '', offer.benefits) : null)));
  }
  content.append(options);
  const links = policyLinks(profile.policies);
  if (links) content.append(el('div', 'stack', node('p', 'muted', 'By paying you agree to these terms:'), links));
}

function renderDetails(content, profile, status) {
  const isMember = status.action === 'open' || status.action === 'join';
  if (profile.content_guide) content.append(fold('Where to begin', isMember, node('p', '', profile.content_guide)));
  if (profile.rules) content.append(fold('Community rules', false, node('p', '', profile.rules)));
  const chosen = state.plans.find((item) => item.id === state.selectedPlan);
  const kv = (label, value) => el('div', 'kv', node('span', '', label), node('span', '', value));
  content.append(fold('Billing and support', false,
    el('div', 'group inset', kv('Sold by', profile.seller), kv('Support', profile.support_contact),
      kv('Billing', 'Every 30 days, in Telegram Stars')),
    profile.mode === 'native_channel'
      ? node('p', 'muted', 'Telegram bills this channel directly. To stop renewing, open Telegram Settings, then My Stars, and cancel the subscription there. You keep access until the paid period ends.')
      : null,
    chosen?.terms ? node('p', '', chosen.terms) : null, policyLinks(profile.policies)));
}

function renderManage(content) {
  const membership = state.membership;
  const group = node('div', 'group');
  if (state.notificationPrefs) {
    const reminder = node('input'); reminder.type = 'checkbox'; reminder.checked = state.notificationPrefs.reminders_enabled;
    reminder.addEventListener('change', async () => {
      try {
        state.notificationPrefs = await api('/api/v1/me/notification-preferences', { method: 'PATCH', body: JSON.stringify({ reminders_enabled: reminder.checked }) });
        notice(reminder.checked ? "We'll remind you before your access ends." : 'Reminders are off.');
      } catch (error) { reminder.checked = !reminder.checked; notice(error.message, true); }
    });
    group.append(el('label', 'toggle', node('span', '', 'Remind me before my access ends'), reminder));
  }
  const history = node('div', 'group');
  history.hidden = true;
  const loadHistory = async () => {
    const data = await api(`/api/v1/me/payments?community_id=${encodeURIComponent(state.selected)}`);
    drawPayments(history, data.payments, loadHistory);
  };
  const historyRow = actionButton('', 'list-row', loadHistory);
  historyRow.append(node('span', 'grow', 'Payments and refunds'), node('span', 'chevron', '›'));
  group.append(historyRow);
  content.append(el('div', 'section-title', node('h2', '', 'Manage')), group, history);
  const renewing = membership.renewing_subscriptions || [];
  if (renewing.length > 1) {
    content.append(node('div', 'card tone-warn', `You have ${renewing.length} renewals running for this community. Turn off the ones you don't need, and ask for a refund of any extra payment under Payments and refunds.`));
  }
  renewing.forEach((subscription, index) => {
    const label = renewing.length > 1 ? `Cancel renewal ${index + 1} (paid until ${date(subscription.paid_through)})` : 'Cancel renewal';
    content.append(actionButton(label, 'button danger full', () => cancelRenewal(subscription)));
  });
}

function drawPayments(container, payments, reload) {
  container.hidden = false;
  container.replaceChildren();
  if (!payments.length) { container.append(el('div', 'list-row', node('span', 'sub', 'No payments yet.'))); return; }
  for (const payment of payments) {
    const row = el('div', 'list-row', el('span', 'grow', node('span', 'title', stars(payment.amount_stars)), node('span', 'sub', date(payment.paid_at))));
    if (payment.refunded_at) row.append(node('span', 'pill', 'Refunded'));
    else if (payment.refund_requested) row.append(node('span', 'pill warn', 'Refund requested'));
    else {
      row.append(actionButton('Ask for a refund', 'button ghost small', async () => {
        const reason = await textSheet({ title: 'Ask for a refund', label: 'Why are you asking for a refund?',
          body: `For your ${stars(payment.amount_stars)} payment on ${date(payment.paid_at)}. If it's approved, you'll get a message and your Stars back.`,
          confirm: 'Send request' });
        if (!reason) return;
        await api('/api/v1/me/refund-requests', { method: 'POST', body: JSON.stringify({ payment_id: payment.id, reason }) });
        notice('Refund request sent.');
        await reload();
      }));
    }
    container.append(row);
  }
}

function supportForm() {
  const form = node('form', 'form');
  const kind = node('select', 'input'); kind.name = 'kind'; kind.setAttribute('aria-label', 'What do you need help with?');
  for (const [value, label] of [['access', "I can't get into the community"], ['payment', 'I have a question about a payment'], ['refund', 'I want a refund'], ['other', 'Something else']]) {
    const option = node('option', '', label); option.value = value; kind.append(option);
  }
  form.append(kind, field('Tell us what happened', 'description', { type: 'textarea', minLength: 10, maxLength: 2000, placeholder: 'Include the date and what you expected to happen.' }),
    submitButton('Send to support', 'button secondary full'));
  submitForm(form, async (values) => {
    await api('/api/v1/me/support-tickets', { method: 'POST', body: JSON.stringify({ community_id: state.selected, ...values }) });
    form.reset();
    notice('Thanks. Your message was sent to the community team.');
  });
  return fold('Need help?', false, form);
}

function mainAction(profile, status) {
  if (!state.token) {
    return { label: 'Open in Telegram to join', disabled: true, onClick: () => {}, note: 'Joining needs your Telegram account.' };
  }
  if (status.action === 'join' || status.action === 'open') {
    return { label: status.action === 'join' ? 'Join the community' : 'Open the community', onClick: () => openTelegramLink(state.membership.join_link) };
  }
  if (status.action !== 'subscribe') return null;
  if (profile.mode !== 'managed_bot') {
    return { label: `Join on Telegram for ${stars(profile.price_stars)}`, note: 'Renews every 30 days. Cancel anytime in Telegram.', onClick: () => openTelegramLink(profile.subscription_link) };
  }
  const plan = state.plans.find((item) => item.id === state.selectedPlan);
  if (!plan) return null;
  return { label: `Join for ${stars(plan.price_stars)}`, note: 'Renews every 30 days. Cancel anytime.', onClick: beginCheckout };
}

async function requestMessagePermission() {
  // Without this permission Telegram blocks payment and access messages.
  if (tg?.requestWriteAccess && !tg.initDataUnsafe?.user?.allows_write_to_pm) {
    await new Promise((resolve) => tg.requestWriteAccess(resolve));
  }
}

async function beginCheckout() {
  if (!state.token || !state.selected || !state.selectedPlan) return;
  await requestMessagePermission();
  const checkout = await api('/api/v1/checkouts', { method: 'POST', body: JSON.stringify({ community_id: state.selected, plan_id: state.selectedPlan }) });
  state.checkout = { ...checkout, status: 'pending' };
  openInvoice(checkout.invoice_url);
}

function openInvoice(link) {
  if (!tg?.openInvoice) { openTelegramLink(link); return; }
  tg.openInvoice(link, async (outcome) => {
    if (outcome === 'cancelled') { notice("Payment cancelled. You haven't been charged."); await refreshMembership(); return; }
    if (outcome === 'failed') { notice('The payment did not go through. Please try again.', true); await refreshMembership(); return; }
    notice('Confirming your payment…');
    for (let attempt = 0; attempt < 8; attempt += 1) {
      await new Promise((resolve) => setTimeout(resolve, 2000));
      await refreshMembership();
      if (state.membership?.entitled) {
        tg.HapticFeedback?.notificationOccurred?.('success');
        notice("Payment confirmed. You're almost in.");
        return;
      }
    }
    notice('Telegram is still confirming your payment. Check back in a moment.');
  });
}

async function cancelRenewal(subscription) {
  const confirmed = await confirmSheet({ title: 'Cancel your renewal?',
    body: `You'll keep access until ${date(subscription.paid_through)}, and you won't be charged again.`,
    confirm: 'Cancel renewal', cancel: 'Keep my membership', danger: true });
  if (!confirmed) return;
  await api(`/api/v1/me/subscriptions/${encodeURIComponent(subscription.id)}/cancel`, { method: 'POST' });
  notice(`Renewal cancelled. You keep access until ${date(subscription.paid_through)}.`);
  await refreshMembership();
}

export { renderDiscovery, selectCommunity, refreshMembership, renderMembership };
