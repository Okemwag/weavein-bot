import { tg, state, node, el, api, notice, actionButton, field, submitForm, submitButton, fold, confirmSheet, openTelegramLink, date, stars } from './shared.js';

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

const planLabel = (account) => (account.status !== 'active' ? 'Paused'
  : ({ pilot: 'Early access', pro: 'Pro', free: 'Free' })[account.plan] || 'Free');

// planCard shows the MemberOS plan, community allowance and Pro billing controls.
function planCard(context, account, isOwner, reload) {
  const full = account.communities_used >= account.community_limit;
  const add = actionButton('Add another community', 'button secondary full', async () => context.addCommunity());
  add.disabled = full || account.status !== 'active';
  const card = el('div', 'card', el('div', 'section-title', node('h2', '', 'Your plan'),
    node('span', `pill ${account.status === 'active' ? 'good' : 'warn'}`, planLabel(account))),
  node('p', '', `You're using ${account.communities_used} of ${account.community_limit} ${account.community_limit === 1 ? 'community' : 'communities'}.`));
  const offer = account.pro_offer;
  const sub = account.subscription;
  if (sub) {
    const line = { active: `Pro renews on ${date(sub.paid_through)}.`,
      cancel_scheduled: `Pro won't renew. It stays on until ${date(sub.paid_through)}.`,
      payment_failed: `Your last Pro payment didn't go through. Pro stays on for ${offer.grace_days} days after ${date(sub.paid_through)}; top up your Stars so Telegram can retry.` }[sub.state];
    if (line) card.append(node('p', sub.state === 'active' ? 'muted' : '', line));
    if (isOwner && sub.you_paid && sub.state !== 'payment_failed') {
      const cancelling = sub.state === 'active';
      card.append(actionButton(cancelling ? 'Cancel Pro renewal' : 'Turn Pro renewal back on', 'text-button', async () => {
        if (cancelling && !await confirmSheet({ title: 'Cancel Pro?', body: `Pro stays on until ${date(sub.paid_through)}. After that your communities keep running, but you can't add more than the Free plan allows.`, confirm: 'Cancel renewal', cancel: 'Keep Pro', danger: true })) return;
        try {
          await api(`/api/v1/creator/workspaces/${encodeURIComponent(account.workspace_id)}/saas/${cancelling ? 'cancel' : 'resume'}`, { method: 'POST' });
          notice(cancelling ? "Pro won't renew." : 'Pro renewal is back on.');
          await reload();
        } catch (error) { notice(error.message, true); }
      }));
    }
  } else if (account.lapsed_at && account.plan === 'free') {
    card.append(node('p', '', 'Pro has ended. Your communities and members are all still here; upgrade again to add more.'));
  }
  card.append(add);
  if (offer?.available && !sub && isOwner) {
    card.append(el('div', 'stack', node('p', '', `Pro: up to ${offer.community_limit} communities for ${stars(offer.price_stars)} every ${offer.period_days} days. Cancel anytime.`),
      actionButton(`Upgrade to Pro for ${stars(offer.price_stars)}`, 'button full', () => upgradeToPro(account, reload))));
  } else if (full && !sub && account.plan !== 'pilot') {
    card.append(node('p', '', 'Need more? Contact the MemberOS team.'));
  }
  return card;
}

async function upgradeToPro(account, reload) {
  const checkout = await api(`/api/v1/creator/workspaces/${encodeURIComponent(account.workspace_id)}/saas/checkout`, { method: 'POST' });
  if (!tg?.openInvoice) { openTelegramLink(checkout.invoice_url); return; }
  tg.openInvoice(checkout.invoice_url, async (outcome) => {
    if (outcome === 'cancelled') { notice("Payment cancelled. You haven't been charged."); return; }
    if (outcome === 'failed') { notice('The payment did not go through. Please try again.', true); return; }
    notice('Confirming your payment…');
    for (let attempt = 0; attempt < 8; attempt += 1) {
      await new Promise((resolve) => setTimeout(resolve, 2000));
      const latest = await api(`/api/v1/creator/workspaces/${encodeURIComponent(account.workspace_id)}/saas`).catch(() => null);
      if (latest?.subscription) {
        tg.HapticFeedback?.notificationOccurred?.('success');
        notice("You're on Pro. Thanks!");
        await reload();
        return;
      }
    }
    notice('Telegram is still confirming your payment. Check back in a moment.');
  });
}

async function renderSettings(content, context) {
  const { workspace } = context;
  const [guidance, account] = await Promise.all([api(`${context.root}/onboarding`), api(`/api/v1/creator/workspaces/${encodeURIComponent(workspace.id)}/saas`).catch(() => null)]);
  content.replaceChildren(communityForm(context, guidance));
  if (workspace.role === 'owner') content.append(profileForm(context), preferencesForm(context));
  if (account) content.append(planCard(context, account, workspace.role === 'owner', () => renderSettings(content, context)));
  const danger = dangerZone(context);
  if (danger) content.append(danger);
}

export { renderSettings };
