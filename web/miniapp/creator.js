import { state, byId, node, el, api, setAction, setBack } from './shared.js';
import { renderProfileStep, renderConnectStep } from './creator_setup.js';
import { renderHome } from './creator_home.js';
import { renderMemberDirectory } from './creator_members.js';
import { renderPlans } from './creator_plans.js';
import { renderCreatorSupport } from './creator_support.js';
import { renderSettings } from './creator_settings.js';

const tabs = [['home', 'Home'], ['members', 'Members'], ['plans', 'Plans'], ['requests', 'Requests'], ['settings', 'Settings']];

// loadStudio decides where a creator is in their journey: no profile, no
// community yet, or running a community.
async function loadStudio({ quiet = false } = {}) {
  if (!state.token) return;
  try {
    const [communities, workspaces] = await Promise.all([api('/api/v1/creator/communities'), api('/api/v1/creator/workspaces')]);
    state.creatorCommunities = communities.communities || [];
    state.workspaces = workspaces.workspaces || [];
    byId('tab-studio').hidden = false;
    byId('tab-studio-label').textContent = state.workspaces.length ? 'Manage' : 'Create';
    if (!state.workspaces.some((item) => item.id === state.activeWorkspaceId)) state.activeWorkspaceId = state.workspaces[0]?.id || null;
    // Closed communities drop out, unless their disconnect still has to be finished.
    const available = state.creatorCommunities.filter((item) => item.workspace_id === state.activeWorkspaceId && (item.status !== 'archived' || item.disconnect_unfinished));
    if (!available.some((item) => item.id === state.activeCommunityId)) state.activeCommunityId = available[0]?.id || null;
    if (quiet) return;
    await draw(available);
  } catch (error) {
    if (!quiet) byId('studio-content').replaceChildren(node('div', 'empty', error.message));
  }
}

async function draw(available) {
  const content = byId('studio-content');
  const tabBar = byId('studio-tabs');
  const head = byId('studio-head');
  tabBar.replaceChildren();
  head.replaceChildren();
  setAction(null);
  const active = state.workspaces.find((item) => item.id === state.activeWorkspaceId);
  if (!active) { renderProfileStep(head, content, loadStudio); return; }
  if (!available.length || state.addingCommunity) {
    const cancel = available.length ? () => { state.addingCommunity = false; setBack(null); loadStudio(); } : null;
    await renderConnectStep(head, content, active, available.length, cancel, async (id) => {
      state.addingCommunity = false; state.activeCommunityId = id; state.studioTab = 'home'; await loadStudio();
    });
    return;
  }
  setBack(null);
  const community = available.find((item) => item.id === state.activeCommunityId);
  drawHeader(head, available, community);
  for (const [key, label] of tabs) {
    const tab = node('button', `tab ${state.studioTab === key ? 'active' : ''}`, label); tab.type = 'button';
    if (state.studioTab === key) tab.setAttribute('aria-current', 'page');
    tab.addEventListener('click', () => { state.studioTab = key; window.scrollTo({ top: 0 }); draw(available); });
    tabBar.append(tab);
  }
  const context = { id: community.id, community, workspace: active, root: `/api/v1/creator/communities/${encodeURIComponent(community.id)}`,
    refresh: loadStudio, goTab: (key) => { state.studioTab = key; window.scrollTo({ top: 0 }); draw(available); },
    addCommunity: () => { state.addingCommunity = true; loadStudio(); } };
  content.replaceChildren(node('p', 'muted', 'Loading…'));
  const renderers = { home: renderHome, members: renderMemberDirectory, plans: renderPlans, requests: renderCreatorSupport, settings: renderSettings };
  try { await renderers[state.studioTab](content, context); }
  catch (error) { content.replaceChildren(node('div', 'empty', error.message)); }
}

function drawHeader(head, available, community) {
  if (available.length > 1) {
    const picker = node('select', 'input'); picker.setAttribute('aria-label', 'Choose a community');
    for (const item of available) {
      const option = node('option', '', item.title); option.value = item.id; option.selected = item.id === community.id; picker.append(option);
    }
    picker.addEventListener('change', () => { state.activeCommunityId = picker.value; state.studioTab = 'home'; loadStudio(); });
    head.append(picker);
    return;
  }
  const status = community.status === 'draft' ? node('span', 'pill warn', 'Not live yet') : community.status === 'published' ? node('span', 'pill good', 'Live') : node('span', 'pill', 'Closed');
  head.append(el('div', 'section-title', node('h1', '', community.title), status));
}

export { loadStudio };
