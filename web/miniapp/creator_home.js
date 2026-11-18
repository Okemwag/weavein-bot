import { node, el, api, notice, actionButton, openTelegramLink, date, stars } from './shared.js';
import { downloadCSV } from './creator_members.js';

let periodDays = 30;

function periodQuery() {
  const day = (offset) => new Date(Date.now() + offset * 86400000).toISOString().slice(0, 10);
  return new URLSearchParams({ from: day(-periodDays), to: day(1) });
}

function shareLink(community) {
  return new URL(`/app/?community=${encodeURIComponent(community.id)}`, window.location.origin).href;
}

function attentionCard(tone, title, detail, button) {
  return el('div', `card tone-${tone}`, node('h2', '', title), node('p', '', detail), button);
}

async function recheck(context, message) {
  await api(`${context.root}/recheck`, { method: 'POST' });
  notice(message);
  await context.refresh();
}

// Anything needing the creator comes first, each with the action that fixes it.
function attention(context, overview, planData, openRequests) {
  const { community } = context;
  const cards = [];
  if (community.disconnect_unfinished) {
    cards.push(attentionCard('warn', 'Finish disconnecting', 'Your community is closed, but its old join link still needs to be cancelled.',
      actionButton('Go to Settings', 'button full', () => context.goTab('settings'))));
  }
  if (community.status === 'draft') {
    cards.push(attentionCard('warn', 'Your community is not live yet', 'We saved it, but we could not finish connecting it to Telegram. People can’t join until this is done.',
      actionButton('Finish connecting', 'button full', () => recheck(context, 'Your community is live.'))));
  }
  if (community.status === 'published' && community.mode === 'managed_bot' && ['degraded', 'disconnected'].includes(community.connection_status)) {
    cards.push(attentionCard('bad', 'MemberOS can’t manage your channel', 'Check that MemberOS is still an admin that can invite and remove members, then try again. Until then, new members can’t get in.',
      actionButton('I fixed it, check again', 'button full', () => recheck(context, 'All good. MemberOS can manage your channel again.'))));
  }
  const activePlans = (planData.plans || []).filter((plan) => plan.status === 'active');
  if (community.mode === 'managed_bot' && community.status === 'published' && !activePlans.length) {
    cards.push(attentionCard('warn', 'Add a plan so people can join', 'You have no plan on sale, so nobody can pay to join yet.',
      actionButton('Add a plan', 'button full', () => context.goTab('plans'))));
  }
  if (openRequests > 0) {
    cards.push(attentionCard('warn', `${openRequests} ${openRequests === 1 ? 'member needs' : 'members need'} help`, 'Replying quickly prevents refunds and cancellations.',
      actionButton('View requests', 'button secondary full', () => context.goTab('requests'))));
  }
  const billing = overview.billing_metrics;
  if (billing !== 'unavailable' && billing?.failed_access_jobs > 0) {
    cards.push(attentionCard('bad', `${billing.failed_access_jobs} membership ${billing.failed_access_jobs === 1 ? 'update' : 'updates'} failed`, 'We could not let someone in or out of your community. See who is affected under Members.',
      actionButton('View members', 'button secondary full', () => context.goTab('members'))));
  }
  return cards;
}

function shareCard(community) {
  const link = shareLink(community);
  const share = actionButton('Share your link', 'button full', () => {
    openTelegramLink(`https://t.me/share/url?url=${encodeURIComponent(link)}&text=${encodeURIComponent(`Join ${community.title}`)}`);
  });
  const copy = actionButton('Copy', 'button ghost small', async () => {
    try { await navigator.clipboard.writeText(link); notice('Link copied.'); }
    catch { notice('Press and hold the link above to copy it.'); }
  });
  return el('div', 'card', node('h2', '', 'Invite people'), node('p', '', 'Anyone with this link can see your community and join.'),
    el('div', 'link-box', link), el('div', 'button-row', share, copy));
}

function metric(label, value, attentionValue = false) {
  return el('div', 'metric', node('span', 'label', label), node('span', `number ${attentionValue ? 'attention' : ''}`, value));
}

function numbers(content, context, overview) {
  const period = node('select', 'input'); period.setAttribute('aria-label', 'Time period');
  for (const [days, label] of [[7, 'Last 7 days'], [30, 'Last 30 days'], [90, 'Last 90 days'], [365, 'Last year']]) {
    const option = node('option', '', label); option.value = days; option.selected = days === periodDays; period.append(option);
  }
  period.addEventListener('change', () => { periodDays = Number(period.value); context.goTab('home'); });
  const billing = overview.billing_metrics;
  if (billing === 'unavailable') {
    content.append(el('div', 'section-title', node('h2', '', 'Your numbers')),
      el('div', 'metrics', metric('Members we can see', overview.observed_members)),
      node('p', 'muted', "Telegram handles payments for this channel, so it doesn't share earnings with us. You can see them in Telegram."));
    return;
  }
  content.append(el('div', 'section-title', node('h2', '', 'Your numbers')), period,
    el('div', 'metrics', metric('Active members', billing.active_paid_subscriptions),
      metric('New members', billing.new_subscribers),
      metric('Stars earned', (billing.recorded_gross_stars - billing.recorded_refunded_stars).toLocaleString()),
      metric('Cancelled', billing.cancellations)));
  const outcomes = billing.observed_renewal_successes + billing.observed_renewal_failures;
  const details = node('div', 'group');
  const row = (label, value) => details.append(el('div', 'kv', node('span', '', label), node('span', '', value)));
  if (billing.subscribers_awaiting_access > 0) row('Paid but not joined yet', billing.subscribers_awaiting_access);
  if (billing.expired_subscriptions > 0) row('Memberships ended', billing.expired_subscriptions);
  if (outcomes) row('Renewals that succeeded', `${Math.round(100 * billing.observed_renewal_successes / outcomes)}%`);
  for (const item of billing.paid_by_plan || []) row(`On ${item.plan}`, `${item.subscribers} ${item.subscribers === 1 ? 'member' : 'members'}`);
  if (details.children.length) content.append(details);
}

async function recentPayments(content, context) {
  const data = await api(`${context.root}/payments`);
  const exporter = actionButton('Export all', 'text-button', () => downloadCSV(`${context.root}/payments.csv?${periodQuery()}`, 'memberos-payments.csv'));
  content.append(el('div', 'section-title', node('h2', '', 'Recent payments'), data.payments.length ? exporter : null));
  if (!data.payments.length) { content.append(node('div', 'empty', 'Payments appear here as soon as someone joins.')); return; }
  const group = node('div', 'group');
  for (const payment of data.payments.slice(0, 5)) {
    group.append(el('div', 'list-row', el('span', 'grow', node('span', 'title', stars(payment.amount_stars)),
      node('span', 'sub', `Member ${payment.telegram_user_id} · ${date(payment.paid_at)}`)),
    payment.refunded_at ? node('span', 'pill', 'Refunded') : null));
  }
  content.append(group);
}

async function renderHome(content, context) {
  const [overview, planData, support] = await Promise.all([
    api(`${context.root}/overview?${periodQuery()}`), api(`${context.root}/plans`), api(`${context.root}/support-tickets`),
  ]);
  const openRequests = support.tickets.filter((ticket) => ticket.status !== 'resolved').length;
  content.replaceChildren(...attention(context, overview, planData, openRequests));
  if (context.community.status === 'published') content.append(shareCard(context.community));
  numbers(content, context, overview);
  if (context.community.mode === 'managed_bot') await recentPayments(content, context);
}

export { renderHome };
