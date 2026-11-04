import { state, node, api, date, displayStatus, notice } from './shared.js';

async function downloadCSV(path, filename) {
  const response = await fetch(path, { headers: { Authorization: 'Bearer ' + state.token }, cache: 'no-store' });
  if (!response.ok) throw new Error('Export unavailable (' + response.status + ').');
  const url = URL.createObjectURL(await response.blob());
  const link = document.createElement('a');
  link.href = url; link.download = filename; link.click();
  setTimeout(() => URL.revokeObjectURL(url), 10000);
}

function memberPath(communityID, userID) {
  return '/api/v1/creator/communities/' + encodeURIComponent(communityID)
    + '/members/' + encodeURIComponent(userID);
}

async function openMemberProfile(communityID, userID, container) {
  container.replaceChildren(node('p', 'muted', 'Loading member profile…'));
  try {
    const profile = await api(memberPath(communityID, userID));
    container.replaceChildren();
    const card = node('div', 'detail-card');
    card.append(node('h2', '', profile.first_name || 'Telegram user ' + userID),
      node('p', 'muted', 'ID ' + userID + (profile.username ? ' · @' + profile.username : '')),
      node('p', 'muted', 'Telegram access: ' + displayStatus(profile.access_status)));
    for (const sub of profile.subscriptions || []) {
      const item = node('div', 'member-row');
      item.append(node('strong', '', (sub.plan || 'Legacy plan') + ' · ' + displayStatus(sub.state)),
        node('small', '', 'Subscribed ' + date(sub.subscribed_at) + ' · Paid through ' + date(sub.paid_through)));
      card.append(item);
    }
    const history = node('details');
    history.append(node('summary', '', 'Payment and renewal history'));
    for (const payment of profile.payments || []) {
      history.append(node('p', '', payment.amount_stars + ' Stars · ' + date(payment.paid_at)
        + (payment.refunded_at ? ' · Refunded' : '')));
    }
    for (const event of profile.events || []) history.append(node('p', 'muted', event.kind + ' · ' + date(event.at)));
    card.append(history);
    const notes = node('details');
    notes.append(node('summary', '', 'Private administrative notes'));
    for (const item of profile.private_notes || []) notes.append(node('p', '', date(item.created_at) + ' · ' + item.body));
    const form = node('form', 'setup-form');
    const body = node('textarea', 'select'); body.required = true; body.maxLength = 2000; body.placeholder = 'Add a private note';
    const save = node('button', 'button secondary', 'Save note'); save.type = 'submit';
    form.append(body, save);
    form.addEventListener('submit', async (event) => {
      event.preventDefault();
      try {
        await api(memberPath(communityID, userID) + '/notes', { method: 'POST', body: JSON.stringify({ body: body.value }) });
        notice('Note saved.'); await openMemberProfile(communityID, userID, container);
      } catch (error) { notice(error.message, true); }
    });
    notes.append(form); card.append(notes); container.append(card);
    if (profile.mode === 'managed_bot') {
      const root = '/api/v1/creator/communities/' + encodeURIComponent(communityID) + '/access-overrides';
      const overrides = await api(root);
      const access = node('details', 'detail-card');
      access.append(node('summary', '', 'Temporary access override'));
      for (const item of overrides.overrides.filter((entry) => entry.telegram_user_id === userID)) {
        const row = node('div', 'member-row');
        row.append(node('span', '', (item.active ? 'Active until ' : 'Ended ') + date(item.valid_until) + ' · ' + item.reason));
        if (item.active) {
          const revoke = node('button', 'button danger', 'Revoke'); revoke.type = 'button';
          revoke.addEventListener('click', async () => {
            if (!window.confirm('Revoke this temporary access? Other paid access will be preserved.')) return;
            try {
              await api(root + '/' + encodeURIComponent(item.id) + '/revoke', { method: 'POST' });
              notice('Override revoked; access will be reconciled.'); await openMemberProfile(communityID, userID, container);
            } catch (error) { notice(error.message, true); }
          });
          row.append(revoke);
        }
        access.append(row);
      }
      const grant = node('form', 'setup-form');
      const until = node('input', 'select'); until.type = 'datetime-local'; until.required = true;
      const reason = node('textarea', 'select'); reason.placeholder = 'Reason for temporary access (10–500 characters)';
      reason.required = true; reason.minLength = 10; reason.maxLength = 500;
      const submit = node('button', 'button secondary', 'Grant temporary access'); submit.type = 'submit';
      grant.append(until, reason, submit);
      grant.addEventListener('submit', async (event) => {
        event.preventDefault();
        try {
          await api(root, { method: 'POST', body: JSON.stringify({
            telegram_user_id: userID, valid_until: new Date(until.value).toISOString(), reason: reason.value,
          }) });
          notice('Temporary access queued.'); await openMemberProfile(communityID, userID, container);
        } catch (error) { notice(error.message, true); }
      });
      access.append(grant); container.append(access);
    }
  } catch (error) { container.replaceChildren(node('div', 'empty', error.message)); }
}

function renderMemberDirectory(content, communityID, initial, plans) {
  const root = '/api/v1/creator/communities/' + encodeURIComponent(communityID);
  const section = node('div', 'section-heading');
  section.append(node('h2', '', 'Members'), node('span', 'muted', 'Known to MemberOS'));
  const form = node('form', 'member-filters');
  const search = node('input', 'select'); search.placeholder = 'Search name, username or Telegram ID';
  const status = node('select', 'select');
  for (const [value, label] of [['', 'All statuses'], ['active', 'Active'], ['cancel_scheduled', 'Canceled renewal'],
    ['payment_failed', 'Renewal failed'], ['expired', 'Expired'], ['refunded', 'Refunded']]) {
    const option = node('option', '', label); option.value = value; status.append(option);
  }
  const plan = node('select', 'select');
  const anyPlan = node('option', '', 'All plans'); anyPlan.value = ''; plan.append(anyPlan);
  for (const item of plans.plans || []) {
    const option = node('option', '', item.name); option.value = item.id; plan.append(option);
  }
  const searchButton = node('button', 'button secondary', 'Search'); searchButton.type = 'submit';
  const exportButton = node('button', 'button ghost', 'Export members'); exportButton.type = 'button';
  form.append(search, status, plan, searchButton, exportButton);
  const list = node('div', 'member-list');
  const controls = node('div', 'actions');
  const profile = node('div');
  let offset = 0;
  const query = () => {
    const values = new URLSearchParams({ q: search.value.trim(), status: status.value, plan_id: plan.value });
    return values;
  };
  const draw = (data) => {
    list.replaceChildren(); controls.replaceChildren();
    if (!data.members.length) list.append(node('div', 'empty', 'No matching members.'));
    for (const member of data.members) {
      const row = node('div', 'member-row');
      const label = node('div');
      label.append(node('strong', '', member.first_name || 'Telegram user ' + member.telegram_user_id),
        node('small', '', (member.plan_name ? member.plan_name + ' · ' : '')
          + displayStatus(member.billing_status || member.access_status)
          + (member.paid_through ? ' · ' + date(member.paid_through) : '')));
      const open = node('button', 'button secondary', 'Profile'); open.type = 'button';
      open.addEventListener('click', () => openMemberProfile(communityID, member.telegram_user_id, profile));
      row.append(label, open); list.append(row);
    }
    if (offset > 0) {
      const previous = node('button', 'button ghost', 'Previous'); previous.type = 'button';
      previous.addEventListener('click', () => { offset -= 50; load(); }); controls.append(previous);
    }
    if (data.has_more) {
      const next = node('button', 'button ghost', 'Next'); next.type = 'button';
      next.addEventListener('click', () => { offset += 50; load(); }); controls.append(next);
    }
  };
  const load = async () => {
    try { const values = query(); values.set('offset', String(offset)); draw(await api(root + '/members?' + values)); }
    catch (error) { notice(error.message, true); }
  };
  form.addEventListener('submit', (event) => { event.preventDefault(); offset = 0; load(); });
  exportButton.addEventListener('click', async () => {
    try { await downloadCSV(root + '/members.csv?' + query(), 'memberos-members.csv'); }
    catch (error) { notice(error.message, true); }
  });
  content.append(node('div', 'divider'), section, form, list, controls, profile);
  draw(initial);
}

export { renderMemberDirectory, downloadCSV };
