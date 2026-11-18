import { api, byId, node, el, notice, date, stars, actionButton, fold, confirmSheet, textSheet } from './shared.js';
import { renderInspection } from './operations_inspection.js';

function updateName(type) {
  return ({ message: 'Payment or refund', subscription: 'Renewal change', chat_member: 'Member joined or left', chat_join_request: 'Join request',
    pre_checkout_query: 'Payment check', command: 'Bot command', chat_shared: 'Channel selection', contact: 'Private message' })[type] || 'Telegram update';
}

const updateState = { failed: 'Failed after retries', quarantined: 'Looks wrong, so we set it aside', answer_unknown: 'Payment check may not have reached Telegram' };
const refundState = { requested: 'Waiting for your decision', processing: 'Sending to Telegram', submitted: 'Sent; waiting for Telegram to confirm', needs_review: "We don't know if this refund went through" };
const jobAction = { approve: 'Let in', remove: 'Remove', decline: 'Decline join request', cancel_renewal: 'Stop renewal for' };
const starReasons = { no_matching_payment: 'Telegram has this payment, but we have no record of it', amount_mismatch: 'The amounts differ', payload_mismatch: 'It does not match its checkout',
  payment_not_observed: 'We recorded this, but Telegram has not shown it yet', refund_without_payment: 'Telegram shows a refund we have no payment for', refund_not_recorded: 'Telegram shows a refund we have not recorded' };

async function loadOperations() {
  const content = byId('operations-content');
  content.replaceChildren(node('p', 'muted', 'Loading…'));
  try {
    const [updates, jobs, refunds, starRecords, notifications] = await Promise.all([
      api('/api/v1/admin/updates'), api('/api/v1/admin/access-jobs'),
      api('/api/v1/admin/refund-requests'), api('/api/v1/admin/star-discrepancies'),
      api('/api/v1/admin/notifications'),
    ]);
    content.replaceChildren();
    const run = (label, path, done, sheet) => actionButton(label, 'button secondary small', async () => {
      if (sheet && !(await confirmSheet(sheet))) return;
      await api(path, { method: 'POST' });
      notice(done);
      await loadOperations();
    });
    const noted = (label, path, question, done) => actionButton(label, 'button ghost small', async () => {
      const note = await textSheet({ title: label, label: question, confirm: label });
      if (!note) return;
      await api(path, { method: 'POST', body: JSON.stringify({ note }) });
      notice(done);
      await loadOperations();
    });
    const section = (title, items, render) => {
      if (!items.length) return 0;
      content.append(el('div', 'section-title', node('h2', '', title), node('span', '', String(items.length))), ...items.map(render));
      return items.length;
    };
    const item = (title, detail, ...buttons) => el('div', 'card', node('h3', '', title), detail ? node('p', '', detail) : null, buttons.length ? el('div', 'button-row', ...buttons) : null);

    let waiting = section('Refunds to decide', refunds.refund_requests, (refund) => {
      const buttons = [];
      if (refund.status === 'requested') buttons.push(actionButton('Refund', 'button small', async () => {
        const sure = await confirmSheet({ title: `Refund ${stars(refund.amount_stars)}?`, body: 'The Stars go back to the member, and you may not be able to reverse this.', confirm: 'Refund now', danger: true });
        if (!sure) return;
        await api(`/api/v1/admin/refund-requests/${refund.id}/approve`, { method: 'POST' });
        notice('Refund sent to Telegram.');
        await loadOperations();
      }));
      if (refund.status === 'needs_review') buttons.push(noted('Try again', `/api/v1/admin/refund-requests/${refund.id}/reopen`, 'Confirm in the Stars history that Telegram did not refund this payment.', 'Ready to approve again.'));
      if (refund.status === 'requested' || refund.status === 'needs_review') buttons.push(noted('Decline', `/api/v1/admin/refund-requests/${refund.id}/reject`, 'Why are you declining this refund?', 'Refund declined.'));
      return item(`${stars(refund.amount_stars)} · ${refundState[refund.status] || refund.status}`, `“${refund.reason}” · ${date(refund.created_at)}`, ...buttons);
    });
    waiting += section('Telegram events we could not process', updates.updates, (update) => {
      const buttons = [];
      if (update.state !== 'answer_unknown') buttons.push(run('Try again', `/api/v1/admin/updates/${update.bot_id}/${update.update_id}/retry`, 'Queued to try again.'));
      buttons.push(noted('Mark as reviewed', `/api/v1/admin/updates/${update.bot_id}/${update.update_id}/acknowledge`, 'What did you check?', 'Marked as reviewed.'));
      return item(updateName(update.type), `${updateState[update.state] || update.state}${update.error ? `: ${update.error}` : ''}`, ...buttons);
    });
    waiting += section('Membership changes that failed', jobs.jobs, (job) => item(`${jobAction[job.action] || 'Update'} member ${job.telegram_user_id}`, job.error || 'No reason recorded.',
      run('Try again', `/api/v1/admin/access-jobs/${job.id}/retry`, 'Queued to try again.'),
      noted('Mark as reviewed', `/api/v1/admin/access-jobs/${job.id}/acknowledge`, 'Why does this need no further action?', 'Marked as reviewed.')));
    const duplicateRisk = { title: 'Send again?', body: 'Telegram may have delivered this already, so people could get a message twice.', confirm: 'Send again' };
    const unsure = notifications.notifications.filter((entry) => entry.status === 'uncertain');
    if (unsure.length > 1) content.append(run(`Send all ${unsure.length} unconfirmed messages again`, '/api/v1/admin/notifications/retry-uncertain?acknowledge_duplicate_risk=true', 'Queued to send again.', duplicateRisk));
    waiting += section('Messages we are not sure were delivered', notifications.notifications, (entry) => {
      const uncertain = entry.status !== 'failed';
      return item(`To member ${entry.telegram_user_id}`, entry.error || 'Delivery not confirmed.',
        run('Send again', `/api/v1/admin/notifications/${entry.id}/retry${uncertain ? '?acknowledge_duplicate_risk=true' : ''}`, 'Queued to send again.', uncertain ? duplicateRisk : null),
        noted('Mark as reviewed', `/api/v1/admin/notifications/${entry.id}/acknowledge`, 'Why does this need no further action?', 'Marked as reviewed.'));
    });
    if (starRecords.discrepancies.length) {
      content.append(fold(`Stars records that differ (${starRecords.discrepancies.length})`, false, ...starRecords.discrepancies.map((entry) => el('div', 'list-row', el('span', 'grow', node('span', 'title', stars(entry.amount_stars)),
        node('span', 'sub', `${starReasons[entry.reason] || entry.reason} · ${entry.transaction_id}`))))));
    }
    if (!waiting) content.prepend(el('div', 'empty', node('strong', '', "You're all caught up"), node('span', '', 'Nothing needs a decision right now.')));
    await renderInspection(content, updateName);
    content.append(el('div', 'section-title', node('h2', '', 'Tools')), el('div', 'button-row',
      run('Check member access now', '/api/v1/admin/reconcile-access', 'Member access checked.'),
      run('Compare Stars with Telegram', '/api/v1/admin/reconcile-stars', 'Stars records compared.')));
  } catch (error) { content.replaceChildren(node('div', 'empty', error.message)); }
}

export { loadOperations };
