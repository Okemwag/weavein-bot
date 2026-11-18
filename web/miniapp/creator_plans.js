import { node, el, api, notice, actionButton, field, submitForm, submitButton, fold, confirmSheet, stars } from './shared.js';

function planFields(plan = {}) {
  return [field('Monthly price in Stars', 'price_stars', { type: 'number', min: 1, max: 10000, value: plan.price_stars }),
    field('Short description', 'description', { type: 'textarea', required: false, maxLength: 2000, value: plan.description }),
    field('What members get', 'benefits', { type: 'textarea', required: false, maxLength: 2000, value: plan.benefits }),
    field('Renewal and cancellation terms', 'terms', { type: 'textarea', maxLength: 4000,
      value: plan.terms ?? 'Renews every 30 days through Telegram Stars. Cancel anytime; you keep access until the end of the period you paid for.' })];
}

function planCard(plan, context, redraw) {
  const paused = plan.status === 'paused';
  const card = el('div', 'card', el('div', 'section-title', node('h2', '', plan.name), node('span', `pill ${paused ? 'warn' : plan.status === 'active' ? 'good' : ''}`,
    { active: 'On sale', paused: 'Paused', archived: 'Retired' }[plan.status] || plan.status)),
  node('p', '', `${stars(plan.price_stars)} / month`), plan.description ? node('p', '', plan.description) : null);
  if (plan.status === 'archived') return card;
  const form = el('form', 'form', ...planFields(plan), submitButton('Save changes'));
  submitForm(form, async (values) => {
    await api(`/api/v1/creator/plans/${encodeURIComponent(plan.id)}/versions`, { method: 'POST', body: JSON.stringify({ ...values, price_stars: Number(values.price_stars) }) });
    notice('Saved. New members get these terms. Current members keep theirs.');
    await redraw();
  });
  const setStatus = async (status, message) => {
    await api(`/api/v1/creator/plans/${encodeURIComponent(plan.id)}`, { method: 'PATCH', body: JSON.stringify({ status }) });
    notice(message);
    await redraw();
  };
  const toggle = paused ? actionButton('Put back on sale', 'button secondary small', () => setStatus('active', 'This plan is back on sale.'))
    : actionButton('Pause sales', 'button secondary small', () => setStatus('paused', 'Sales paused. Current members are not affected.'));
  const retire = actionButton('Retire plan', 'button danger small', async () => {
    const sure = await confirmSheet({ title: `Retire ${plan.name}?`, body: "New members can't buy it, and you can't undo this. Current members are not affected.", confirm: 'Retire plan', danger: true });
    if (sure) await setStatus('archived', 'Plan retired.');
  });
  card.append(fold('Edit plan', false, form), el('div', 'button-row', toggle, retire));
  return card;
}

async function renderPlans(content, context) {
  const redraw = () => renderPlans(content, context);
  const data = await api(`${context.root}/plans`);
  if (data.mode === 'native_channel') {
    const plan = data.plans?.[0];
    content.replaceChildren(el('div', 'card', node('h2', '', plan ? `${stars(plan.price_stars)} / month` : 'Telegram sets your price'),
      node('p', '', 'Telegram handles payments for this channel, so the price is fixed on your subscription link. To change it, create a new paid link in Telegram.')));
    return;
  }
  const live = data.plans.filter((plan) => plan.status !== 'archived');
  const retired = data.plans.filter((plan) => plan.status === 'archived');
  content.replaceChildren(node('p', 'lead', "Members choose a plan when they join. Changing a plan never affects what current members pay."));
  if (!live.length) content.append(node('div', 'empty', 'Add your first plan so people can join.'));
  for (const plan of live) content.append(planCard(plan, context, redraw));
  const form = el('form', 'form', field('Plan name', 'name', { maxLength: 80, placeholder: 'Premium' }), ...planFields(), submitButton('Add plan'));
  submitForm(form, async (values) => {
    await api(`${context.root}/plans`, { method: 'POST', body: JSON.stringify({ ...values, price_stars: Number(values.price_stars) }) });
    notice('Plan added. It is on sale now.');
    await redraw();
  });
  content.append(fold('Add a plan', !live.length, form));
  if (retired.length) content.append(fold(`Retired plans (${retired.length})`, false, ...retired.map((plan) => planCard(plan, context, redraw))));
}

export { renderPlans };
