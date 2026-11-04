const steps = [
  ['DISCOVER', 'Find your next community.', 'Browse independent communities and compare the current membership offer.', 'COMMUNITY', 'Capital Research Club', 'Premium research and private discussion', 'Explore community', 'A subscriber finds your community and reviews the offer before paying.'],
  ['SUBSCRIBE', 'Choose your membership.', 'Review the Stars price, renewal period, seller and plan terms.', '30-DAY MEMBERSHIP', '500 Stars', 'Recurring subscription through Telegram', 'Continue to Telegram', 'Telegram handles the payment. MemberOS waits for verified confirmation before granting access.'],
  ['ACCESS', 'Your place is ready.', 'A valid entitlement lets you request entry to the correct private community.', 'MEMBERSHIP STATUS', 'Paid through 30 Nov', 'Access request ready to approve', 'Request access', 'MemberOS checks the entitlement and records the actual Telegram membership separately.'],
  ['MANAGE', 'Stay in control.', 'See your paid-through date, renewal status and support options.', 'MY MEMBERSHIP', 'Active', 'Renewal and access status in one place', 'View membership', 'Subscribers can review membership and turn off renewal while retaining already-paid access.'],
];
for (const button of document.querySelectorAll('[data-step]')) {
  button.addEventListener('click', () => {
    const step = steps[Number(button.dataset.step)];
    if (!step) return;
    for (const item of document.querySelectorAll('[data-step]')) item.setAttribute('aria-pressed', String(item === button));
    for (const [id, value] of [['demo-kicker', step[0]], ['demo-title', step[1]], ['demo-body', step[2]], ['demo-card-label', step[3]], ['demo-card-value', step[4]], ['demo-card-caption', step[5]], ['demo-action', step[6]], ['demo-explanation', step[7]]]) {
      document.getElementById(id).textContent = value;
    }
  });
}

const form = document.getElementById('creator-contact');
form?.addEventListener('submit', async (event) => {
  event.preventDefault();
  const status = document.getElementById('contact-status');
  const button = form.querySelector('button[type=submit]');
  button.disabled = true;
  status.textContent = 'Sending…';
  try {
    const values = Object.fromEntries(new FormData(form));
    const response = await fetch('/api/v1/creator-inquiries', {
      method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(values),
    });
    if (!response.ok) throw new Error('Could not send your inquiry. Check the details and try again.');
    form.reset(); status.textContent = 'Inquiry received. The MemberOS team can review it in Ops.';
  } catch (error) { status.textContent = error.message; }
  finally { button.disabled = false; }
});

if (location.pathname === '/pricing') document.getElementById('pricing')?.scrollIntoView();
if (location.pathname === '/support') document.getElementById('contact')?.scrollIntoView();
