import { tg, state, node, el, api, notice, setAction, setBack, field, submitForm, submitButton, fold } from './shared.js';

function stepHead(head, step, title, lead) {
  head.append(node('span', 'step-label', `Step ${step} of 2`), node('h1', '', title), node('p', 'lead', lead));
}

// Step 1: who members will see as the seller.
function renderProfileStep(head, content, reload) {
  stepHead(head, 1, 'Start your community', 'Tell members who they are joining. You can change this later.');
  const form = node('form', 'form card');
  form.append(field('Your name or brand', 'name', { maxLength: 120, placeholder: 'Sam Trading Club', hint: 'Members see this as the seller.' }),
    field('How can members reach you?', 'support_contact', { minLength: 3, maxLength: 240, placeholder: '@yourname or help@example.com', hint: 'Shown to members who need help.' }),
    submitButton('Continue'));
  submitForm(form, async (values) => {
    await api('/api/v1/creator/workspaces', { method: 'POST', body: JSON.stringify(values) });
    await reload();
  });
  content.replaceChildren(form);
}

function paymentOptions(workspace, account) {
  const allowed = workspace.managed_billing_enabled && account?.managed_billing_allowed;
  const option = (value, title, text, checked, disabled) => {
    const radio = node('input'); radio.type = 'radio'; radio.name = 'billing_mode'; radio.value = value; radio.checked = checked; radio.disabled = disabled;
    return el('label', 'option', radio, el('span', 'grow', el('span', 'head', node('span', '', title)), node('p', '', text)));
  };
  return el('div', 'stack', option('native_channel', 'Telegram paid channel', 'The simplest option. Telegram handles payments and who gets in. One price, channels only.', true, false),
    option('managed_bot', 'MemberOS memberships', allowed ? 'Offer several plans, handle refunds, and include groups. MemberOS manages who gets in.'
      : 'Offer several plans, handle refunds, and include groups. Ask the MemberOS team to turn this on for you.', false, !allowed));
}

// Step 2: connect a channel and set a price, on one screen with one main action.
async function renderConnectStep(head, content, workspace, connected, cancel, onConnected) {
  if (connected) {
    if (cancel) head.append(setBack(cancel, 'Your community'));
    head.append(node('h1', '', 'Add another community'));
  } else {
    stepHead(head, 2, 'Connect your channel', 'Choose your private channel and set your price.');
  }
  const id = encodeURIComponent(workspace.id);
  const account = await api(`/api/v1/creator/workspaces/${id}/saas`).catch(() => null);
  if (account && (account.status !== 'active' || account.communities_used >= account.community_limit)) {
    content.replaceChildren(el('div', 'empty', node('strong', '', account.status !== 'active' ? 'New communities are paused on your account' : "You've reached your community limit"),
      node('span', '', `You're using ${account.communities_used} of ${account.community_limit}. Contact the MemberOS team to add more.`)));
    return;
  }
  const form = node('form', 'form');
  const chatId = node('input'); chatId.type = 'hidden'; chatId.name = 'telegram_chat_id';
  const title = field('Community name', 'title', { maxLength: 120, hint: 'Members see this name.' });
  const titleInput = title.querySelector('input');
  const chosen = node('p', 'muted', 'No channel chosen yet.');
  const selectionPath = `/api/v1/creator/workspaces/${id}/chat-selection`;
  const showSelection = (chat) => {
    if (!chat) return false;
    chatId.value = String(chat.id);
    chosen.textContent = chat.issue ? `${chat.title || 'Your channel'}: ${chat.issue}` : `✓ ${chat.title || 'Your channel'} is ready to connect.`;
    if (chat.title && !titleInput.value) titleInput.value = chat.title;
    return true;
  };
  const choose = node('button', 'button secondary full', 'Choose my channel'); choose.type = 'button';
  const linkField = field('Link to any post in your channel', 'channel_post_link', { type: 'url', required: false, placeholder: 'https://t.me/c/1234567890/5' });
  if (tg?.requestChat) {
    choose.addEventListener('click', async () => {
      choose.disabled = true;
      try {
        const prepared = await api(selectionPath, { method: 'POST' });
        chatId.value = '';
        chosen.textContent = 'Choose your private channel in Telegram.';
        tg.requestChat(prepared.request_id, async (sent) => {
          if (!sent) { choose.disabled = false; chosen.textContent = 'No channel chosen yet.'; return; }
          for (let attempt = 0; attempt < 10 && !chatId.value; attempt += 1) {
            await new Promise((resolve) => setTimeout(resolve, 1000));
            const result = await api(selectionPath).catch(() => ({ chat: null }));
            showSelection(result.chat);
          }
          if (!chatId.value) chosen.textContent = "We haven't heard back from Telegram yet. Reopen this screen in a moment.";
          choose.disabled = false;
        });
      } catch (error) { notice(error.message, true); choose.disabled = false; }
    });
    api(selectionPath).then((result) => showSelection(result.chat)).catch(() => {});
  } else {
    choose.hidden = true;
    chosen.textContent = 'Update Telegram to pick your channel here, or paste a link to one of its posts below.';
  }
  form.append(el('div', 'card', node('h2', '', 'Your channel'),
    node('p', '', 'It must be private, and you must be its owner. Telegram will ask you to add MemberOS as an admin so it can let members in.'), choose, chosen),
    fold("Can't see your channel?", !tg?.requestChat, node('p', '', 'Open a post in your private channel, copy its link and paste it here. A group works too, with MemberOS memberships.'), linkField),
    el('div', 'card form', node('h2', '', 'About it'), title, field('What do members get?', 'description', { type: 'textarea', required: false, maxLength: 2000 }),
      field('Monthly price in Stars', 'price_stars', { type: 'number', min: 1, max: 10000, hint: 'From 1 to 10,000 Stars, charged every 30 days.' })),
    el('div', 'section-title', node('h2', '', 'How should members pay?')), paymentOptions(workspace, account), chatId);
  content.replaceChildren(form);
  const submit = async () => {
    if (!form.reportValidity()) return;
    const values = Object.fromEntries(new FormData(form));
    let selected = Number(values.telegram_chat_id);
    if (!selected && values.channel_post_link) {
      let match = null;
      try {
        const link = new URL(values.channel_post_link);
        if (['t.me', 'telegram.me'].includes(link.hostname)) match = /^\/c\/(\d+)\/\d+/.exec(link.pathname);
      } catch { match = null; }
      if (!match) throw new Error("That link doesn't look right. Copy the link to a post in your private channel.");
      selected = -1000000000000 - Number(match[1]);
    }
    if (!selected) throw new Error('Choose your private channel first.');
    const result = await api(`/api/v1/creator/workspaces/${id}/communities`, { method: 'POST', body: JSON.stringify({
      telegram_chat_id: selected, title: values.title, description: values.description,
      price_stars: Number(values.price_stars), billing_mode: values.billing_mode }) });
    notice(result.status === 'published' ? 'Your community is live. Share your link to get members.' : 'Almost there. We saved your community, and you can finish connecting it from Home.');
    await onConnected(result.id);
  };
  form.addEventListener('submit', (event) => { event.preventDefault(); });
  setAction({ label: 'Connect community', onClick: submit, note: state.creatorCommunities.length ? null : 'We check your channel before anything goes live.' });
}

export { renderProfileStep, renderConnectStep };
