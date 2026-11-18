import { state, node, el, api, notice, actionButton, field, submitForm, submitButton, fold, confirmSheet } from './shared.js';

function communityForm(context, guidance) {
  const form = el('form', 'form', field('Category', 'category', { required: false, maxLength: 80, value: guidance.category, placeholder: 'Trading, fitness, language…' }),
    field('Community rules', 'rules', { type: 'textarea', required: false, maxLength: 1800, value: guidance.rules }),
    field('Where should new members begin?', 'content_guide', { type: 'textarea', required: false, maxLength: 1000, value: guidance.content_guide,
      hint: 'We include this in the welcome message.' }),
    submitButton('Save'));
  submitForm(form, async (values) => {
    await api(`${context.root}/onboarding`, { method: 'PATCH', body: JSON.stringify(values) });
    notice('Saved.');
  });
  return fold('Community details', true, form);
}

function profileForm(context) {
  const { workspace } = context;
  const form = el('form', 'form', field('Your name or brand', 'name', { maxLength: 120, value: workspace.name, hint: 'Members see this as the seller.' }),
    field('How can members reach you?', 'support_contact', { minLength: 3, maxLength: 240, value: workspace.support_contact }), submitButton('Save'));
  submitForm(form, async (values) => {
    await api(`/api/v1/creator/workspaces/${encodeURIComponent(workspace.id)}`, { method: 'PATCH', body: JSON.stringify(values) });
    notice('Saved.');
    await context.refresh();
  });
  return fold('Your profile', false, form);
}

function preferencesForm(context) {
  const { workspace } = context;
  const language = node('select', 'input'); language.name = 'language';
  for (const [code, label] of [['en', 'English'], ['sw', 'Kiswahili']]) {
    const option = node('option', '', label); option.value = code; option.selected = workspace.language === code; language.append(option);
  }
  const zones = Intl.supportedValuesOf ? Intl.supportedValuesOf('timeZone') : [];
  const timezone = node('select', 'input'); timezone.name = 'timezone';
  for (const zone of [...new Set(['UTC', workspace.timezone || 'UTC', ...zones])]) {
    const option = node('option', '', zone.replaceAll('_', ' ')); option.value = zone; option.selected = zone === (workspace.timezone || 'UTC'); timezone.append(option);
  }
  const alerts = node('input'); alerts.type = 'checkbox'; alerts.checked = workspace.notifications_enabled;
  const form = el('form', 'form', el('label', 'field', node('span', '', 'Language'), language), el('label', 'field', node('span', '', 'Time zone'), timezone),
    el('label', 'toggle', node('span', '', 'Tell me when something needs my attention'), alerts), submitButton('Save'));
  submitForm(form, async (values) => {
    await api(`/api/v1/creator/workspaces/${encodeURIComponent(workspace.id)}/settings`, { method: 'PATCH',
      body: JSON.stringify({ timezone: values.timezone, language: values.language, notifications_enabled: alerts.checked }) });
    notice('Saved.');
    await context.refresh();
  });
  return fold('Preferences', false, form);
}

function dangerZone(context) {
  const { community, workspace } = context;
  const items = [];
  const unfinished = community.disconnect_unfinished;
  if (workspace.role === 'owner' && community.mode === 'managed_bot' && (community.status === 'published' || unfinished)) {
    items.push(node('p', '', unfinished ? 'Your community is closed. Cancel its old join link to finish.' : 'Close this community on MemberOS. This is only possible while it has never had a paying member.'),
      actionButton(unfinished ? 'Finish disconnecting' : 'Disconnect this community', 'button danger full', async () => {
        const sure = unfinished || await confirmSheet({ title: `Disconnect ${community.title}?`, body: "New people won't be able to join, and you can't reconnect the same chat afterwards.", confirm: 'Disconnect', danger: true });
        if (!sure) return;
        await api(`${context.root}/disconnect`, { method: 'POST' });
        notice('Community disconnected.');
        state.activeCommunityId = null;
        state.studioTab = 'home';
        await context.refresh();
      }));
  }
  if (workspace.role === 'owner') {
    const requested = !!workspace.deletion_requested_at;
    const remove = actionButton(requested ? 'Deletion requested' : 'Ask to delete my account', 'button danger full', async () => {
      const sure = await confirmSheet({ title: 'Ask to delete your account?', body: 'Our team will review it first. Payment records are kept during the review, and your members keep their access.', confirm: 'Send request', danger: true });
      if (!sure) return;
      await api(`/api/v1/creator/workspaces/${encodeURIComponent(workspace.id)}/deletion-request`, { method: 'POST' });
      notice("Request sent. We'll be in touch.");
      await context.refresh();
    });
    remove.disabled = requested;
    items.push(remove);
  }
  return items.length ? fold('Close or delete', false, ...items) : null;
}

async function renderSettings(content, context) {
  const { workspace } = context;
  const [guidance, account] = await Promise.all([api(`${context.root}/onboarding`), api(`/api/v1/creator/workspaces/${encodeURIComponent(workspace.id)}/saas`).catch(() => null)]);
  content.replaceChildren(communityForm(context, guidance));
  if (workspace.role === 'owner') content.append(profileForm(context), preferencesForm(context));
  if (account) {
    const full = account.communities_used >= account.community_limit;
    const add = actionButton('Add another community', 'button secondary full', async () => context.addCommunity());
    add.disabled = full || account.status !== 'active';
    content.append(el('div', 'card', el('div', 'section-title', node('h2', '', 'Your account'), node('span', `pill ${account.status === 'active' ? 'good' : 'warn'}`, account.status === 'active' ? (account.plan === 'pilot' ? 'Early access' : 'Free') : 'Paused')),
      node('p', '', `You're using ${account.communities_used} of ${account.community_limit} ${account.community_limit === 1 ? 'community' : 'communities'}.`),
      add, full ? node('p', '', 'Need more? Contact the MemberOS team.') : null));
  }
  const danger = dangerZone(context);
  if (danger) content.append(danger);
}

export { renderSettings };
