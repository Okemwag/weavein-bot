import { state, byId, node, el, api, notice, setAction, setBack, show } from './shared.js';
import { listSummary } from './member_status.js';

async function loadMyMemberships(onSelect) {
  setAction(null);
  setBack(null);
  byId('membership-back').replaceChildren();
  byId('membership-head').hidden = false;
  byId('membership-title').textContent = 'My memberships';
  byId('membership-description').textContent = '';
  const content = byId('membership-content');
  if (!state.token) {
    content.replaceChildren(node('div', 'empty', 'Open MemberOS in Telegram to see your memberships.'));
    return;
  }
  content.replaceChildren(node('p', 'muted', 'Loading your memberships…'));
  try {
    const data = await api('/api/v1/me/memberships');
    state.memberships = data.memberships;
    content.replaceChildren();
    if (!data.memberships.length) {
      const explore = node('button', 'button', 'Explore communities'); explore.type = 'button';
      explore.addEventListener('click', () => show('discover'));
      content.append(el('div', 'empty', node('strong', '', 'No memberships yet'),
        node('span', '', 'Communities you join will show up here, with your renewal date.'), explore));
      return;
    }
    const group = node('div', 'group');
    for (const member of data.memberships) {
      const summary = listSummary(member);
      const open = member.community_status === 'published';
      const row = node(open ? 'button' : 'div', 'list-row');
      if (open) { row.type = 'button'; row.addEventListener('click', () => onSelect(member.community_id, 'membership')); }
      row.append(node('span', 'avatar', (member.title || 'M').slice(0, 1).toUpperCase()),
        el('span', 'grow', node('span', 'title', member.title),
          node('span', 'sub', open ? summary.detail : 'This community has closed. Contact its owner about your account.')),
        node('span', `pill ${summary.pill}`, summary.label));
      group.append(row);
      if (member.renewing_subscriptions > 1) {
        group.append(el('div', 'list-row', node('span', 'sub', `You have ${member.renewing_subscriptions} renewals running for this community. Open it to turn off the extras.`)));
      }
    }
    content.append(group);
  } catch (error) { content.replaceChildren(node('div', 'empty', error.message)); notice(error.message, true); }
}

export { loadMyMemberships };
