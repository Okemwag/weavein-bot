import { api, node, el, notice, date, stars, actionButton, fold, textSheet, statusPill } from './shared.js';

const auditLabels = { payment_recorded: 'Payment recorded', payment_refunded: 'Payment refunded', refund_approval: 'Refund sent', refund_rejected: 'Refund declined',
  subscription_canceled_by_member: 'Member cancelled renewal', community_connected: 'Community connected', community_disconnected: 'Community disconnected',
  duplicate_subscription_detected: 'Duplicate payment found', access_override_granted: 'Free access given', access_override_revoked: 'Free access ended',
  saas_payment_recorded: 'Pro payment', saas_refund_recorded: 'Pro refunded', saas_refund_approval: 'Pro refund sent', saas_pro_lapsed: 'Pro ended',
  saas_duplicate_subscription: 'Duplicate Pro payment', saas_subscription_canceled: 'Pro renewal cancelled', saas_subscription_failed: 'Pro renewal failed' };

// proPaymentRow lists one MemberOS Pro charge with a single-use refund action.
function proPaymentRow(payment, reload) {
  const status = payment.refunded_at ? 'Refunded' : payment.refund_status === 'needs_review' ? 'Check in Telegram' : payment.refund_status === 'submitted' ? 'Refund sent' : 'Paid';
  const row = el('div', 'list-row', el('span', 'grow', node('span', 'title', `${payment.workspace_name} · ${stars(payment.price_stars)}`),
    node('span', 'sub', `${date(payment.paid_at)} · paid by ${payment.payer_telegram_user_id}`)), node('span', 'pill', status));
  if (!payment.refunded_at && !payment.refund_status) {
    row.append(actionButton('Refund', 'text-button', async () => {
      const reason = await textSheet({ title: 'Refund this Pro payment?', body: 'Telegram returns the Stars to the payer and Pro ends unless another payment covers it. This can be sent only once.',
        label: 'Reason', confirm: 'Send refund', minLength: 10, maxLength: 500 });
      if (!reason) return;
      try {
        await api(`/api/v1/admin/saas/payments/${encodeURIComponent(payment.id)}/refund`, { method: 'POST', body: JSON.stringify({ reason }) });
        notice('Refund sent. It completes when Telegram confirms it.');
      } catch (error) { notice(error.message, true); }
      await reload();
    }));
  }
  return row;
}

function ticketCard(ticket, reload) {
  const flag = ticket.escalated_at ? 'With you' : ticket.escalation_due ? 'Overdue' : 'Open';
  const card = el('div', 'card', el('div', 'section-title', node('h3', '', ticket.community_title), node('span', `pill ${ticket.escalation_due ? 'warn' : ''}`, flag)),
    node('span', 'muted', `Member ${ticket.telegram_user_id} · creator contact: ${ticket.support_contact}`), node('p', '', ticket.description));
  const buttons = node('div', 'button-row');
  if (!ticket.escalated_at) {
    buttons.append(actionButton('Take over', 'button ghost small', async () => {
      const reason = await textSheet({ title: 'Take over this request', label: 'Why does this need you?', confirm: 'Take over' });
      if (!reason) return;
      await api(`/api/v1/admin/support-tickets/${encodeURIComponent(ticket.id)}/escalate`, { method: 'POST', body: JSON.stringify({ reason }) });
      notice('You now own this request.');
      await reload();
    }));
  }
  const evidence = actionButton('See records', 'button ghost small', async () => {
    const result = await api(`/api/v1/admin/support-tickets/${encodeURIComponent(ticket.id)}/evidence`);
    card.append(fold('Payment and access records', true, node('pre', 'evidence', JSON.stringify(result.evidence, null, 2))));
  });
  const solve = actionButton('Mark as solved', 'button secondary small', async () => {
    const resolution = await textSheet({ title: 'Mark as solved', label: 'What did you do to solve it?', confirm: 'Mark as solved' });
    if (!resolution) return;
    await api(`/api/v1/admin/support-tickets/${encodeURIComponent(ticket.id)}/resolve`, { method: 'POST', body: JSON.stringify({ resolution }) });
    notice('Marked as solved.');
    card.remove();
  });
  buttons.append(evidence, solve);
  card.append(buttons);
  return card;
}

function inquiryCard(inquiry) {
  const card = el('div', 'card', node('h3', '', inquiry.name), node('span', 'muted', inquiry.email), node('p', '', inquiry.message),
    inquiry.community_link ? node('span', 'muted', inquiry.community_link) : null);
  card.append(actionButton('Mark as handled', 'button secondary small', async () => {
    const resolution = await textSheet({ title: 'Mark as handled', label: 'What did you do?', confirm: 'Mark as handled' });
    if (!resolution) return;
    await api(`/api/v1/admin/creator-inquiries/${encodeURIComponent(inquiry.id)}/resolve`, { method: 'POST', body: JSON.stringify({ resolution }) });
    notice('Marked as handled.');
    card.remove();
  }));
  return card;
}

function findMember() {
  const form = node('form', 'stack');
  const q = node('input', 'input'); q.type = 'search'; q.placeholder = 'User ID, community or payment ID'; q.setAttribute('aria-label', 'Find a membership');
  const submit = node('button', 'button secondary', 'Find'); submit.type = 'submit';
  const results = node('div', 'group');
  form.append(q, submit, results);
  form.addEventListener('submit', async (event) => {
    event.preventDefault();
    try {
      const data = await api(`/api/v1/admin/incidents?q=${encodeURIComponent(q.value.trim())}`);
      results.replaceChildren();
      if (!data.incidents.length) results.append(el('div', 'list-row', node('span', 'sub', 'No memberships found.')));
      for (const entry of data.incidents) {
        results.append(el('div', 'list-row', el('span', 'grow', node('span', 'title', `${entry.community_title} · member ${entry.telegram_user_id}`),
          node('span', 'sub', `Paid until ${date(entry.paid_through)}${entry.failed_access_jobs ? ` · ${entry.failed_access_jobs} failed changes` : ''}`)), statusPill(entry.state)));
      }
    } catch (error) { notice(error.message, true); }
  });
  return fold('Find a membership', false, form);
}

async function renderInspection(content, updateName) {
  const reload = () => renderTickets();
  const [health, creators, tickets, audit, inquiries, saas, updates] = await Promise.all([
    api('/api/v1/admin/health'), api('/api/v1/admin/creators'), api('/api/v1/admin/support-tickets'), api('/api/v1/admin/audit'),
    api('/api/v1/admin/creator-inquiries'), api('/api/v1/admin/saas-finance'), api('/api/v1/admin/updates'),
  ]);
  const proPayments = await api('/api/v1/admin/saas/payments').catch(() => ({ payments: [] }));
  const ticketArea = node('div', 'stack');
  function renderTickets() {
    return api('/api/v1/admin/support-tickets').then((data) => ticketArea.replaceChildren(...data.tickets.map((ticket) => ticketCard(ticket, reload))));
  }
  if (tickets.tickets.length) {
    ticketArea.append(...tickets.tickets.map((ticket) => ticketCard(ticket, reload)));
    content.append(el('div', 'section-title', node('h2', '', 'Member requests'), node('span', '', String(tickets.tickets.length))), ticketArea);
  }
  if (inquiries.inquiries.length) {
    content.append(el('div', 'section-title', node('h2', '', 'People who want to join as creators'), node('span', '', String(inquiries.inquiries.length))), ...inquiries.inquiries.map(inquiryCard));
  }
  const webhook = health.telegram_webhook || {};
  const webhookWords = { healthy: 'Connected', delivery_backlog: 'Telegram is behind', recent_delivery_error: 'Recent delivery problem', webhook_mismatch: 'Pointing at the wrong address', origin_unconfigured: 'Public address not set', telegram_unavailable: "Can't reach Telegram" };
  const metric = (label, value) => el('div', 'metric', node('span', 'label', label), node('span', `number ${value > 0 ? 'attention' : ''}`, value));
  content.append(el('div', 'section-title', node('h2', '', 'System health'), node('span', `pill ${webhook.status === 'healthy' ? 'good' : 'warn'}`, webhookWords[webhook.status] || 'Unknown')),
    el('div', 'metrics', metric('Events waiting', health.pending_updates), metric('Failed events', health.failed_updates), metric('Failed changes', health.failed_access_jobs), metric('Open requests', health.open_support_tickets)));
  if (webhook.last_error) content.append(node('p', 'muted', `Latest Telegram delivery error: ${webhook.last_error}`));
  if (health.unhealthy_communities.length) {
    content.append(fold(`Communities with a problem (${health.unhealthy_communities.length})`, true, ...health.unhealthy_communities.map((entry) => el('div', 'list-row', el('span', 'grow', node('span', 'title', entry.title),
      node('span', 'sub', entry.error || 'Not checked yet'))))));
  }
  content.append(findMember());
  content.append(fold(`Creators (${creators.creators.length})`, false, ...creators.creators.map((entry) => el('div', 'list-row', el('span', 'grow', node('span', 'title', entry.community_title || entry.workspace_name),
    node('span', 'sub', `${entry.creator_name} · ${entry.status === 'published' ? 'Live' : entry.status === 'draft' ? 'Not live yet' : entry.status === 'archived' ? 'Closed' : 'Setting up'}`))))));
  content.append(fold('Recent Telegram events', false, ...(updates.recent || []).map((entry) => el('div', 'list-row', el('span', 'grow', node('span', 'title', updateName(entry.type)),
    node('span', 'sub', `${date(entry.received_at)}${entry.error ? ` · ${entry.error}` : ''}`)), node('span', 'pill', entry.state.replaceAll('_', ' '))))));
  content.append(fold('Recent activity', false, ...audit.audit.map((entry) => el('div', 'list-row', el('span', 'grow', node('span', 'title', auditLabels[entry.action] || entry.action.replaceAll('_', ' ')),
    node('span', 'sub', date(entry.at)))))));
  if (saas.saas_revenue.length) {
    content.append(fold('MemberOS revenue', false, ...saas.saas_revenue.map((entry) => node('p', '', entry.currency === 'XTR'
      ? `${stars(entry.net_minor)} net from ${entry.events} Pro events (${stars(entry.refunded_minor)} refunded)`
      : `${entry.currency} ${entry.net_minor} minor units from ${entry.events} events`))));
  }
  if (proPayments.payments.length) {
    const area = node('div', 'stack');
    const drawPro = async () => {
      const latest = await api('/api/v1/admin/saas/payments').catch(() => proPayments);
      area.replaceChildren(...latest.payments.map((payment) => proPaymentRow(payment, drawPro)));
    };
    area.append(...proPayments.payments.map((payment) => proPaymentRow(payment, drawPro)));
    content.append(fold(`Pro payments (${proPayments.payments.length})`, false, area));
  }
}

export { renderInspection };
