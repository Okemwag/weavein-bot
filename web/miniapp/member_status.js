import { date } from './shared.js';

const present = (status) => ['member', 'restricted', 'administrator', 'creator'].includes(status);

// describeMembership turns account state into what the person needs to know and
// the one thing they should do next. action is join, open, subscribe or none.
function describeMembership(profile, membership, now = new Date()) {
  if (!membership) return { action: 'subscribe' };
  const access = membership.access_status;
  if (access === 'administrator' || access === 'creator') {
    return { tone: 'good', title: 'You run this community', detail: 'Owners and admins always keep access, with no payment needed.', action: 'none' };
  }
  if (profile.mode !== 'managed_bot') {
    if (!present(access)) return { action: 'subscribe' };
    return { tone: 'good', title: "You're a member",
      detail: membership.subscription_until ? `Your Telegram subscription runs until ${date(membership.subscription_until)}. Manage it in your Telegram settings.` : 'Open the channel from your Telegram chat list.',
      action: 'none' };
  }
  const until = membership.paid_through ? new Date(membership.paid_through) : null;
  const renew = membership.auto_renew_state;
  if (membership.entitled) {
    if (!present(access)) {
      return { tone: 'good', title: 'Paid. One step left', action: 'join', canRecheck: true,
        detail: "Tap Join the community, then send the join request in Telegram. You'll be approved automatically." };
    }
    if (membership.billing_status === 'no_subscription') {
      return { tone: 'good', title: "You're a member", detail: 'The community owner gave you free access.', action: 'open' };
    }
    if (renew === 'failed' || membership.billing_status === 'payment_failed') {
      return { tone: 'warn', title: "Your renewal didn't go through", action: 'open',
        detail: `You keep access until ${date(until)}. Make sure you have enough Stars so Telegram can try again.` };
    }
    if (until && until < now && renew === 'on') {
      return { tone: 'good', title: "You're a member", action: 'open',
        detail: "Your renewal is being processed. You keep access in the meantime, and there's nothing you need to do." };
    }
    if (renew === 'off') {
      return { tone: 'neutral', title: `You're in until ${date(until)}`, action: 'open',
        detail: "You turned off renewal, so you won't be charged again." };
    }
    return { tone: 'good', title: "You're a member", detail: `Renews on ${date(until)}.`, action: 'open' };
  }
  if (membership.billing_status === 'refunded') {
    return { tone: 'neutral', title: 'Your payment was refunded', detail: 'Your membership has ended. You can join again anytime.', action: 'subscribe' };
  }
  if (until && membership.billing_status !== 'no_subscription') {
    return { tone: 'neutral', title: `Your membership ended on ${date(until)}`, detail: 'Join again to get back in.', action: 'subscribe' };
  }
  if (present(access)) {
    return { tone: 'neutral', title: "You're already in this community", detail: "There's no membership on your account yet. You can subscribe below.", action: 'subscribe' };
  }
  return { action: 'subscribe' };
}

// listSummary is the one-line status on the My memberships list.
function listSummary(member, now = new Date()) {
  if (member.mode !== 'managed_bot') {
    return present(member.access_status) ? { pill: 'good', label: 'Member', detail: 'Managed in Telegram' } : { pill: '', label: 'Not a member', detail: 'Managed in Telegram' };
  }
  const until = member.paid_through ? new Date(member.paid_through) : null;
  if (member.entitled) {
    if (member.billing_status === 'payment_failed') return { pill: 'warn', label: 'Renewal failed', detail: `Access until ${date(until)}` };
    if (!present(member.access_status)) return { pill: 'warn', label: 'One step left', detail: 'Tap to join the community' };
    if (member.auto_renew_state === 'off') return { pill: 'warn', label: 'Ending soon', detail: `Access until ${date(until)}` };
    if (until && until > now) return { pill: 'good', label: 'Active', detail: `Renews on ${date(until)}` };
    return { pill: 'good', label: 'Active', detail: until ? 'Renewal in progress' : 'Free access' };
  }
  if (member.billing_status === 'refunded') return { pill: '', label: 'Refunded', detail: 'Membership ended' };
  return { pill: '', label: 'Ended', detail: until ? `Ended on ${date(until)}` : 'No membership' };
}

export { describeMembership, listSummary };
