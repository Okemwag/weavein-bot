# MemberOS Subscription Lifecycle Engine v1

Status: partially implemented. The native channel backend, gated managed billing engine, and Mini App UI are in this repository. Automated creator-owned bot onboarding, live Telegram validation, broader analytics, and retention remain planned. See the [product implementation checklist](PRODUCT_IMPLEMENTATION_CHECKLIST.md) for the complete release status. Reviewed against Telegram Bot API documentation on 2026-11-04.

## Decision and scope

The first deployable slice supports one creator-owned private **channel** per community and a native Telegram Stars subscription invite link. Telegram owns billing, renewal, and native channel admission/removal. MemberOS stores a verified Telegram member identity, observes channel membership, shows the subscriber's known membership status in a Mini App, and gives the creator a membership dashboard. It must not present native-channel revenue, renewal success, refunds, or individual payment history as verified data.

The same data model and event pipeline must accommodate a second, explicitly gated **managed billing** mode: a creator-owned bot issues recurring Stars invoices, MemberOS processes bot payment events and controls admission to a private channel or group. This mode is necessary for the complete paid lifecycle, including failed-payment handling and accurate bot-payment analytics. It is not enabled until webhook, bot ownership, permissions, and live payment tests pass. A platform-owned billing bot would receive the Stars, so it is not the default seller configuration.

V1 excludes annual Telegram billing, bundles, automatic creator payouts, revenue conversion to fiat, referral attribution, and subscriber-level native-channel billing reconciliation. All prices and revenue are denominated in Stars; no USD MRR is inferred from Stars.

## Trust and ownership boundaries

| Mode | Seller / Stars recipient | Source of paid state | Access authority | Reporting |
| --- | --- | --- | --- | --- |
| Native channel | Channel owner / channel balance | Telegram channel subscription; `getChatMember` can expose membership and `until_date` | Telegram | Observed members and known expiry only; billing figures unavailable |
| Managed bot | Creator-owned bot / bot balance | Verified `successful_payment`, `subscription`, and refund updates, plus bot Star transactions | MemberOS policy and Telegram admin operations | Recorded Stars payments, refunds, renewal outcomes, and actual access |

Creator setup must verify ownership of the channel and bot, record the receiving entity, check the bot is an administrator with required rights, and disclose the seller and refund/support contact before publishing a plan. Store bot tokens encrypted by a secret manager, separate by creator and environment. No dashboard user can read another creator's records.

## Core invariants

1. Identify users by Telegram numeric ID (`BIGINT`), never username. Accept Mini App `initData` only after server-side signature and `auth_date` verification. Reject missing user IDs, stale timestamps, duplicate security parameters, invalid signatures, and wrong bot context.
2. Payment state, subscription/renewal state, entitlement, and observed Telegram access are separate records. A successful payment never implies access was granted.
3. In managed mode, only a verified `successful_payment` for the expected bot, buyer, payload, `XTR` amount, and plan can grant a paid entitlement. A pre-checkout approval, client callback, or invoice creation cannot.
4. Cancellation disables future renewal but preserves the paid entitlement until its recorded expiration. A failed renewal does not erase an unexpired paid period.
5. A refund is a durable financial reversal. The product policy for access after refund is explicit and auditable; default v1 policy revokes the refunded period immediately if no other paid period covers the member.
6. Admission and removal jobs are idempotent. Never remove a member while any valid entitlement for that chat remains, or while the system cannot establish that it controls the member's access.
7. Native-channel subscriptions are observed, not converted into synthetic MemberOS payment rows. Unknown native billing history remains `unknown`.

## PostgreSQL model

All timestamps are `timestamptz` in UTC. All primary keys are UUIDs except Telegram IDs. Every creator-owned row carries `creator_id`; foreign keys enforce tenant consistency through application checks and composite keys or scoped queries. Use migrations and database constraints for the following logical tables:

| Table | Required fields and constraints |
| --- | --- |
| `creators` | `id`, display name, support contact, created/updated timestamps |
| `creator_users` | `(creator_id, telegram_user_id)` unique, role, verified_at |
| `billing_bots` | `id`, `creator_id`, Telegram bot ID unique, encrypted token reference, webhook secret reference, status, verified_at; never persist token plaintext |
| `communities` | `id`, `creator_id`, Telegram chat ID unique, type (`channel`, `supergroup`), ownership verification and admin capability snapshot |
| `members` | `id`, `creator_id`, `telegram_user_id`, profile fields, `init_data_verified_at`; unique `(creator_id, telegram_user_id)` |
| `plans` | `id`, `creator_id`, `community_id`, mode (`native_channel`, `managed_bot`), `price_stars`, `period_seconds=2592000`, billing bot ID nullable by mode, native invite-link reference nullable by mode, status and immutable price version |
| `checkout_attempts` | `id`, `creator_id`, `member_id`, `plan_id`, opaque random invoice payload unique, expected amount/currency/bot ID, status (`pending`, `paid`, `expired`, `rejected`), created/expires timestamps; managed only |
| `subscriptions` | `id`, `creator_id`, `member_id`, `plan_id`, provider mode, state, `paid_through`, `auto_renew_state`, `last_payment_at`, `last_failed_at`, `canceled_at`; unique live subscription identity per bot/payload where Telegram supplies one; native rows have `paid_through` only when observed |
| `payments` | `id`, `creator_id`, `subscription_id`, bot ID, Telegram charge ID unique per bot, invoice payload, amount Stars, `is_first_recurring`, provider expiration, paid_at, refund status; managed only |
| `refunds` | `id`, `payment_id`, Telegram charge ID unique per bot, amount Stars, observed_at, policy outcome |
| `entitlements` | `id`, `creator_id`, `member_id`, `community_id`, source (`managed_payment`, `native_observation`, `manual_exception`), source ID, `valid_from`, `valid_until`, status, reason; overlap allowed, access decision is union of valid rows |
| `access_observations` | `(community_id, telegram_user_id)` unique, Telegram member status, observed `until_date`, observed_at, source (`chat_member`, `getChatMember`) |
| `telegram_updates` | `(bot_id, update_id)` unique, update type, received_at, payload JSONB, processing state, attempts, last error, processed_at; retention and redaction policy required |
| `access_jobs` | `id`, `creator_id`, `community_id`, `telegram_user_id`, action, dedupe key unique, status, run_after, attempts, lease_until, last error, created/finished timestamps |
| `audit_events` | actor, creator, subject, action, before/after JSONB redacted, source update/job ID, happened_at; append only |

Use foreign keys and `CHECK` constraints for nonnegative Stars, positive periods, mode-specific fields, and allowed enums. Index updates/jobs by processing state and due time; index entitlements by `(community_id, member_id, valid_until)`. A subscription is a durable business identity; a renewal appends a payment and extends its paid-through time in one transaction. Never overwrite payment history.

## API and webhook contracts

| Route / handler | Behavior |
| --- | --- |
| `POST /api/v1/telegram/session` | Verify raw `initData` using the configured bot token and a bounded `auth_date` window; issue a short-lived, creator-scoped session. Do not trust `initDataUnsafe`. |
| `GET /api/v1/communities/:id` | Public profile, plan, seller/support disclosure, and mode. No private member data. |
| `GET /api/v1/me/membership?community_id=...` | Authenticated member's observed status, entitlement, `paid_through` if known, access state, and explicit data freshness. |
| `POST /api/v1/checkouts` | Managed mode only. Create pending attempt with a random 1–128 byte payload, immutable price snapshot, and `createInvoiceLink` for `XTR`, one price item, 30-day `subscription_period`. Return invoice URL. |
| `POST /api/v1/telegram/webhooks/:bot_id` | Verify `X-Telegram-Bot-Api-Secret-Token` against that bot; enforce body limit; durably insert `(bot_id, update_id)` before 2xx. Duplicate updates return 2xx without replaying effects. |
| `pre_checkout_query` | Validate payload, user, currency, amount, plan status, and bot; call `answerPreCheckoutQuery` within 10 seconds. This is a fast path, not an entitlement trigger. |
| `message.successful_payment` | In one transaction, claim charge ID, match checkout/subscription, append payment, set paid-through from Telegram `subscription_expiration_date`, update renewal state, create/extend entitlement, and enqueue access work. Quarantine unmatched or contradictory payments for review; do not silently grant. |
| `subscription` | Handle `canceled`, `active`, and `failed` for bot subscriptions using payload and user ID. `failed` starts recovery monitoring; `canceled` preserves paid-through; `active` restores auto-renew intent. No new paid period without a payment event. |
| `message.refunded_payment` | Match charge ID, append reversal once, recompute entitlement from remaining paid periods, and queue access reconciliation. Unknown charge goes to manual review. |
| `chat_join_request` | Managed mode: check current entitlement and approve only when eligible; record result and errors. Native mode does not claim to control Telegram's paid-link admission. |
| `chat_member` | Update observed Telegram access. The bot must be admin and explicitly subscribe to `chat_member` updates. |

Use HTTPS webhooks and set `allowed_updates` explicitly. Telegram retries failed webhook deliveries but retains updates for no longer than 24 hours; durable ingestion and operations alerts are required. For pre-checkout, respond within Telegram's 10-second deadline even when general webhook processing is asynchronous. Webhook authentication, replay deduplication, and authorization are separate controls.

## State transitions and policy

`subscription.state` is a compact projection, not the sole access decision. `auto_renew_state` is `unknown | on | off | failed`; `access_observations` represent actual Telegram state.

| Current | Event | New state | Entitlement/action |
| --- | --- | --- | --- |
| none | Checkout created | `pending` | No entitlement |
| pending/expired | First verified managed payment | `active` | Grant until Telegram-reported expiration; enqueue admission |
| active/cancel_scheduled/grace_period | Verified renewal | `active` | Extend to Telegram-reported expiration; keep access |
| active | `subscription=canceled` | `cancel_scheduled` | Set auto-renew off; keep access through paid-through |
| cancel_scheduled | `subscription=active` | `active` | Set auto-renew on; do not extend time |
| active/cancel_scheduled | `subscription=failed` | `payment_failed` | Keep existing paid-through; recovery notice if permitted |
| payment_failed | Paid-through passes | `grace_period` or `expired` | Default v1: no grace; optional configured grace must be shown as promotional access, not paid time |
| any | Refund of covering payment | `refunded` or prior valid state | Recompute all covering entitlements; reconcile access |
| expired/refunded | New verified payment | `reactivated`, then `active` | New entitlement and re-admission after unban if needed |
| active/cancel_scheduled/payment_failed | Paid-through passes with no other valid coverage | `expired` | Queue removal in managed mode only |

`suspended` is an administrative access override with actor, reason, and expiry; it never changes payment history. If Telegram omits a managed subscription expiration date, quarantine the payment and investigate; do not derive a paid-through date from local clock arithmetic. Out-of-order `subscription` updates must not reverse a newer payment-derived paid-through value. Store a Telegram event timestamp where the update includes one, and always store receipt time.

Native mode derives an **observed** membership projection from `chat_member` and targeted `getChatMember` calls. `ChatMemberMember.until_date` may provide a current subscription expiry; it is not a payment ledger. If absent, show `expiry unknown`. Telegram is responsible for native paid-link removal, so MemberOS does not run ban jobs for native subscribers.

## Access worker and reconciliation

Managed mode uses a transactional outbox in `access_jobs`: payment or entitlement transaction commits the desired access change and the job together. Workers claim due rows with `FOR UPDATE SKIP LOCKED`, use bounded retries with jitter, and send terminal failures to an operations queue. A provider timeout may mean the operation succeeded; re-read membership before retrying.

For admission, issue a join-request link, approve an eligible `chat_join_request`, then confirm via `chat_member` or `getChatMember`. For removal, lock the member/community decision, recompute all valid entitlements, check the bot's `can_restrict_members` right, and only then call `banChatMember`. Call `unbanChatMember` with `only_if_banned=true` before a later rejoin. `unbanChatMember` does not itself readmit the user. Never ban creator/admin accounts or members whose access is not controlled by MemberOS.

Run expiration reconciliation at least every 15 minutes and a daily targeted access audit for known managed subscribers. Reconciliation recomputes entitlement first, then compares it with observed access and enqueues a correction. It must never infer a failed charge from a missing webhook. A payment-gap alert, webhook backlog alert, permission-loss alert, and stuck-access-job alert are launch gates. For managed bots, `getStarTransactions` can support a separate charge-level financial reconciliation, subject to pagination and availability; reconcile by charge ID rather than inventing subscription history.

Native mode refreshes known members on opening the Mini App and on a bounded periodic schedule. It cannot discover every channel subscriber from a full member list through the Bot API, so counts represent observed members and must carry a coverage label. Do not label these counts total paid subscribers.

## Dashboard and subscriber experience

Subscriber screens: community profile, Stars price and seller disclosure, native invite link or managed invoice, pending-payment state, paid-through date when verified, actual Telegram access status, cancellation status, support contact, and rejoin flow. An invoice callback can prompt refresh, but only the backend can mark payment complete.

Creator dashboard v1: observed channel members, members with known expiry, access exceptions, bot permission health, and data freshness. Managed-mode analytics, when enabled: recorded Stars receipts/refunds, active paid entitlements, renewal success for a defined billing cohort, failed renewals, and access error counts. Metric definitions and denominators must be displayed; native-mode billing metrics are `unavailable`, never zero. No fictional metrics in production UI.

Transactional notifications use only eligible bot chats and documented Telegram messaging permissions. Record consent/ability to message and delivery outcome. Rate-limit renewal reminders and provide creator controls; failed delivery must not affect entitlement.

## End-to-end acceptance cases

1. Valid Mini App `initData` binds the correct numeric user ID; tampered, stale, and wrong-bot data are rejected. Reuse within the accepted timestamp window is treated as the same identity, not as proof of a new transaction or action.
2. Native subscription link joins a test channel; `chat_member` and `getChatMember` update observed state and optional `until_date`; no payment or MRR is fabricated.
3. Managed checkout rejects wrong bot, buyer, payload, amount, currency, stale plan, or duplicate pre-checkout; a successful payment activates exactly one charge/entitlement.
4. Duplicate and out-of-order webhook deliveries produce one payment and no entitlement regression.
5. Payment succeeds while bot lacks admission permission: financial state remains paid, access state remains pending/error, retry resumes after permission repair.
6. Cancellation mid-period preserves access until paid-through; reactivation changes renewal intent without extending paid-through.
7. Failed renewal before paid-through retains access; expiration after paid-through removes only if no other entitlement or grace remains.
8. Refund reverses the matching charge exactly once and applies the documented access policy.
9. Re-subscription after expiration unbans when needed and restores access after verified payment; old invoices cannot activate an unintended plan.
10. Worker timeout, Telegram 429, webhook outage, permission loss, and service restart recover through retry and reconciliation without double payment or wrongful ban.
11. Cross-creator IDs, tokens, updates, plans, analytics, and member records remain isolated.

Test with unit-level transition cases, PostgreSQL integration tests for constraints and concurrent delivery, a fake Telegram adapter for retry behavior, and a Telegram test-environment/live pilot for actual Stars and admin permissions. A local test cannot prove Telegram settlement or production webhook delivery.

## Delivery sequence and gates

1. Create migrations, tenant-scoped repositories, identity verifier, webhook inbox, and audit trail.
2. Pilot one native private channel and creator dashboard, displaying only observed membership and expiry. Verify seller disclosure and data freshness in the Mini App.
3. Add creator-owned billing bot onboarding, managed checkout, pre-checkout fast path, payment/refund/subscription handlers, and the access worker behind a disabled feature flag.
4. Run integration and Telegram pilot cases, verify bot rights and revenue recipient, then enable managed mode for one creator.
5. Add cohort analytics and retention campaigns only after the underlying financial and access data are complete enough to support each metric.

## Telegram references

- [Bot API: updates, payments, subscription changes, membership and webhook methods](https://core.telegram.org/bots/api)
- [Mini Apps: validating `initData`](https://core.telegram.org/bots/webapps)
- [Stars subscriptions: bot and channel behavior](https://core.telegram.org/api/subscriptions)
