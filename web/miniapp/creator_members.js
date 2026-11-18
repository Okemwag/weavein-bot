import { state, node, el, api, date, notice, setAction, setBack, actionButton, field, submitForm, submitButton, fold, confirmSheet, stars, statusPill } from './shared.js';

async function downloadCSV(path, filename) {
  const response = await fetch(path, { headers: { Authorization: `Bearer ${state.token}` }, cache: 'no-store' });
  if (!response.ok) throw new Error("We couldn't prepare that file. Please try again.");
  const url = URL.createObjectURL(await response.blob());
  const link = document.createElement('a');
  link.href = url; link.download = filename; link.click();
  setTimeout(() => URL.revokeObjectURL(url), 10000);
}

const memberName = (member) => member.first_name || (member.username ? `@${member.username}` : `Member ${member.telegram_user_id}`);

const eventLabels = { payment_verified: 'Payment received', payment_refunded: 'Payment refunded', subscription_canceled: 'Renewal cancelled',
  subscription_failed: 'Renewal failed', subscription_active: 'Renewal turned back on', cancellation_requested: 'Renewal cancelled',
  entitlement_expired: 'Membership ended', renewal_canceled_by_system: 'Renewal stopped by MemberOS' };

async function openMemberProfile(content, context, userID) {
  const memberPath = `${context.root}/members/${encodeURIComponent(userID)}`;
  const reopen = () => openMemberProfile(content, context, userID);
  const close = () => { setBack(null); renderMemberDirectory(content, context); };
  content.replaceChildren(node('p', 'muted', 'Loading member…'));
  const profile = await api(memberPath);
  const name = memberName({ ...profile, telegram_user_id: userID });
  content.replaceChildren(setBack(close, 'All members'),
    el('div', 'card', node('h1', '', name), node('span', 'muted', `${profile.username ? `@${profile.username} · ` : ''}Telegram ID ${userID}`), statusPill(profile.access_status)));
  if ((profile.subscriptions || []).length) {
    const group = node('div', 'group');
    for (const sub of profile.subscriptions) {
      group.append(el('div', 'list-row', el('span', 'grow', node('span', 'title', sub.plan || 'Membership'),
        node('span', 'sub', `Joined ${date(sub.subscribed_at)} · Paid until ${date(sub.paid_through)}`)), statusPill(sub.state)));
    }
    content.append(el('div', 'section-title', node('h2', '', 'Membership')), group);
  }
  if (profile.mode === 'managed_bot') content.append(await freeAccess(context, userID, reopen));
  const history = [...(profile.payments || []).map((payment) => node('p', '', `${stars(payment.amount_stars)} on ${date(payment.paid_at)}${payment.refunded_at ? ' (refunded)' : ''}`)),
    ...(profile.events || []).map((event) => node('p', '', `${eventLabels[event.kind] || 'Membership updated'} on ${date(event.at)}`))];
  content.append(fold('Payment history', false, ...(history.length ? history : [node('p', '', 'Nothing yet.')])));
  const form = el('form', 'form', field('Add a note', 'body', { type: 'textarea', maxLength: 2000 }), submitButton('Save note', 'button secondary full'));
  submitForm(form, async (values) => {
    await api(`${memberPath}/notes`, { method: 'POST', body: JSON.stringify(values) });
    notice('Note saved.');
    await reopen();
  });
  content.append(fold(`Private notes (${(profile.private_notes || []).length})`, false, node('p', '', 'Only you can see these. Members never do.'),
    ...(profile.private_notes || []).map((item) => node('p', '', `${date(item.created_at)}: ${item.body}`)), form));
}

async function freeAccess(context, userID, reopen) {
  const root = `${context.root}/access-overrides`;
  const data = await api(root);
  const mine = data.overrides.filter((entry) => entry.telegram_user_id === userID);
  const active = mine.filter((entry) => entry.active);
  const body = active.map((item) => el('div', 'list-row', el('span', 'grow', node('span', 'title', `Free until ${date(item.valid_until)}`), node('span', 'sub', item.reason)),
    actionButton('End now', 'button danger small', async () => {
      const sure = await confirmSheet({ title: 'End free access now?', body: 'If they have no paid membership, they will be removed from the community.', confirm: 'End free access', danger: true });
      if (!sure) return;
      await api(`${root}/${encodeURIComponent(item.id)}/revoke`, { method: 'POST' });
      notice('Free access ended.');
      await reopen();
    })));
  const days = node('select', 'input'); days.name = 'days'; days.setAttribute('aria-label', 'How long?');
  for (const value of [7, 14, 30, 90]) {
    const option = node('option', '', `${value} days`); option.value = value; option.selected = value === 30; days.append(option);
  }
  const form = el('form', 'form', el('label', 'field', node('span', '', 'How long?'), days),
    field('Why are you giving free access?', 'reason', { type: 'textarea', minLength: 10, maxLength: 500, placeholder: 'For example: giveaway winner, or a make-good for an outage.' }),
    submitButton('Give free access', 'button secondary full'));
  submitForm(form, async (values) => {
    const until = new Date(Date.now() + Number(values.days) * 86400000).toISOString();
    await api(root, { method: 'POST', body: JSON.stringify({ telegram_user_id: userID, valid_until: until, reason: values.reason }) });
    notice('Free access given. They can now request to join.');
    await reopen();
  });
  return fold('Give free access', active.length > 0, node('p', '', 'Let this person in without paying, for a limited time.'), ...body, form);
}

async function accessIssues(content, context) {
  if (context.community.mode !== 'managed_bot') return;
  const data = await api(`${context.root}/access-mismatches`);
  const issues = data.mismatches.filter((item) => item.failed_job || item.entitled !== ['member', 'restricted', 'administrator', 'creator'].includes(item.observed_status));
  if (!issues.length) return;
  const open = (item) => openMemberProfile(content, context, item.telegram_user_id);
  const group = node('div', 'group');
  for (const item of issues.slice(0, 20)) {
    const reason = item.failed_job ? "We couldn't update their access" : item.entitled ? 'Paid, but not in the community yet' : 'In the community without a membership';
    const row = el('button', 'list-row', el('span', 'grow', node('span', 'title', `Member ${item.telegram_user_id}`), node('span', 'sub', reason)), node('span', 'chevron', '›'));
    row.type = 'button';
    row.addEventListener('click', () => open(item).catch((error) => notice(error.message, true)));
    group.append(row);
  }
  content.append(el('div', 'section-title', node('h2', '', 'Needs a look'), node('span', '', String(issues.length))), group,
    node('p', 'muted', 'People who were already in your chat when you connected it stay in. Give them free access, or remove them yourself in Telegram.'));
}

async function renderMemberDirectory(content, context) {
  setAction(null);
  const plans = await api(`${context.root}/plans`);
  content.replaceChildren();
  await accessIssues(content, context);
  const search = node('input', 'input'); search.type = 'search'; search.placeholder = 'Search by name, @username or ID';
  search.setAttribute('aria-label', 'Search members');
  const status = node('select', 'input'); status.setAttribute('aria-label', 'Filter by status');
  for (const [value, label] of [['', 'Everyone'], ['active', 'Active'], ['cancel_scheduled', 'Ending soon'], ['payment_failed', 'Renewal failed'], ['expired', 'Ended'], ['refunded', 'Refunded']]) {
    const option = node('option', '', label); option.value = value; status.append(option);
  }
  const plan = node('select', 'input'); plan.setAttribute('aria-label', 'Filter by plan');
  const anyPlan = node('option', '', 'All plans'); anyPlan.value = ''; plan.append(anyPlan);
  for (const item of plans.plans || []) {
    const option = node('option', '', item.name); option.value = item.id; plan.append(option);
  }
  const form = el('form', 'stack', search, el('div', 'metrics', status, plan), node('button', 'button secondary', 'Search'));
  const query = () => new URLSearchParams({ q: search.value.trim(), status: status.value, plan_id: plan.value });
  const exporter = actionButton('Export', 'text-button', () => downloadCSV(`${context.root}/members.csv?${query()}`, 'memberos-members.csv'));
  const list = node('div', 'group');
  const pager = node('div', 'button-row');
  let offset = 0;
  const load = async () => {
    const values = query(); values.set('offset', String(offset));
    const data = await api(`${context.root}/members?${values}`);
    list.replaceChildren(); pager.replaceChildren();
    if (!data.members.length) list.append(node('div', 'empty', search.value || status.value || plan.value ? 'No members match that search.' : 'Your members will appear here once people join. Share your link from Home to get started.'));
    for (const member of data.members) {
      const row = el('button', 'list-row', el('span', 'grow', node('span', 'title', memberName(member)),
        node('span', 'sub', [member.plan_name, member.paid_through ? `Paid until ${date(member.paid_through)}` : null].filter(Boolean).join(' · '))),
      statusPill(member.billing_status || member.access_status), node('span', 'chevron', '›'));
      row.type = 'button';
      row.addEventListener('click', () => openMemberProfile(content, context, member.telegram_user_id).catch((error) => notice(error.message, true)));
      list.append(row);
    }
    if (offset > 0) pager.append(actionButton('Previous', 'button ghost', async () => { offset -= 50; await load(); }));
    if (data.has_more) pager.append(actionButton('Next', 'button ghost', async () => { offset += 50; await load(); }));
  };
  form.addEventListener('submit', (event) => { event.preventDefault(); offset = 0; load().catch((error) => notice(error.message, true)); });
  content.append(el('div', 'section-title', node('h2', '', 'Members'), exporter), form, list, pager);
  await load();
}

export { renderMemberDirectory, downloadCSV };
