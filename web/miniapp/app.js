import { tg, state, byId, uuid, notice, api, show } from './shared.js';
import { renderDiscovery, selectCommunity, refreshMembership, renderMembership } from './subscriber.js';
import { loadStudio } from './creator.js';
import { loadOperations } from './operations.js';

async function start() {
  tg?.ready?.();
  tg?.expand?.();
  byId('tab-discover').addEventListener('click', () => show('discover'));
  byId('tab-membership').addEventListener('click', () => { show('membership'); renderMembership(); });
  byId('tab-studio').addEventListener('click', () => show('studio'));
  byId('tab-operations').addEventListener('click', () => { show('operations'); loadOperations(); });
  byId('back-to-discover').addEventListener('click', () => show('discover'));
  window.addEventListener('focus', () => { if (state.view === 'membership') refreshMembership(); });

  if (tg?.initData) {
    try {
      const session = await api('/api/v1/telegram/session', { method: 'POST', body: JSON.stringify({ init_data: tg.initData }) });
      state.token = session.token;
      byId('tab-operations').hidden = !session.is_operator;
      byId('connection').textContent = 'Connected to Telegram';
      loadStudio();
    } catch (error) { byId('connection').textContent = 'Session unavailable'; notice(error.message, true); }
  } else { byId('connection').textContent = 'Preview mode'; }

  try {
    const data = await api('/api/v1/communities');
    state.communities = data.communities || [];
    renderDiscovery();
    const url = new URL(window.location.href);
    const signedStart = tg?.initData ? new URLSearchParams(tg.initData).get('start_param') : '';
    const target = (url.searchParams.get('community') || signedStart || '').replace(/^c_/, '');
    if (uuid.test(target)) selectCommunity(target);
  } catch (error) { notice(error.message, true); }
}


start();
