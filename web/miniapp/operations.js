import { api, byId, node, notice, date } from './shared.js';
import { renderInspection } from './operations_inspection.js';

async function loadOperations() {
  const content = byId('operations-content');
  content.replaceChildren(node('p', 'muted', 'Loading operations…'));
  try {
    const [updates, jobs, refunds, stars] = await Promise.all([
      api('/api/v1/admin/updates'), api('/api/v1/admin/access-jobs'),
      api('/api/v1/admin/refund-requests'), api('/api/v1/admin/star-discrepancies'),
    ]);
    content.replaceChildren();
    const action = (label, path, warning = '') => {
      const button = node('button', 'button secondary', label); button.type = 'button';
      button.addEventListener('click', async () => {
        if (warning && !window.confirm(warning)) return;
        button.disabled = true;
        try { await api(path, { method: 'POST' }); notice(`${label} requested.`); await loadOperations(); }
        catch (error) { notice(error.message, true); button.disabled = false; }
      });
      return button;
    };
    const section = (title, items, render) => {
      const heading = node('div', 'section-heading');
      heading.append(node('h2', '', title), node('span', 'muted', String(items.length)));
      const list = node('div', 'member-list');
      if (!items.length) list.append(node('div', 'empty', 'No items.'));
      for (const item of items) list.append(render(item));
      content.append(node('div', 'divider'), heading, list);
    };
    const controls = node('div', 'actions');
    controls.append(action('Check known access', '/api/v1/admin/reconcile-access'),
      action('Observe bot Stars', '/api/v1/admin/reconcile-stars'));
    content.append(controls);
    section('Webhook exceptions', updates.updates, (item) => {
      const row = node('div', 'member-row');
      row.append(node('strong', '', `${item.type} · update ${item.update_id}`),
        node('small', '', `${item.state} · ${item.error || ''}`),
        action('Retry', `/api/v1/admin/updates/${item.bot_id}/${item.update_id}/retry`));
      return row;
    });
    section('Failed access jobs', jobs.jobs, (item) => {
      const row = node('div', 'member-row');
      row.append(node('strong', '', `${item.action} · user ${item.telegram_user_id}`),
        node('small', '', item.error || 'Unknown error'),
        action('Retry', `/api/v1/admin/access-jobs/${item.id}/retry`));
      return row;
    });
    section('Refund requests', refunds.refund_requests, (item) => {
      const row = node('div', 'member-row');
      row.append(node('strong', '', `${item.amount_stars} Stars · ${item.status}`),
        node('small', '', `${item.reason} · ${date(item.created_at)}`));
      if (item.status === 'requested') row.append(action('Approve refund',
        `/api/v1/admin/refund-requests/${item.id}/approve`,
        `Refund ${item.amount_stars} Stars to the subscriber? This calls Telegram and may not be reversible.`));
      return row;
    });
    section('Stars discrepancies', stars.discrepancies, (item) => {
      const row = node('div', 'member-row');
      row.append(node('strong', '', `${item.reason} · ${item.amount_stars} Stars`),
        node('small', '', `Transaction ${item.transaction_id}`));
      return row;
    });
    await renderInspection(content);
  } catch (error) { content.replaceChildren(node('div', 'empty', error.message)); }
}

export { loadOperations };
