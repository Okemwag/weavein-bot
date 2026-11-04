import { api, node, notice, date } from './shared.js';

function section(content, title, items, render) {
  const heading = node('div', 'section-heading');
  heading.append(node('h2', '', title), node('span', 'muted', String(items.length)));
  const list = node('div', 'member-list');
  if (!items.length) list.append(node('div', 'empty', 'No items.'));
  for (const item of items) list.append(render(item));
  content.append(node('div', 'divider'), heading, list);
}

async function renderInspection(content) {
  const [health, creators, tickets, audit, inquiries] = await Promise.all([
    api('/api/v1/admin/health'), api('/api/v1/admin/creators'),
    api('/api/v1/admin/support-tickets'), api('/api/v1/admin/audit'),
    api('/api/v1/admin/creator-inquiries'),
  ]);
  const summary = node('div', 'metric-grid');
  for (const [label, value] of [['Pending updates', health.pending_updates],
    ['Failed updates', health.failed_updates], ['Failed access jobs', health.failed_access_jobs],
    ['Failed notices', health.failed_notifications], ['Open tickets', health.open_support_tickets]]) {
    const metric = node('div', 'metric');
    metric.append(node('div', 'label', label), node('div', 'number', value));
    summary.append(metric);
  }
  content.append(node('div', 'divider'), node('h2', '', 'Integration health'), summary);
  section(content, 'Communities needing attention', health.unhealthy_communities, (item) => {
    const row = node('div', 'member-row');
    row.append(node('strong', '', item.title + ' · ' + item.status), node('small', '', item.error || 'Connection has not been checked yet'));
    return row;
  });
  section(content, 'Creators and communities', creators.creators, (item) => {
    const row = node('div', 'member-row');
    row.append(node('strong', '', item.creator_name + ' · ' + (item.community_title || 'No community')),
      node('small', '', item.workspace_name + ' · ' + (item.status || 'setup')));
    return row;
  });
  section(content, 'Open support and escalations', tickets.tickets, (item) => {
    const row = node('div', 'member-row');
    row.append(node('strong', '', item.community_title + ' · user ' + item.telegram_user_id),
      node('small', '', (item.escalated ? 'Escalated · ' : '') + item.kind + ' · ' + item.description));
    const resolve = node('button', 'button secondary', 'Resolve'); resolve.type = 'button';
    resolve.addEventListener('click', async () => {
      const resolution = window.prompt('How was this issue resolved?');
      if (!resolution) return;
      try {
        await api('/api/v1/admin/support-tickets/' + encodeURIComponent(item.id) + '/resolve',
          { method: 'POST', body: JSON.stringify({ resolution }) });
        notice('Ticket resolved.'); row.remove();
      } catch (error) { notice(error.message, true); }
    });
    row.append(resolve); return row;
  });
  section(content, 'Creator inquiries', inquiries.inquiries, (item) => {
    const row = node('div', 'member-row');
    row.append(node('strong', '', item.name + ' · ' + item.email),
      node('small', '', item.message + (item.community_link ? ' · ' + item.community_link : '')));
    const resolve = node('button', 'button secondary', 'Mark resolved'); resolve.type = 'button';
    resolve.addEventListener('click', async () => {
      const resolution = window.prompt('Record how this inquiry was handled (at least 10 characters).');
      if (!resolution) return;
      try {
        await api('/api/v1/admin/creator-inquiries/' + encodeURIComponent(item.id) + '/resolve',
          { method: 'POST', body: JSON.stringify({ resolution }) });
        notice('Inquiry resolved.'); row.remove();
      } catch (error) { notice(error.message, true); }
    });
    row.append(resolve); return row;
  });
  const search = node('form', 'member-filters');
  const q = node('input', 'select'); q.placeholder = 'Search user ID, community, charge, or subscription';
  const submit = node('button', 'button secondary', 'Find incident'); submit.type = 'submit';
  const results = node('div', 'member-list');
  search.append(q, submit);
  search.addEventListener('submit', async (event) => {
    event.preventDefault();
    try {
      const data = await api('/api/v1/admin/incidents?q=' + encodeURIComponent(q.value.trim()));
      results.replaceChildren();
      if (!data.incidents.length) results.append(node('div', 'empty', 'No incidents found.'));
      for (const item of data.incidents) {
        const row = node('div', 'member-row');
        row.append(node('strong', '', item.community_title + ' · user ' + item.telegram_user_id),
          node('small', '', item.state + ' · paid through ' + date(item.paid_through)
            + ' · failed access jobs ' + item.failed_access_jobs));
        results.append(row);
      }
    } catch (error) { notice(error.message, true); }
  });
  content.append(node('div', 'divider'), node('h2', '', 'Subscription incidents'), search, results);
  section(content, 'Recent audit actions', audit.audit, (item) => {
    const row = node('div', 'member-row');
    row.append(node('strong', '', item.action), node('small', '', date(item.at)));
    return row;
  });
}

export { renderInspection };
