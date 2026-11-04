import { state, byId, node, api, date, displayStatus, notice } from './shared.js';

async function loadMyMemberships(onSelect) {
  byId('membership-title').textContent = 'My memberships';
  byId('membership-description').textContent = 'View current and past communities, then open one to manage access.';
  const content = byId('membership-content');
  content.replaceChildren(node('p', 'muted', 'Loading memberships…'));
  if (!state.token) {
    content.replaceChildren(node('div', 'empty', 'Open MemberOS in Telegram to see your memberships.'));
    return;
  }
  try {
    const data = await api('/api/v1/me/memberships');
    content.replaceChildren();
    if (!data.memberships.length) {
      content.append(node('div', 'empty', 'No memberships yet. Explore communities to get started.'));
      return;
    }
    for (const member of data.memberships) {
      const card = node('div', 'detail-card');
      card.append(node('h2', '', member.title),
        node('p', '', member.mode === 'managed_bot'
          ? displayStatus(member.billing_status) + ' · Paid through ' + date(member.paid_through)
          : 'Telegram access: ' + displayStatus(member.access_status)));
      if (member.community_status === 'published') {
        const open = node('button', 'button secondary full', 'View membership');
        open.type = 'button'; open.addEventListener('click', () => onSelect(member.community_id));
        card.append(open);
      } else card.append(node('p', 'muted', 'This community is no longer open for new access. Contact support for account history.'));
      content.append(card);
    }
  } catch (error) { content.replaceChildren(node('div', 'empty', error.message)); notice(error.message, true); }
}

export { loadMyMemberships };
