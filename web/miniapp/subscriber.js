import { tg, state, byId, uuid, node, notice, api, show, openTelegramLink, date, displayStatus } from './shared.js';

function renderDiscovery() {
  const list = byId('community-list');
  list.replaceChildren();
  byId('community-count').textContent = `${state.communities.length} available`;
  if (!state.communities.length) {
    list.append(node('div', 'empty', 'No communities have been published yet. Check back soon.'));
    return;
  }
  for (const community of state.communities) {
    const card = node('article', 'card');
    const top = node('div', 'card-top');
    const copy = node('div');
    copy.append(node('div', 'eyebrow', community.seller), node('h3', '', community.title), node('p', '', community.description || 'A private member community.'));
    top.append(copy, node('div', 'avatar', (community.title || 'M').slice(0, 1).toUpperCase()));
    const footer = node('div', 'card-footer');
    const price = node('div', 'price', `${community.price_stars} Stars`);
    price.append(node('small', '', ' / 30 days'));
    const button = node('button', 'button secondary', 'Explore');
    button.type = 'button';
    button.addEventListener('click', () => selectCommunity(community.id));
    footer.append(price, button);
    card.append(top, footer);
    list.append(card);
  }
}

async function selectCommunity(id) {
  if (!uuid.test(id)) return;
  state.selected = id;
  state.profile = null;
  state.plans = [];
  state.selectedPlan = null;
  state.membership = null;
  show('membership');
  byId('membership-title').textContent = 'Loading community…';
  byId('membership-description').textContent = '';
  byId('membership-content').replaceChildren();
  try {
    state.profile = await api(`/api/v1/communities/${encodeURIComponent(id)}`);
    state.plans = (await api(`/api/v1/communities/${encodeURIComponent(id)}/plans`)).plans || [];
    state.selectedPlan = state.plans[0]?.id || null;
    if (state.token) await refreshMembership();
    else renderMembership();
  } catch (error) { notice(error.message, true); }
}

async function refreshMembership() {
  if (!state.selected || !state.token) return;
  try {
    state.membership = await api(`/api/v1/me/membership?community_id=${encodeURIComponent(state.selected)}`);
    renderMembership();
  } catch (error) { notice(error.message, true); renderMembership(); }
}

function row(label, value) {
  const outer = node('div', 'row');
  outer.append(node('span', 'muted', label), node('span', 'value', value));
  return outer;
}

function renderMembership() {
  const profile = state.profile;
  if (!profile) return;
  byId('membership-title').textContent = profile.title;
  byId('membership-description').textContent = profile.description || `A private community by ${profile.seller}.`;
  const content = byId('membership-content');
  content.replaceChildren();

  for (const offer of state.plans) {
    const plan = node('div', 'detail-card');
    plan.append(node('div', 'label', offer.name));
    const price = node('div', 'value', `${offer.price_stars} Stars`);
    price.append(node('small', '', ' / 30 days, recurring'));
    plan.append(price);
    if (offer.description) plan.append(node('p', '', offer.description));
    if (offer.benefits) plan.append(row('Includes', offer.benefits));
    plan.append(row('Seller', profile.seller), row('Support', profile.support_contact), node('p', '', offer.terms));
    if (profile.mode === 'managed_bot' && state.plans.length > 1) {
      const choose = node('button', `button ${offer.id === state.selectedPlan ? '' : 'secondary'}`, offer.id === state.selectedPlan ? 'Selected plan' : 'Choose this plan');
      choose.type = 'button';
      choose.addEventListener('click', () => { state.selectedPlan = offer.id; renderMembership(); });
      plan.append(choose);
    }
    content.append(plan);
  }
  if (!state.plans.length) content.append(node('div', 'empty', 'No membership plan is available for new subscriptions.'));

  const membership = state.membership;
  const status = node('div', 'detail-card');
  status.append(node('div', 'label', 'Your membership'));
  status.append(node('span', `badge ${membership?.billing_status === 'payment_failed' ? 'warn' : ''}`,
    membership ? displayStatus(profile.mode === 'managed_bot' ? membership.billing_status : membership.access_status) : 'Open in Telegram to view status'));
  if (membership) {
    status.append(row('Telegram access', displayStatus(membership.access_status)));
    if (profile.mode === 'managed_bot') {
      status.append(row('Paid through', date(membership.paid_through)));
      status.append(row('Auto renewal', membership.auto_renew_state === 'off' ? 'Off' : membership.auto_renew_state === 'on' ? 'On' : 'Unknown'));
    } else if (membership.subscription_until) {
      status.append(row('Observed expiry', date(membership.subscription_until)));
    }
    status.append(row('Access checked', membership.observed_at ? date(membership.observed_at) : 'Not yet observed'));
    if (!membership.fresh) status.append(node('p', '', 'Telegram is temporarily unavailable. Access shown here may be out of date.'));
  }
  content.append(status);

  const actions = node('div', 'actions');
  if (profile.mode === 'native_channel') {
    if (['member', 'administrator', 'creator'].includes(membership?.access_status)) {
      actions.append(node('div', 'empty', 'Your channel access is active. Open the channel from your Telegram chat list.'));
    } else {
      const subscribe = node('button', 'button full', 'Subscribe in Telegram');
      subscribe.type = 'button';
      subscribe.addEventListener('click', () => openTelegramLink(profile.subscription_link));
      actions.append(subscribe);
    }
  } else if (membership?.entitled) {
    const join = node('button', 'button full', ['member', 'administrator', 'creator'].includes(membership.access_status) ? 'Open community' : 'Request access');
    join.type = 'button';
    join.addEventListener('click', () => openTelegramLink(membership.join_link));
    actions.append(join);
    if (membership.auto_renew_state === 'on' && membership.subscription_id) {
      const cancel = node('button', 'button danger full', 'Turn off renewal');
      cancel.type = 'button';
      cancel.addEventListener('click', cancelRenewal);
      actions.append(cancel);
    }
  } else {
    const subscribe = node('button', 'button full', 'Subscribe with Stars');
    subscribe.type = 'button';
    subscribe.disabled = !state.token || !state.selectedPlan;
    subscribe.addEventListener('click', beginCheckout);
    actions.append(subscribe);
  }
  if (state.token) {
    const refresh = node('button', 'button ghost full', 'Refresh membership');
    refresh.type = 'button';
    refresh.addEventListener('click', refreshMembership);
    actions.append(refresh);
  } else {
    actions.append(node('div', 'empty', 'Open this Mini App in Telegram to verify your identity and manage membership.'));
  }
  content.append(actions);
}

async function beginCheckout() {
  if (!state.token || !state.selected || !state.selectedPlan) return;
  notice('Preparing your Telegram invoice…');
  try {
    const checkout = await api('/api/v1/checkouts', { method: 'POST', body: JSON.stringify({ community_id: state.selected, plan_id: state.selectedPlan }) });
    notice('Complete payment in Telegram. Access will appear after payment confirmation.');
    if (!tg?.openInvoice) { openTelegramLink(checkout.invoice_url); return; }
    tg.openInvoice(checkout.invoice_url, async () => {
      for (let attempt = 0; attempt < 8; attempt += 1) {
        await new Promise((resolve) => setTimeout(resolve, 2000));
        await refreshMembership();
        if (state.membership?.entitled) { notice('Payment confirmed. You can now request access.'); return; }
      }
      notice('Payment is still being confirmed. Refresh membership shortly.');
    });
  } catch (error) { notice(error.message, true); }
}

async function cancelRenewal() {
  const id = state.membership?.subscription_id;
  if (!id) return;
  const confirmed = tg?.showConfirm ? await new Promise((resolve) => tg.showConfirm('Turn off renewal? You keep access until the end of your paid period.', resolve))
    : window.confirm('Turn off renewal? You keep access until the end of your paid period.');
  if (!confirmed) return;
  try {
    await api(`/api/v1/me/subscriptions/${encodeURIComponent(id)}/cancel`, { method: 'POST' });
    notice('Renewal is off. Your paid access remains active through the date shown.');
    await refreshMembership();
  } catch (error) { notice(error.message, true); }
}


export { renderDiscovery, selectCommunity, refreshMembership, renderMembership };
