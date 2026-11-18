# MemberOS pricing

Decision of 18 November 2026. Revisit after the first 20 paying creators.

## Plans

| Plan | Price | Allowance |
| --- | --- | --- |
| Free | 0 | 1 community |
| Pro | 750 Telegram Stars every 30 days (`SAAS_PRO_PRICE_STARS`) | Up to 5 communities (`SAAS_PRO_COMMUNITY_LIMIT`) |
| Early access (pilot) | Operator grant, not charged | Set by the operator; may include managed billing |

There is no commission on what members pay. MemberOS keeps no share of creator revenue.

## Why Stars, and why 750

- Telegram requires digital goods and services sold in bots and Mini Apps to be paid in Stars (currency `XTR`) and has suspended bots that took other payment for digital products. MemberOS Pro is a digital service sold inside the Mini App, so it is priced in Stars and paid as a Telegram Star subscription to the MemberOS bot.
- Star subscriptions support only a 30-day period, so there is no annual plan yet. A discounted 90- or 365-day plan would need a one-off invoice and manual renewal.
- Developers receive about US$0.013 per Star when withdrawing through Fragment, after Telegram's share. 750 Stars nets about **US$9.75**, which meets the US$10 target. Buyers pay more than that for the same Stars (about US$0.015 to US$0.02 each depending on where they buy them), so the price creators see is roughly US$11 to US$15. Public pages state the price in Stars and explain that local cost varies, rather than quoting a dollar figure that would often be wrong.
- If the target is instead "creators pay about US$10", use about 500 to 600 Stars, which nets about US$6.50 to US$7.80.

## Market reference (November 2026, mostly third-party sources)

| Product | Model |
| --- | --- |
| LaunchPass | US$29/month plus 3.5% of sales, plus Stripe fees |
| InviteMember | Flat monthly plans reported from about US$19, tiered by monthly sales volume |
| Tribute (Telegram) | Commission on sales, reported between 10% and 20% |
| Whop | About 3% platform fee plus processing; reviewers estimate about 7% all-in |
| Patreon | About 10% plus processing for new creators |

MemberOS Pro undercuts flat-fee competitors and charges nothing on sales. That suits a product that is Telegram-only, Stars-only, and in early access, and suits creators in markets where card-based tools are hard to use. A percentage fee is not practical anyway: in native channel mode, Telegram pays creators directly and MemberOS never sees the money.

## Non-payment policy (enforced in code)

- Pro renews automatically. Owners can cancel or resume in Settings; only the person who paid can change the renewal, because Telegram ties the subscription to their account.
- If a Pro period ends without a renewal, Pro continues for 7 days (`saasGraceDays`). The owner gets a notice when the period ends.
- After the grace period, the account returns to Free limits. **Existing communities, members and history are never archived or deleted for non-payment.** Only adding new communities beyond the Free limit is blocked.
- A refunded Pro payment ends Pro immediately unless another paid period covers it.

## Things to validate

- Ask the first creators whether 5 communities is the right Pro allowance, and whether a higher tier (for example 2,000 Stars for 20 communities) is wanted.
- Watch whether creators in Kenya and other target markets can buy Stars easily; if not, a non-Telegram payment route for the SaaS fee may be needed outside the Mini App, subject to Telegram's rules.
- Confirm the current Stars withdrawal rate from Telegram's `usd_rate` before each price review.
