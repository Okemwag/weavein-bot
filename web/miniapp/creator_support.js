import { node, api, notice } from './shared.js';

async function renderCreatorSupport(content, id, onResolved) {
  const root = `/api/v1/creator/communities/${encodeURIComponent(id)}`;
  const section = node('div', 'section-heading');
  section.append(node('h2', '', 'Support requests'), node('span', 'muted', 'Latest 100'));
  content.append(node('div', 'divider'), section);
  const tickets = node('div', 'member-list');
  content.append(tickets);
  try {
    const support = await api(`${root}/support-tickets`);
    if (!support.tickets.length) tickets.append(node('div', 'empty', 'No support requests yet.'));
    for (const ticket of support.tickets.slice(0, 20)) {
      const row = node('div', 'member-row');
      row.append(node('strong', '', `${ticket.kind} · Telegram user ${ticket.telegram_user_id}`),
        node('small', '', `${ticket.status} · ${ticket.description}`));
      if (ticket.status !== 'resolved') {
        const resolve = node('button', 'button secondary', 'Resolve'); resolve.type = 'button';
        resolve.addEventListener('click', async () => {
          const resolution = window.prompt('Describe how this support request was resolved (at least 10 characters).');
          if (!resolution) return;
          try {
            await api(`${root}/support-tickets/${ticket.id}/resolve`, { method: 'POST', body: JSON.stringify({ resolution }) });
            notice('Support request resolved.'); await onResolved();
          } catch (error) { notice(error.message, true); }
        });
        row.append(resolve);
      }
      tickets.append(row);
    }
  } catch (error) { tickets.replaceChildren(node('p', 'muted', `Support requests unavailable: ${error.message}`)); }
}

export { renderCreatorSupport };
