import { tg, state, byId, uuid, node, notice, api, show, openTelegramLink, date, displayStatus } from './shared.js';
import { loadMyMemberships } from './subscriber_home.js';

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
  state.checkout = null;
  show('membership');
  byId('membership-title').textContent = 'Loading community…';
  byId('membership-description').textContent = '';
  byId('membership-content').replaceChildren();
  try {
    state.profile = await api(`/api/v1/communities/${encodeURIComponent(id)}`);
    state.plans = (await api(`/api/v1/communities/${encodeURIComponent(id)}/plans`)).plans || [];
    state.selectedPlan = state.plans[0]?.id || null;
    if (state.token && !state.notificationPrefs) {
      try { state.notificationPrefs = await api('/api/v1/me/notification-preferences'); }
      catch { state.notificationPrefs = null; }
    }
    if (state.token) {
      await Promise.all([refreshMembership(), refreshCheckout()]);
    }
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

async function refreshCheckout() {
  if (!state.selected || !state.token) return;
  try {
    state.checkout = await api('/api/v1/me/checkout-status?community_id=' + encodeURIComponent(state.selected));
    renderMembership();
  } catch (error) { notice(error.message, true); }
}

function row(label, value) {
  const outer = node('div', 'row');
  outer.append(node('span', 'muted', label), node('span', 'value', value));
  return outer;
}

function renderMembership() {
  const profile = state.profile;
  if (!profile) { loadMyMemberships(selectCommunity); return; }
  byId('membership-title').textContent = profile.title;
  byId('membership-description').textContent = profile.description || `A private community by ${profile.seller}.`;
  const content = byId('membership-content');
  content.replaceChildren();
  const allMemberships = node('button', 'text-button', '← All my memberships');
  allMemberships.type = 'button';
  allMemberships.addEventListener('click', () => {
    state.profile = null; state.selected = null; state.membership = null; state.checkout = null;
    loadMyMemberships(selectCommunity);
  });
  content.append(allMemberships);
  if (profile.category) content.append(node('p', 'eyebrow', profile.category));
  if (profile.rules) {
    const rules = node('div', 'detail-card');
    rules.append(node('div', 'label', 'Community rules'), node('p', '', profile.rules)); content.append(rules);
  }
  if (profile.content_guide) {
    const guide = node('div', 'detail-card');
    guide.append(node('div', 'label', 'Where to begin'), node('p', '', profile.content_guide)); content.append(guide);
  }

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
  if (profile.mode === 'managed_bot' && state.checkout && state.checkout.status !== 'none') {
    const checkout = node('div', 'detail-card');
    const current = state.checkout.status;
    checkout.append(node('div', 'label', 'Latest checkout'));
    const copy = {
      pending: 'Payment is pending. Complete the Telegram invoice or reopen it below.',
      paid: 'Payment confirmed by Telegram. Your membership is being updated.',
      failed: 'The invoice could not be prepared. Start a new checkout.',
      expired: 'This checkout expired. Start a new checkout.',
    };
    checkout.append(node('p', '', copy[current] || 'Check membership status for the latest outcome.'));
    if (current === 'pending' && state.checkout.invoice_url) {
      const reopen = node('button', 'button secondary full', 'Reopen invoice'); reopen.type = 'button';
      reopen.addEventListener('click', () => openTelegramLink(state.checkout.invoice_url));
      checkout.append(reopen);
    }
    content.append(checkout);
  }

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
  if (state.token) {
    const support = node('div', 'detail-card');
    support.append(node('div', 'label', 'Need help?'), node('p', '', 'Report a payment or access issue to the community team.'));
    const form = node('form');
    const kind = node('select', 'select');
    for (const [value, label] of [['access', 'Missing access'], ['payment', 'Payment question'], ['refund', 'Refund request'], ['other', 'Other']]) {
      const option = node('option', '', label); option.value = value; kind.append(option);
    }
    const description = node('textarea', 'input');
    description.placeholder = 'Describe what happened (at least 10 characters)';
    description.required = true; description.minLength = 10; description.maxLength = 2000;
    const submit = node('button', 'button secondary full', 'Send support request'); submit.type = 'submit';
    form.append(kind, description, submit);
    form.addEventListener('submit', async (event) => {
      event.preventDefault(); submit.disabled = true;
      try {
        await api('/api/v1/me/support-tickets', { method: 'POST', body: JSON.stringify({ community_id: state.selected, kind: kind.value, description: description.value }) });
        description.value = ''; notice('Support request received.');
      } catch (error) { notice(error.message, true); }
      finally { submit.disabled = false; }
    });
    support.append(form); content.append(support);
    if (profile.mode === 'managed_bot' && state.notificationPrefs) {
      const preference = node('label', 'row');
      const reminder = node('input'); reminder.type = 'checkbox';
      reminder.checked = state.notificationPrefs.reminders_enabled;
      reminder.addEventListener('change', async () => {
        try {
          state.notificationPrefs = await api('/api/v1/me/notification-preferences',
            { method: 'PATCH', body: JSON.stringify({ reminders_enabled: reminder.checked }) });
          notice('Reminder preference saved.');
        } catch (error) { reminder.checked = !reminder.checked; notice(error.message, true); }
      });
      preference.append(node('span', '', 'Remind me before paid access expires'), reminder);
      content.append(preference);
    }
    if (profile.mode === 'managed_bot' && (membership?.subscription_id || membership?.billing_status)) {
    const history = node('div', 'detail-card');
    const showPayments = node('button', 'button secondary full', 'View payments and refunds');
    showPayments.type = 'button';
    const paymentList = node('div', 'member-list');
    showPayments.addEventListener('click', async () => {
      try {
        const data = await api(`/api/v1/me/payments?community_id=${encodeURIComponent(state.selected)}`);
        paymentList.replaceChildren();
        if (!data.payments.length) paymentList.append(node('p', 'muted', 'No verified payments recorded yet.'));
        for (const payment of data.payments) {
          const item = node('div', 'member-row');
          item.append(node('strong', '', `${payment.amount_stars} Stars · ${date(payment.paid_at)}`));
          if (payment.refunded_at) item.append(node('small', '', 'Refund confirmed'));
          else if (payment.refund_requested) item.append(node('small', '', 'Refund under review'));
          else {
            const request = node('button', 'button secondary', 'Request refund review');
            request.type = 'button';
            request.addEventListener('click', async () => {
              const reason = window.prompt('Why are you requesting a refund? Please include at least 10 characters.');
              if (!reason) return;
              try {
                await api('/api/v1/me/refund-requests', { method: 'POST', body: JSON.stringify({ payment_id: payment.id, reason }) });
                notice('Refund request received for review.'); showPayments.click();
              } catch (error) { notice(error.message, true); }
            });
            item.append(request);
          }
          paymentList.append(item);
        }
      } catch (error) { notice(error.message, true); }
    });
    history.append(showPayments, paymentList); content.append(history);
    }
  }
}

async function beginCheckout() {
  if (!state.token || !state.selected || !state.selectedPlan) return;
  notice('Preparing your Telegram invoice…');
  try {
    const checkout = await api('/api/v1/checkouts', { method: 'POST', body: JSON.stringify({ community_id: state.selected, plan_id: state.selectedPlan }) });
    state.checkout = { ...checkout, status: 'pending' };
    renderMembership();
    notice('Complete payment in Telegram. Access will appear after payment confirmation.');
    if (!tg?.openInvoice) { openTelegramLink(checkout.invoice_url); return; }
    tg.openInvoice(checkout.invoice_url, async () => {
      for (let attempt = 0; attempt < 8; attempt += 1) {
        await new Promise((resolve) => setTimeout(resolve, 2000));
        await refreshMembership();
        await refreshCheckout();
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
