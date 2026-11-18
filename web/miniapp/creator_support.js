import { node, el, api, notice, actionButton, fold, textSheet, date } from './shared.js';

const kinds = { access: "Can't get in", payment: 'Payment question', refund: 'Refund request', other: 'Other' };

function ticketCard(ticket, context, redraw) {
  const card = el('div', 'card', el('div', 'section-title', node('h2', '', kinds[ticket.kind] || 'Request'), node('span', '', date(ticket.created_at))),
    node('span', 'muted', `From member ${ticket.telegram_user_id}`), node('p', '', ticket.description));
  if (ticket.status === 'resolved') {
    if (ticket.resolution) card.append(node('p', '', `Solved: ${ticket.resolution}`));
    return card;
  }
  card.append(actionButton('Mark as solved', 'button secondary full', async () => {
    const resolution = await textSheet({ title: 'Mark as solved', label: 'What did you do to solve it?',
      body: "This is kept for your records. The member isn't told automatically, so contact them in Telegram if they're waiting to hear from you.", confirm: 'Mark as solved' });
    if (!resolution) return;
    await api(`${context.root}/support-tickets/${encodeURIComponent(ticket.id)}/resolve`, { method: 'POST', body: JSON.stringify({ resolution }) });
    notice('Marked as solved.');
    await redraw();
  }));
  return card;
}

async function renderCreatorSupport(content, context) {
  const redraw = () => renderCreatorSupport(content, context);
  const support = await api(`${context.root}/support-tickets`);
  const open = support.tickets.filter((ticket) => ticket.status !== 'resolved');
  const solved = support.tickets.filter((ticket) => ticket.status === 'resolved');
  content.replaceChildren();
  if (!open.length) content.append(el('div', 'empty', node('strong', '', "You're all caught up"), node('span', '', 'When a member asks for help, it will appear here.')));
  for (const ticket of open) content.append(ticketCard(ticket, context, redraw));
  if (solved.length) content.append(fold(`Solved (${solved.length})`, false, ...solved.slice(0, 20).map((ticket) => ticketCard(ticket, context, redraw))));
}

export { renderCreatorSupport };
