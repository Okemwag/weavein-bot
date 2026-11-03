import { tg, state, byId, node, notice, api, date, displayStatus } from './shared.js';

async function loadStudio() {
  if (!state.token) return;
  try {
    const [data, workspaceData] = await Promise.all([api('/api/v1/creator/communities'), api('/api/v1/creator/workspaces')]);
    state.creatorCommunities = data.communities || [];
    state.workspaces = workspaceData.workspaces || [];
    byId('tab-studio').hidden = false;
    if (!state.workspaces.some((item) => item.id === state.activeWorkspaceId)) state.activeWorkspaceId = state.workspaces[0]?.id || null;
    await renderStudioSetup();
    const available = state.creatorCommunities.filter((item) => item.workspace_id === state.activeWorkspaceId);
    if (!available.some((item) => item.id === state.activeCommunityId)) state.activeCommunityId = available[0]?.id || null;
    if (state.activeCommunityId) await renderStudio(state.activeCommunityId);
    else byId('studio-content').replaceChildren(node('div', 'empty', 'Connect your first private community to continue setup.'));
  } catch (error) { byId('studio-content').replaceChildren(node('div', 'empty', error.message)); }
}

function formField(label, name, type = 'text', required = true, value = '') {
  const wrapper = node('label', 'form-field');
  wrapper.append(node('span', '', label));
  const input = node(type === 'textarea' ? 'textarea' : 'input', 'select');
  if (type !== 'textarea') input.type = type;
  input.name = name;
  input.required = required;
  input.value = value;
  if (name === 'price_stars') { input.min = '1'; input.max = '10000'; }
  wrapper.append(input);
  return wrapper;
}

async function renderStudioSetup() {
  const setup = byId('studio-setup');
  setup.replaceChildren();
  if (!state.workspaces.length) {
    const form = node('form', 'detail-card setup-form');
    form.append(node('h2', '', 'Create your workspace'), node('p', '', 'Use the name your subscribers should recognize.'),
      formField('Business or creator name', 'name'), formField('Support contact', 'support_contact'), node('button', 'button full', 'Create workspace'));
    form.addEventListener('submit', async (event) => {
      event.preventDefault();
      try {
        await api('/api/v1/creator/workspaces', { method: 'POST', body: JSON.stringify(Object.fromEntries(new FormData(form))) });
        notice('Workspace created. Connect your private community.');
        await loadStudio();
      } catch (error) { notice(error.message, true); }
    });
    setup.append(form);
    return;
  }
  const active = state.workspaces.find((item) => item.id === state.activeWorkspaceId);
  if (!active) return;
  if (state.workspaces.length > 1) {
    const picker = node('select', 'select'); picker.setAttribute('aria-label', 'Choose a workspace');
    for (const item of state.workspaces) {
      const option = node('option', '', item.name); option.value = item.id; option.selected = item.id === active.id; picker.append(option);
    }
    picker.addEventListener('change', () => { state.activeWorkspaceId = picker.value; state.activeCommunityId = null; loadStudio(); });
    setup.append(picker);
  }
  let progress;
  try { progress = await api(`/api/v1/creator/workspaces/${encodeURIComponent(active.id)}/onboarding`); }
  catch (error) { setup.append(node('div', 'empty', error.message)); return; }
  const checklist = node('div', 'detail-card');
  checklist.append(node('h2', '', progress.setup_complete ? 'Community setup complete' : 'Finish your setup'),
    node('p', '', 'Your progress is saved as you complete each step. Reopen Studio any time to continue.'));
  const steps = node('ol', 'setup-steps');
  for (const step of progress.steps) steps.append(node('li', step.complete ? 'done' : '', `${step.complete ? '✓' : '○'} ${step.label}`));
  checklist.append(steps);
  setup.append(checklist);
  if (active.role !== 'owner') return;
  const profile = node('details', 'detail-card setup-details');
  profile.append(node('summary', '', 'Creator profile and support contact'));
  const profileForm = node('form', 'setup-form');
  profileForm.append(formField('Business or creator name', 'name', 'text', true, active.name),
    formField('Support contact', 'support_contact', 'text', true, active.support_contact), node('button', 'button full', 'Save profile'));
  profileForm.addEventListener('submit', async (event) => {
    event.preventDefault();
    try {
      await api(`/api/v1/creator/workspaces/${encodeURIComponent(active.id)}`, { method: 'PATCH', body: JSON.stringify(Object.fromEntries(new FormData(profileForm))) });
      notice('Creator profile saved.'); await loadStudio();
    } catch (error) { notice(error.message, true); }
  });
  profile.append(profileForm); setup.append(profile);
  const connection = node('details', 'detail-card setup-details');
  connection.open = progress.connected_communities === 0;
  connection.append(node('summary', '', 'Connect another private community'));
  const form = node('form', 'setup-form');
  form.append(node('p', '', 'Add the configured MemberOS bot as an administrator in your channel or supergroup, then enter its numeric chat ID. You must own the chat.'));
  const mode = node('select', 'select'); mode.name = 'billing_mode';
  const nativeOption = node('option', '', 'Native paid channel'); nativeOption.value = 'native_channel'; mode.append(nativeOption);
  const managedOption = node('option', '', 'Bot-managed Stars subscription'); managedOption.value = 'managed_bot'; mode.append(managedOption);
  managedOption.disabled = !active.managed_billing_enabled;
  const modeLabel = node('label', 'form-field'); modeLabel.append(node('span', '', 'Billing mode'), mode);
  form.append(formField('Telegram chat ID', 'telegram_chat_id'), formField('Community title', 'title'),
    formField('Description', 'description', 'textarea', false), formField('Price in Stars / 30 days', 'price_stars', 'number'), modeLabel,
    node('p', '', 'Managed subscriptions credit this deployment’s billing bot. Confirm its Stars recipient with the MemberOS operator before selling.'),
    node('button', 'button full', 'Connect community'));
  form.addEventListener('submit', async (event) => {
    event.preventDefault();
    const values = Object.fromEntries(new FormData(form));
    const body = { telegram_chat_id: Number(values.telegram_chat_id), title: values.title, description: values.description,
      price_stars: Number(values.price_stars), billing_mode: values.billing_mode };
    try {
      const result = await api(`/api/v1/creator/workspaces/${encodeURIComponent(active.id)}/communities`, { method: 'POST', body: JSON.stringify(body) });
      state.activeCommunityId = result.id;
      notice(result.status === 'published' ? 'Community connected and published.' : 'Community saved as a draft. Recheck after webhook setup is restored.');
      await loadStudio();
    } catch (error) { notice(error.message, true); }
  });
  connection.append(form); setup.append(connection);
}

function renderPlanManager(content, communityID, data) {
  const section = node('div', 'detail-card');
  section.append(node('h2', '', 'Membership plans'));
  if (data.mode === 'native_channel') {
    section.append(node('p', '', 'Telegram controls the price and access for this paid channel link. Changing its offer requires a new Telegram subscription link.'));
    content.append(section);
    return;
  }
  section.append(node('p', '', 'New subscribers see the latest active version. Existing purchases keep their recorded version and price.'));
  for (const plan of data.plans) {
    const card = node('div', 'plan-editor');
    card.append(node('strong', '', plan.name), node('span', `badge ${plan.status === 'paused' ? 'warn' : ''}`, plan.status),
      node('p', '', `${plan.price_stars} Stars / 30 days · version ${plan.version}`));
    if (plan.status !== 'archived') {
      const edit = node('details', 'setup-details');
      edit.append(node('summary', '', 'Edit future offer terms'));
      const form = node('form', 'setup-form');
      form.append(formField('Price in Stars / 30 days', 'price_stars', 'number', true, plan.price_stars),
        formField('Description', 'description', 'textarea', false, plan.description),
        formField('Benefits', 'benefits', 'textarea', false, plan.benefits),
        formField('Renewal and cancellation terms', 'terms', 'textarea', true, plan.terms),
        node('button', 'button full', 'Publish new version'));
      form.addEventListener('submit', async (event) => {
        event.preventDefault();
        try {
          const values = Object.fromEntries(new FormData(form));
          await api(`/api/v1/creator/plans/${encodeURIComponent(plan.id)}/versions`, { method: 'POST', body: JSON.stringify({ ...values, price_stars: Number(values.price_stars) }) });
          notice('New plan version published. Existing purchases keep their original terms.');
          await renderStudio(communityID);
        } catch (error) { notice(error.message, true); }
      });
      edit.append(form); card.append(edit);
      const controls = node('div', 'plan-controls');
      const nextStatus = plan.status === 'active' ? 'paused' : 'active';
      const toggle = node('button', 'button secondary', plan.status === 'active' ? 'Pause new sales' : 'Resume new sales');
      toggle.type = 'button';
      toggle.addEventListener('click', async () => {
        try {
          await api(`/api/v1/creator/plans/${encodeURIComponent(plan.id)}`, { method: 'PATCH', body: JSON.stringify({ status: nextStatus }) });
          notice(nextStatus === 'active' ? 'New sales resumed.' : 'New sales paused. Existing subscriptions remain recorded.');
          await renderStudio(communityID);
          await renderStudioSetup();
        } catch (error) { notice(error.message, true); }
      });
      const archive = node('button', 'button danger', 'Archive'); archive.type = 'button';
      archive.addEventListener('click', async () => {
        const confirmed = tg?.showConfirm ? await new Promise((resolve) => tg.showConfirm('Archive this plan? It cannot be reactivated.', resolve))
          : window.confirm('Archive this plan? It cannot be reactivated.');
        if (!confirmed) return;
        try {
          await api(`/api/v1/creator/plans/${encodeURIComponent(plan.id)}`, { method: 'PATCH', body: JSON.stringify({ status: 'archived' }) });
          notice('Plan archived for new sales.'); await renderStudio(communityID); await renderStudioSetup();
        } catch (error) { notice(error.message, true); }
      });
      controls.append(toggle, archive); card.append(controls);
    }
    section.append(card);
  }
  const create = node('details', 'setup-details');
  create.append(node('summary', '', 'Add a membership plan'));
  const createForm = node('form', 'setup-form');
  createForm.append(formField('Plan name', 'name'), formField('Price in Stars / 30 days', 'price_stars', 'number'),
    formField('Description', 'description', 'textarea', false), formField('Benefits', 'benefits', 'textarea', false),
    formField('Renewal and cancellation terms', 'terms', 'textarea'), node('button', 'button full', 'Create plan'));
  createForm.addEventListener('submit', async (event) => {
    event.preventDefault();
    try {
      const values = Object.fromEntries(new FormData(createForm));
      await api(`/api/v1/creator/communities/${encodeURIComponent(communityID)}/plans`, { method: 'POST', body: JSON.stringify({ ...values, price_stars: Number(values.price_stars) }) });
      notice('Membership plan created.'); await renderStudio(communityID); await renderStudioSetup();
    } catch (error) { notice(error.message, true); }
  });
  create.append(createForm); section.append(create); content.append(section);
}

async function renderStudio(id) {
  state.activeCommunityId = id;
  const content = byId('studio-content');
  content.replaceChildren(node('p', 'muted', 'Loading your community…'));
  try {
    const [overview, members, planData] = await Promise.all([
      api(`/api/v1/creator/communities/${encodeURIComponent(id)}/overview`),
      api(`/api/v1/creator/communities/${encodeURIComponent(id)}/members`),
      api(`/api/v1/creator/communities/${encodeURIComponent(id)}/plans`),
    ]);
    content.replaceChildren();
    const selector = node('select', 'select');
    selector.setAttribute('aria-label', 'Choose a community');
    for (const community of state.creatorCommunities.filter((item) => item.workspace_id === state.activeWorkspaceId)) {
      const option = node('option', '', community.title);
      option.value = community.id;
      option.selected = community.id === id;
      selector.append(option);
    }
    selector.addEventListener('change', () => renderStudio(selector.value));
    content.append(selector);
    const chosen = state.creatorCommunities.find((item) => item.id === id);
    if (chosen?.status === 'published') {
      const enrollment = new URL(`/app/?community=${encodeURIComponent(id)}`, window.location.origin).href;
      const link = node('div', 'detail-card');
      link.append(node('div', 'label', 'Enrollment link'), node('p', '', enrollment));
      const copy = node('button', 'button secondary', 'Copy link'); copy.type = 'button';
      copy.addEventListener('click', async () => {
        try { await navigator.clipboard.writeText(enrollment); notice('Enrollment link copied.'); }
        catch { notice('Copy the enrollment link shown above.'); }
      });
      link.append(copy); content.append(link);
    }
    if (chosen?.status === 'draft') {
      const repair = node('button', 'button secondary full', 'Recheck connection and publish');
      repair.addEventListener('click', async () => {
        try { await api(`/api/v1/creator/communities/${encodeURIComponent(id)}/recheck`, { method: 'POST' }); notice('Community published.'); await loadStudio(); }
        catch (error) { notice(error.message, true); }
      });
      content.append(node('p', 'muted', 'This community is saved as a draft. Subscriber sales are not active.'), repair);
    }
    const metrics = node('div', 'metric-grid');
    const values = [
      ['Observed members', overview.observed_members, 'Known to MemberOS'],
      ['Known expiry', overview.known_expiry_count, 'Telegram observations'],
    ];
    if (overview.billing_metrics !== 'unavailable') {
      values.push(['Paid subscriptions', overview.billing_metrics.active_paid_subscriptions, 'Recorded bot payments']);
      values.push(['Net Stars recorded', overview.billing_metrics.recorded_gross_stars - overview.billing_metrics.recorded_refunded_stars, 'Gross less recorded refunds']);
    }
    for (const [label, value, caption] of values) {
      const metric = node('div', 'metric');
      metric.append(node('div', 'label', label), node('div', 'number', value), node('div', 'sub', caption));
      metrics.append(metric);
    }
    content.append(metrics);
    renderPlanManager(content, id, planData);
    content.append(node('div', 'divider'), node('div', 'section-heading'));
    content.lastChild.append(node('h2', '', 'Members'), node('span', 'muted', 'Latest 50 known members'));
    const list = node('div', 'member-list');
    if (!members.members?.length) list.append(node('div', 'empty', 'No members have been observed yet.'));
    for (const member of members.members || []) {
      const item = node('div', 'member-row');
      const name = node('div');
      name.append(node('strong', '', member.first_name || `Telegram user ${member.telegram_user_id}`),
        node('small', '', member.billing_status ? `${displayStatus(member.billing_status)} · ${displayStatus(member.access_status)}` : displayStatus(member.access_status)));
      item.append(name, node('span', 'muted', member.paid_through ? date(member.paid_through) : ''));
      list.append(item);
    }
    content.append(list);
    if (overview.billing_metrics === 'unavailable') content.append(node('p', 'muted', 'Native channel payment and revenue figures are unavailable through this bot.'));
    else content.append(node('p', 'muted', 'Stars figures use payment events recorded by MemberOS. They have not been reconciled with the bot balance.'));
  } catch (error) { content.replaceChildren(node('div', 'empty', error.message)); }
}


export { loadStudio };
