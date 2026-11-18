const form = document.getElementById('creator-contact');
form?.addEventListener('submit', async (event) => {
  event.preventDefault();
  const status = document.getElementById('contact-status');
  const button = form.querySelector('button[type=submit]');
  button.disabled = true;
  status.textContent = 'Sending…';
  try {
    const response = await fetch('/api/v1/creator-inquiries', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(Object.fromEntries(new FormData(form))),
    });
    if (response.status === 429) throw new Error('Please wait a minute, then try again.');
    if (!response.ok) throw new Error("That didn't send. Please check your details and try again.");
    form.reset();
    status.textContent = "Thanks! We'll reply by email.";
  } catch (error) {
    status.textContent = error instanceof TypeError ? "We couldn't reach the server. Check your connection and try again." : error.message;
  } finally { button.disabled = false; }
});

if (location.pathname === '/pricing') document.getElementById('pricing')?.scrollIntoView();
if (location.pathname === '/support') document.getElementById('contact')?.scrollIntoView();
