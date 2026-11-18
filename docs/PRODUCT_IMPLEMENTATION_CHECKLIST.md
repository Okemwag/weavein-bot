# MemberOS product implementation checklist

Based on **MemberOS — Complete Product Requirements & Development Checklist**, Product Blueprint v1.0, supplied 2026-11-04. Repository audit: 2026-11-04.

## How to read this checklist

- `[x]` means the deliverable has code in this repository and relevant local verification. It does **not** mean a live Telegram or production release has passed.
- `[ ]` means missing or incomplete. A `Partial` note names the existing slice; the box stays open until the full stated behavior works.
- P0 is the commercial release gate. P1 and P2 are tracked below as open backlog. An unchecked feature is a requirement, not a claim of implementation.
- Native paid-channel membership and bot-managed billing are separate modes. Native channel billing history is unavailable to this bot and must not be represented as recorded revenue. The managed mode has one configured receiving bot per service deployment; creator ownership of that bot is an operator attestation, not automated verification.

**Release assessment: not ready for real paying creators.** As of 2026-11-18, build, vet, lint, gofmt, JavaScript syntax checks and `go test -race ./...` against PostgreSQL 16 and 17 pass locally and in CI, including the integration, regression and SQL prepare suites. A local backup and restore of a fully migrated database has been exercised. None of the flows have passed live Telegram validation. No Telegram test-environment payment, real creator pilot, published legal policy, staging deployment or production support flow has been demonstrated.

## P0 — Public website, discovery and positioning

- [x] Embedded responsive public MemberOS site at `/`, inspected in desktop and mobile headless Chrome.
- [x] Homepage explains creator setup, subscriptions, access and operations.
- [ ] Validated MemberOS SaaS pricing page.
- [x] Interactive subscriber journey walkthrough with a direct Mini App preview link.
- [x] Configurable Telegram bot entry link opens the Mini App creator registration flow; live BotFather configuration remains open.
- [ ] Published Terms of Service, Privacy Policy, and Support pages. Partial: `TERMS_URL`, `PRIVACY_URL` and `REFUND_POLICY_URL` are linked from the site footer, the Mini App and `/terms` when set; the documents themselves are not written.
- [x] Public creator contact form stores rate-limited inquiries; protected Ops lists and resolves them with an audit entry.
- [x] Site links to the Mini App and to the configured bot when `TELEGRAM_BOT_USERNAME` is set. Production configuration remains open.

## P0 — Creator accounts, onboarding and workspaces

- [ ] Self-service creator registration with verified Telegram identity. Partial: the Mini App now creates a workspace and connects a private chat; live Telegram launch and complete onboarding remain unverified.
- [x] Editable creator name and support contact in Studio, scoped to the workspace owner and audited.
- [x] Creator workspace creation through an authenticated API, with owner membership and a migrated workspace for existing creators.
- [x] Saved creator setup steps now identify the next incomplete action and navigate to its workspace, profile, connection or offer control. Live Telegram onboarding acceptance remains a release gate.
- [ ] Multiple communities within one workspace, subject to a SaaS plan. Partial: Studio and CLI can attach several verified chats; SaaS limits are absent.
- [x] Persisted onboarding progress derived from saved workspaces, communities and active plans; Studio resumes from current server state.
- [x] Onboarding checklist showing missing workspace, profile, connection, publication and offer steps.
- [ ] Complete account settings. Partial: profile, timezone, notification toggle and language preference are saved in Studio; interface localization and operational preference enforcement remain.
- [x] Owner can request workspace deletion for support review while billing records remain intact.

## P0 — Telegram integration and community connections

- [ ] Documented, deployed MemberOS bot configured through BotFather. Partial: the app accepts a bot token and checks `getMe` during provisioning; no live bot setup is verified.
- [ ] Connect private groups, supergroups, and channels. Partial: native mode handles private channels; managed mode handles private channels and supergroups; basic groups are not supported.
- [x] Verify the connecting Telegram user is the chat owner during CLI and Studio provisioning (`community_verification.go`).
- [x] Store a unique numeric Telegram chat ID (`communities.telegram_chat_id`).
- [x] Verify the configured bot is an administrator during provisioning.
- [x] Check `can_invite_users` and, for managed mode, `can_restrict_members` before publishing.
- [x] Return a clear provisioning error for missing permissions.
- [x] Creator-facing recheck of owner/bot permissions and publication of a saved draft after webhook recovery. Live Telegram recovery remains unverified.
- [x] Periodic managed-community bot permission check records a degraded connection when required rights are missing. Live permission-loss recovery remains unverified.
- [x] Record unknown, healthy, degraded, and disconnected connection states; periodic checks classify a removed bot as disconnected.
- [x] Block attachment of a chat the signed-in creator does not own during provisioning.
- [ ] Safe disconnect that stops automation and preserves billing history.

## P0 — Communities, plans and pricing

- [ ] Community title, description, logo, and category. Partial: title and description are stored; logo and category are absent.
- [ ] Free, paid, and invitation-only community types. Current schema supports paid communities only.
- [x] Named managed-billing plans as separate entities, with a versioned offer; a legacy community price remains for compatibility.
- [x] Plan description, benefits, and terms fields.
- [ ] Plan-to-access-destination mapping. Current model supports one chat per community.
- [x] Code path for a 30-day recurring Stars offer in native and managed modes (`provision_native.go`, `provision_managed.go`, `checkout.go`). Live purchase remains a release gate.
- [x] Authenticated creator API to activate, pause, or archive new plan sales; archived plans cannot be reactivated through this route.
- [ ] Renewal terms presented before purchase. Partial: plan terms now appear in the Mini App; published refund and seller policies remain absent.
- [x] New managed checkouts snapshot an immutable plan version, amount, and terms; legacy checkout rows remain nullable for compatibility.
- [ ] Preserve the agreed price and terms through renewals. Partial: the first subscription stores its purchased version and invoice amount; provider-side renewal behavior needs live verification.

## P0 — Subscriber Mini App

- [ ] Live launch from the configured Telegram bot. `/app/` is served, but BotFather setup and live opening are unverified.
- [x] Server-verified Telegram `initData` and numeric user identity (`internal/identity`).
- [x] Creator details and active plans, including benefits and current terms, appear in the Mini App.
- [ ] Complete offer disclosure: price, interval, benefits, cancellation terms, and seller. Partial: these now appear from plan versions; published refund and legal policies are absent.
- [x] Client flow for a managed Stars invoice and native paid-channel link (`web/miniapp/app.js`). Live Stars checkout remains unverified.
- [x] Mini App shows pending, confirmed, failed and expired states from a server-backed checkout attempt; live Telegram checkout remains unverified.
- [x] Show the native link or managed join link when applicable.
- [x] My Memberships lists known managed memberships with paid-through dates and observed native access.
- [x] Renewal and cancellation options for each mode: managed mode turns off renewal in the app; native mode explains cancellation through Telegram's My Stars, because the bot cannot cancel a native channel subscription.
- [x] In-app support contact plus Terms, Privacy and Refund policy links before checkout and under Billing and support, shown when the operator configures their URLs. Managed billing refuses to start without all three.
- [ ] Verified mobile and light/dark Telegram layouts. Partial: one mobile light-theme preview was checked in headless Chrome; Telegram dark theme was not tested.
- [x] Reopening the Mini App reloads checkout and membership state and can reopen a pending invoice.

## P0 — Payments, billing and financial records

- [ ] Legally defined seller, proceeds recipient, and refund responsibility. Partial: the mode distinction is documented and a bot-recipient acknowledgement is required; creator bot ownership and legal terms remain unverified.
- [x] Managed Stars invoice generation with `XTR` and native paid-channel link code.
- [x] Unique server-generated checkout reference (`checkout_attempts.invoice_payload`).
- [x] Bind checkout to creator, community, buyer, amount, and receiving bot.
- [x] Validate current published community and amount at pre-checkout.
- [ ] Prove the 10-second pre-checkout response in Telegram's test environment. The synchronous handler and fake Telegram test exist.
- [x] Activate a managed entitlement only from a stored `successful_payment` event.
- [x] Persist Telegram charge IDs and payment timestamps.
- [x] Deduplicate updates and charges by bot/update and bot/charge IDs; the PostgreSQL lifecycle test covers duplicate charges.
- [x] Store checkout, subscription, entitlement, and observed Telegram access separately.
- [x] Store amounts as integer Stars with `XTR` validation.
- [ ] Authorized refund issuance and published policy. Partial: subscriber requests, protected admin approval, Telegram refund call, and refund-update confirmation are implemented; policy and live outcome handling remain unverified.
- [ ] Complete historical Stars reconciliation. Partial: an admin run now pages through bot transactions, matches incoming charge IDs and exposes discrepancies; live verification and large-history operational behavior remain.
- [x] Creator-scoped payment ledger and reconciliation status in Studio, backed by verified bot payment records.
- [x] Dedicated `saas_financial_events` ledger and protected SaaS finance view are separate from creator Stars payments. No paid SaaS provider is connected, so SaaS revenue is explicitly unrecorded.

## P0 — Subscription lifecycle

- [x] Create a managed subscription after a verified initial payment.
- [x] Use Telegram's reported subscription expiration as paid-through, rejecting missing or invalid dates.
- [x] Record a recurring payment and extend paid-through once per charge ID.
- [x] Subscriber cancellation endpoint calls `editUserStarSubscription` before setting renewal off.
- [x] Keep the existing paid entitlement through cancellation; PostgreSQL test covers this.
- [x] Record `subscription=failed` separately from paid-through.
- [x] Distinguish failed renewal from actual entitlement expiration.
- [x] Mark due entitlements expired and enqueue managed access removal.
- [ ] Prove timely revocation with Telegram test-environment and permission-loss cases. Local worker logic exists.
- [ ] End-to-end reactivation after a fresh payment. Partial: renewed payment can unban, but the full rejoin flow has no live test.
- [x] Recalculate entitlement when a matching refund event is recorded.
- [ ] Complete immutable subscription event history. Partial: an append-only `subscription_events` table records new transitions and migration 013 reconstructs historical payment/refund facts; unobserved historical failures and cancellations cannot be reconstructed safely.
- [x] Ops can inspect and replay failed webhook/access work; a creator can recheck degraded bot permissions and requeue failed access jobs with an audit record. Live recovery remains unverified.
- [ ] Full delayed/out-of-order event handling. Partial: subscription update IDs prevent one regression path; refunds arriving before the charge are quarantined for operator-reviewed replay.

## P0 — Telegram access control

- [x] Generate a paid native invite link or managed join-request link during provisioning.
- [x] Bind a managed join request to the Telegram numeric user ID.
- [x] Recheck current entitlement before approving a managed join request.
- [x] Queue eligible approval work.
- [x] Ineligible join requests receive a best-effort message through Telegram's short-lived join-request chat before decline, with a private-bot-contact fallback and direct rejoin link. Telegram delivery still depends on its timing and messaging permissions.
- [x] Approval jobs await a Telegram member event or membership reconciliation before being marked done.
- [x] Retry failed Telegram access calls with bounded attempts.
- [x] Periodic checks classify degraded or disconnected managed connections and queue an alert for reachable creator owners.
- [ ] Proven safe expiration and removal in Telegram. Local jobs check current entitlements and protect creator/admin accounts.
- [ ] Proven unban and rejoin after reactivation. Local unban code exists.
- [x] Scope access jobs by stored community chat and configured billing bot.
- [x] Protect Telegram creator and administrator accounts from automatic removal.
- [ ] Voluntary leave and re-entry workflow with live tests. Join requests can be processed again locally.
- [ ] Complete expected-versus-observed access reconciliation. Partial: rotating known-member reads queue managed grant/removal jobs and expose mismatches; no full Telegram member list is available and live recovery is unverified.
- [x] Creator-scoped temporary access overrides are audited, expire automatically, and are enforced by join and access workers.

## P0 — Subscriber communication and onboarding

- [x] Verified first payments queue a deduplicated confirmation for reachable private bot contacts.
- [x] Telegram member confirmation queues a deduplicated welcome message for reachable managed members.
- [x] Payment and welcome notices include the managed join link when the bot can message the subscriber.
- [x] Creators can save community rules and content guidance; the Mini App displays them and managed members receive them in the welcome notice with support contact.
- [x] A scheduler queues one three-day reminder per paid-through date for opted-in reachable members.
- [x] Verified recurring payments queue a deduplicated renewal confirmation for reachable members.
- [x] Failed subscription updates queue a notice with the existing paid-through date and Mini App recovery direction.
- [x] Cancellation flows queue the final paid-through date when a private bot chat exists.
- [x] Successful managed removal queues a deduplicated notice for reachable members.
- [x] Expiry notice for reachable members includes a direct community re-subscription link when the public HTTPS origin is configured.
- [x] Dedupe queued bot command replies with a unique update key.
- [x] Durable bot command reply queue records bounded attempts and delivery errors.

The notification worker now handles private-chat bot commands and lifecycle notices. Users must have contacted the bot; expiry reminders honor a subscriber preference. Live delivery remains unverified.

## P0 — Subscriber CRM and administration

- [x] Creator directory supports scoped name, username and numeric-ID search with pagination.
- [x] Store numeric Telegram identity and verified profile fields for members who open the Mini App.
- [x] Creator directory and member profile show the purchased plan and community context.
- [x] Show known subscription state in managed mode and observed access in both modes.
- [x] Member profile shows known managed subscription start and paid-through history.
- [x] Member profile shows verified payment and renewal-event history.
- [x] Show latest observed Telegram access status when available.
- [x] Creator directory is scoped by community and filters by plan and subscription status.
- [x] Creator can open an individual profile with access, subscriptions, payments and events.
- [x] Authorized creator roles can add and read private, audited member notes in their community.
- [x] Creator can grant or revoke temporary managed access with an audit trail and automatic expiry.
- [x] Creator-scoped member CSV export respects filters and neutralizes spreadsheet formulas.

## P0 — Creator dashboard and analytics

- [x] Overview counts distinct currently paid Telegram user IDs.
- [x] Display observed member count with a coverage label.
- [x] Overview counts distinct first-time paying users in a selected period.
- [x] Overview counts recorded cancellation and entitlement-expiry events by selected period.
- [ ] Renewal success rate with a defined denominator and complete event coverage.
- [ ] Reconciled revenue and transaction totals. Partial: recorded gross and refunded Stars are shown, clearly labeled unreconciled.
- [x] Overview shows distinct active paying users grouped by purchased plan.
- [x] Overview counts known active entitlements without observed Telegram access.
- [x] Studio shows failed access job count and known access mismatches; Ops shows reconciliation exceptions.
- [x] Creator overview and payment CSV accept a bounded selected date range.
- [x] Label native billing metrics unavailable and managed Stars totals as recorded bot events.
- [x] Authorized creators can download filtered member and date-scoped payment CSV reports.

## P0 — MemberOS's own SaaS subscriptions

- [ ] Define and validate commercial plans.
- [ ] Free/trial and paid SaaS entitlements.
- [ ] Creator SaaS subscription status and enforcement.
- [x] Free and operator-granted pilot accounts enforce connected-community limits in PostgreSQL; managed-billing eligibility and account suspension gate new setup and checkout.
- [ ] Creator billing and plan-management screen.
- [ ] Supported SaaS billing integration and settlement contract.
- [ ] SaaS payment-failure handling that preserves creator data.
- [ ] Policy and workflow for a creator who stops paying MemberOS.

The Free/Starter/Growth/Business figures in the blueprint are hypotheses, not configured prices.

## P0 — Support, refunds and disputes

- [x] `/help`, `/support`, `/terms`, and `/paysupport` commands enqueue private-chat replies. Live bot acceptance remains unverified.
- [x] Verified Mini App users can file a support request for any published community; the creator can review and resolve it in Studio, and `/paysupport` directs users there. Live delivery remains unverified.
- [x] Mini App support form for any verified Telegram user viewing a published community, including before checkout and for native-channel offers.
- [x] Paid subscriber can report missing access through a creator-scoped support ticket.
- [x] Studio shows known access mismatches, recent payments, and support tickets; admin API lists Stars discrepancies.
- [ ] Complete controlled refund issuance. Partial: protected admin approval records reason and audit, then calls Telegram; live refund confirmation and published policy remain open.
- [x] Ticket resolution and refund review actions are recorded in audit and request records.
- [x] Ops flags overdue tickets, alerts the operator, and records an assigned, audited escalation reason before resolution. Live notification delivery remains unverified.
- [ ] Published refund and cancellation terms before checkout. Partial: managed checkout cannot be enabled until policy URLs are configured, and the Mini App links them above the pay action; the policy text is not written.
- [x] Protected, audited ticket evidence retrieval covers known checkout, payment, subscription and access records; `docs/DISPUTE_EVIDENCE.md` defines preservation and disclosure handling. Legal retention scheduling remains open.

## P0 — MemberOS internal administration

- [x] Protected Ops screen requires the configured platform administrator's verified Telegram session.
- [x] Ops lists known creators, workspaces and community connection state.
- [x] Ops searches known subscriptions by user, community, charge or subscription ID and shows failed access jobs.
- [x] Protected Ops view shows recent webhook processing history and failed/quarantined updates with audited replay controls; raw payloads stay withheld.
- [x] Ops combines queue, notice and community health with Telegram `getWebhookInfo` status, delivery backlog and recent errors. Live provider checks remain unverified.
- [x] Ops shows recent audit actions with sensitive details withheld.
- [x] Creator ticket list and resolution UI; protected operations queue for refund requests.
- [ ] Internal administrator roles and authorization.
- [x] Retries, refund approvals, support resolution, and Stars reconciliation starts record the acting operator; reconciliation run rows retain the operator ID.

## P0 — Security, privacy and legal

- [x] Verify Mini App `initData` server-side with HMAC and bounded `auth_date`.
- [ ] Complete replay policy. The current short time window limits old data; valid data can be reused within that window for identity.
- [x] Use numeric Telegram IDs in primary membership and billing decisions.
- [ ] Authorization on every workspace operation with full roles. Current creator reads check `creator_users`; creator mutation APIs and broad roles do not exist.
- [ ] Role-based access control beyond the owner/admin rows.
- [ ] Managed secret storage and rotation for bot tokens. Current token comes from an environment variable.
- [x] Keep bot tokens out of browser API responses and Mini App assets.
- [ ] Verified TLS deployment and at-rest protection policy. The service registers HTTPS webhooks, but no deployed environment is configured here.
- [x] Verify Telegram webhook secret header before ingesting updates.
- [x] Enforce unique update and charge IDs; PostgreSQL test covers a duplicate charge.
- [x] In-process bounded rate limiting covers public catalog, sessions, checkouts and support requests; distributed limits remain a deployment consideration.
- [ ] Personal-data retention and deletion policy, plus enforcement jobs.
- [ ] Backup schedule and tested restore. Partial: on 2026-11-18 a backup of a fully migrated database restored into an empty database with matching tables, functions and migrations, and both refusal guards worked; no production schedule or offsite copy exists.
- [ ] Legally reviewed Terms and Privacy Policy.
- [ ] Published dispute and refund policy.
- [ ] Kenyan and international data-protection assessment.
- [x] `docs/SECURITY_INCIDENT_RESPONSE.md` defines detection, containment, evidence preservation, recovery, communication and closure steps. Operator rehearsal remains a release gate.

## P0 — Backend infrastructure and reliability

- [x] Modular Go executable with separate identity, Telegram, lifecycle, and Mini App packages.
- [x] Ordered PostgreSQL migrations from 001_initial through 025_operations_resolution; `memberos migrate` applies them without starting the service.
- [ ] Full tenant-aware data model and repository boundary. Partial: explicit workspaces, plan ownership constraints, and tenant API tests exist; repository boundaries and comprehensive cross-tenant tests remain.
- [x] Durable Telegram webhook inbox before acknowledgement.
- [x] Reject invalid webhook secrets and deduplicate `(bot_id, update_id)`.
- [x] Record one payment per `(bot_id, telegram_charge_id)`.
- [ ] Complete provider-independent subscription state machine. Current transitions live in Telegram-specific handlers.
- [x] Separate entitlement records from payments and observed access.
- [x] Durable PostgreSQL access job queue with retries.
- [x] Protected operator APIs list and replay failed/quarantined updates and failed access jobs; retries are audited.
- [x] Separate billing event processing from Telegram access jobs.
- [ ] Complete expected-versus-observed access reconciliation. Partial: periodic known-member checks update observations and queue corrective jobs; live coverage and rate controls remain open.
- [x] API and health requests emit a response request ID and structured method, path, status and duration logs.
- [x] Liveness and database readiness endpoints.
- [x] `scripts/backup-db.sh` creates a restricted PostgreSQL archive and `scripts/restore-db.sh` restores into an empty target database. A scheduled backup and tested restore remain release gates.
- [ ] Complete API validation, authorization, and abuse controls. Partial: body limits and selected role checks exist.
- [ ] CI/CD pipeline and staging deployment. Partial: GitHub Actions checks formatting, vet, race tests against PostgreSQL and lint, and builds and smoke-tests the container image; no staging environment or deploy step exists.
- [x] Periodic operational notices to a reachable configured admin cover delayed/failed webhook work, failed access jobs and missing observed Stars charges after reconciliation; managed community permission changes alert reachable owners. Delivery and response still require live validation.

## P1 and P2 backlog

These are still open. A related P0 table or route does not imply that a growth or advanced feature works.

### Public website — P1

- [ ] Landing pages for educators, investment communities, course creators, and professional networks.
- [ ] Real customer testimonials, case studies, and product comparisons.
- [ ] Search-friendly help articles and integration guides.
- [ ] Affiliate or partner signup page.

### Creator accounts — P1

- [ ] Team invitations to a workspace.
- [ ] Owner, Administrator, Finance, Support, and Analyst roles.
- [ ] Multiple workspaces for agencies.
- [ ] Team action history.

### Telegram integration — P1

- [ ] Several groups or channels in one dashboard.
- [ ] One plan granting access to multiple Telegram destinations.
- [ ] Verified creator-specific bots with secure credential handling.
- [ ] Join-request Mini App or guard-bot onboarding.
- [ ] Optional creator branding in bot messages.

### Plans — P1

- [ ] Provider-compatible one-time access purchases.
- [ ] Free trials with explicit billing and expiry rules.
- [ ] Provider-compatible promotions and coupons.
- [ ] Upgrades and downgrades with effective-date and billing policy.
- [ ] Bundled access to multiple groups.
- [ ] Grandfathered plans.
- [ ] Maximum member count by offer.
- [ ] Complimentary staff or selected-user memberships.

### Plans — P2

- [ ] Membership gifts for another verified Telegram account.
- [ ] Additional billing periods only through providers that support them.
- [ ] Feasible metered digital memberships.

### Subscriber Mini App — P1

- [ ] All memberships in one place.
- [ ] Payment history and available receipts.
- [x] An expired managed membership can start a new Stars checkout from its membership screen; Telegram payment then runs through the normal verified activation path. Live reactivation remains a release gate.
- [ ] Notification preferences.
- [ ] Personalized onboarding and benefits.
- [ ] Referral links and available rewards.
- [ ] Community discovery only with creator consent. Partial: all published communities appear in the current public list, with no separate discovery consent.
- [ ] Localized language and time formatting. Partial: the browser formats dates in its locale; product copy is English only.

### Financial operations — P1

- [ ] Multiple verified billing integrations.
- [ ] Legitimately available settlement and payout-status reporting.
- [ ] Payment discrepancy and exception queue.
- [ ] Transaction exports.
- [ ] Supported Stars balance and revenue history.
- [ ] Separate discounts, fee estimates, and adjustments.
- [ ] Tax reporting fields without automatic tax-compliance claims.

### Financial operations — P2

- [ ] Compliant independent-web billing options.
- [ ] Creator revenue sharing after legal and settlement design.
- [ ] Accounting integrations.

### Subscription lifecycle — P1

- [ ] Cancellation reasons.
- [ ] Provider-compatible configurable grace periods.
- [ ] Plan upgrades and downgrades.
- [ ] Temporary administrative suspension without altering payment history.
- [ ] Verified complimentary extensions and manual entitlements.
- [ ] Failed-renewal recovery journeys.

### Access control — P1

- [ ] Multiple destinations per plan.
- [ ] Access templates for member tiers.
- [ ] Temporary creator-configured exceptions.
- [ ] Guard-bot Mini App onboarding.
- [ ] Detailed billing-versus-access mismatch reports.

### Communications — P1

- [ ] Creator-customized welcome messages.
- [ ] Safe message templates and personalization.
- [ ] Scheduled onboarding sequences.
- [ ] Renewal reminders and win-back messages.
- [ ] Subscriber communication preferences.
- [ ] Delivery statistics for supported notifications.
- [ ] Multilingual transactional notifications.

### Communications — P2

- [ ] Visual automation editor.
- [ ] Conditional workflows based on supported lifecycle and engagement data.
- [ ] Authorized email or other external messaging channels.

### Subscriber CRM — P1

- [ ] Tags and segments.
- [ ] Subscription tenure and historical spend.
- [ ] Acquisition-source tracking.
- [ ] Support assignments.
- [ ] Confirmed bulk administrative actions.
- [ ] Subscriber lifecycle timeline.
- [ ] Members-needing-attention queue.

### Subscriber CRM — P2

- [ ] Custom subscriber fields.
- [ ] Supported CRM integrations.
- [ ] Rule-based segmentation.

### Analytics — P1

- [ ] Stars-denominated MRR or a clearly defined equivalent.
- [ ] Gross and net revenue retention where data is sufficient.
- [ ] Subscriber and revenue churn.
- [ ] Average revenue per paying member.
- [ ] Subscription cohorts and renewal retention.
- [ ] Revenue by community and named plan.
- [ ] Acquisition-source comparison from verified attribution.
- [ ] Failed-payment recovery rates.
- [ ] Scheduled weekly and monthly reports.
- [ ] Clearly labeled forecast scenarios.

### Analytics — P2

- [ ] LTV estimates.
- [ ] Predictive churn based on sufficient history.
- [ ] Cross-community agency reporting.
- [ ] Custom report builder.

### Retention and referrals — P1

- [ ] Individual member referral links.
- [ ] Attributable referral conversions.
- [ ] Signup and payment conversion funnel.
- [ ] Canceled-future-renewal but still-active segment.
- [ ] Reactivation campaigns for expired subscribers.
- [ ] Creator-configured offers permitted by the payment provider.
- [ ] Churn and reactivation by cohort.
- [ ] Creator-configured cancellation questionnaires.
- [ ] Subscriber win-back journeys.

### Retention and referrals — P2

- [ ] Affiliate dashboard and controlled reward fulfillment.
- [ ] Eligible landing-page and onboarding A/B tests.
- [ ] Predictive retention segments.
- [ ] Multistep marketing automation.
- [ ] Customizable public community storefronts.

### MemberOS SaaS billing — P1

- [ ] SaaS plan upgrades and downgrades.
- [ ] Team-seat billing, if introduced.
- [ ] Usage-based pricing for high-volume creators.
- [ ] Creator SaaS invoices and exports.
- [ ] Self-service billing help.
- [ ] Agency and enterprise contracts.

### Support — P1

- [ ] Support ticket dashboard.
- [ ] Ticket assignments.
- [ ] Searchable help center and guides.
- [ ] Ticket-status notifications.
- [ ] Support resolution-time measures.
- [ ] AI-assisted reply suggestions with human review.

### Internal administration — P1

- [ ] Platform SaaS revenue view.
- [ ] Creator retention and churn monitor.
- [ ] Abuse and suspicious-use review.
- [ ] Controlled feature rollout flags.
- [ ] Usage against SaaS plan limits.
- [ ] Account suspension and appeals.
- [ ] Billing corrections with approvals and audit.

### Internal administration — P2

- [ ] Internal fraud and risk tools.
- [ ] Customer-success reporting.
- [ ] Automated administrative reports.

### Security and privacy — P1

- [ ] Additional protections for privileged login.
- [ ] Vulnerability and dependency scans.
- [ ] Bot-token rotation and revocation.
- [ ] Automated abuse detection.
- [ ] Periodic authorization and tenant-isolation reviews.
- [ ] Creator-facing data export and deletion controls.
- [ ] Internal data-processing inventory.

### Infrastructure — P1

- [ ] Distributed tracing.
- [ ] Performance dashboards.
- [ ] API usage analytics.
- [ ] Cache for high-read workloads when needed.
- [ ] Configurable worker concurrency.
- [ ] Operational backlog dashboards.
- [ ] Load and capacity tests.

### Infrastructure — P2

- [ ] Regional deployments where commercially justified.
- [ ] External MemberOS API.
- [ ] Developer webhooks and scoped API credentials.
- [ ] Disaster recovery and enterprise deployment options.

## Milestone and release gates

| Milestone | Current result | Next acceptance evidence |
| --- | --- | --- |
| 1. Backend foundation | **Partial:** workspaces, versioned plans, self-service chat connection code, core inbox, billing handlers, entitlements, jobs | Run new connection integration cases; add replayable event history and broader repository/tenant tests |
| 2. Telegram access engine | **Partial:** CLI provisioning, join jobs, expiry checks | Real permission-loss, approval, removal, re-entry and reconciliation tests |
| 3. Paid lifecycle | **Partial:** managed Stars code and local PostgreSQL test; native paid links | Test-environment payment ownership, renewal, cancellation, refund and balance reconciliation |
| 4. Mini App | **Partial:** embedded UI and local browser preview | Real Telegram launch, checkout, interrupted flow, support/terms, dark-theme and device tests |
| 5. Creator dashboard | **Partial:** guided setup, free/pilot allowance view, community connection, plan controls, searchable CRM, payment history, support, and observed analytics | Live onboarding and payment acceptance; SaaS billing and production permission recovery |
| 6. Commercial release | **Open** | Support, legal, SaaS billing, admin, monitoring, backup/restore, staging and three pilot communities |
| 7. Growth | **Open** | Verified cohort, referral and campaign data before growth claims |

### Commercial launch acceptance — all open

- [ ] Confirm the first production Stars recipient and creator ownership model.
- [ ] Run a real first payment, renewal, cancellation, failed renewal, refund, and reactivation in Telegram's test environment.
- [ ] Prove payment idempotency, access grant/removal, permission-loss recovery, and restart/replay behavior under operational tests.
- [ ] Reconcile creator bot Stars charges against supported Telegram financial data.
- [ ] Complete tenant-isolation and privileged-action security review.
- [ ] Publish Terms, Privacy, refund policy, and accessible payment support including `/paysupport`.
- [ ] Deploy staging with monitoring, alerts, and a tested database backup/restore.
- [ ] Complete end-to-end pilots with at least three creator communities and record their acceptance results.

### Immediate implementation order

1. Choose and verify the first production seller/recipient model with a creator-owned test bot or native channel; run a complete Telegram test-environment purchase and refund.
2. Validate Studio workspace, chat and plan onboarding against PostgreSQL and Telegram; the UI and APIs are implemented but tests were deferred.
3. Validate the new reliability migrations and run payment, access, retry, refund, notification-uncertainty, and pre-checkout interruption scenarios against PostgreSQL and Telegram; then add pagination and verify proactive permission-loss alerts live.
4. Publish legal, support and refund terms; verify `/paysupport`, tickets, and controlled refund decisions with a real bot.
5. Finish creator CRM, internal operations, backups, staging, security controls, and pilot acceptance gates.

## Repository evidence

- Identity: `internal/identity/identity.go`, `internal/identity/auth_test.go`
- Migrations: `internal/store/migrations/`
- Workspace, onboarding and plan APIs: `internal/memberos/workspaces.go`, `internal/memberos/workspace_profile.go`, `internal/memberos/onboarding.go`, `internal/memberos/plans.go`, `internal/models/plan.go`
- HTTP routing, sessions, community and member APIs: `internal/memberos/http.go`, `internal/memberos/session.go`, `internal/memberos/community_profile.go`, `internal/memberos/catalog.go`, `internal/memberos/membership.go`, `internal/memberos/overview.go`
- Telegram webhook ingress: `internal/memberos/webhook.go`
- Checkout and financial events: `internal/memberos/checkout.go`, `internal/memberos/precheckout.go`, `internal/memberos/billing/`, `internal/memberos/cancel.go`
- Access and membership: `internal/memberos/access/`, `internal/memberos/access_adapter.go`, `internal/memberos/chat_member_event.go`, `internal/memberos/join_events.go`, `internal/memberos/webhook_worker.go`
- Mini App: `web/miniapp/`
- Local lifecycle verification: `internal/memberos/integration_test.go`

The Telegram API assumptions in this checklist follow the [Bot API](https://core.telegram.org/bots/api), [Mini Apps documentation](https://core.telegram.org/bots/webapps), [Stars payment guide](https://core.telegram.org/bots/payments-stars), and [Star subscriptions documentation](https://core.telegram.org/api/subscriptions). Current implementation status comes from this repository, not from those external documents.
