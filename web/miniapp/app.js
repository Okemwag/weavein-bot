import { tg, state, byId, uuid, node, notice, api, show } from './shared.js';
import { renderDiscovery, selectCommunity, refreshMembership, renderMembership } from './subscriber.js';
import { loadStudio } from './creator.js';
import { loadOperations } from './operations.js';

function openMembershipsTab() {
  Object.assign(state, { selected: null, profile: null });
  show('membership');
  renderMembership();
}

// Returning people land where they left off: members on their memberships, and
// creators on their community. New visitors land on Explore.
async function chooseLandingView() {
  const params = new URL(window.location.href).searchParams;
  const signedStart = tg?.initData ? new URLSearchParams(tg.initData).get('start_param') : '';
  const target = (params.get('community') || signedStart || '').replace(/^c_/, '');
  if (uuid.test(target)) { selectCommunity(target, 'discover'); return; }
  if (!state.token) return;
  const memberships = await api('/api/v1/me/memberships').then((data) => data.memberships).catch(() => []);
  if (memberships.length) { openMembershipsTab(); return; }
  if (state.creatorCommunities.length) { show('studio'); loadStudio(); }
}

async function start() {
  tg?.ready?.();
  tg?.expand?.();
  byId('tab-discover').addEventListener('click', () => show('discover'));
  byId('tab-membership').addEventListener('click', openMembershipsTab);
  byId('tab-studio').addEventListener('click', () => { show('studio'); loadStudio(); });
  byId('tab-operations').addEventListener('click', () => { show('operations'); loadOperations(); });
  window.addEventListener('focus', () => { if (state.view === 'membership' && state.selected) refreshMembership(); });

  if (tg?.initData) {
    try {
      const session = await api('/api/v1/telegram/session', { method: 'POST', body: JSON.stringify({ init_data: tg.initData }) });
      state.token = session.token;
      byId('tab-operations').hidden = !session.is_operator;
      await loadStudio({ quiet: true });
    } catch (error) { notice(error.message, true); }
  } else {
    byId('preview-banner').hidden = false;
  }

  try {
    const data = await api('/api/v1/communities');
    state.communities = data.communities || [];
    renderDiscovery();
  } catch (error) { byId('community-list').replaceChildren(node('div', 'empty', error.message)); }
  await chooseLandingView();
}

start();
