# MemberOS product implementation checklist

Based on **MemberOS — Complete Product Requirements & Development Checklist**, Product Blueprint v1.0, supplied 2026-11-04. Repository audit: 2026-11-04.

## How to read this checklist

- `[x]` means the deliverable has code in this repository and relevant local verification. It does **not** mean a live Telegram or production release has passed.
- `[ ]` means missing or incomplete. A `Partial` note names the existing slice; the box stays open until the full stated behavior works.
- P0 is the commercial release gate. P1 and P2 are tracked below as open backlog. An unchecked feature is a requirement, not a claim of implementation.
- Native paid-channel membership and bot-managed billing are separate modes. Native channel billing history is unavailable to this bot and must not be represented as recorded revenue. The managed mode has one configured receiving bot per service deployment; creator ownership of that bot is an operator attestation, not automated verification.

**Release assessment: not ready for real paying creators.** Go build, vet, Go lint and JavaScript syntax checks pass for the current changes. Tests were skipped at the user's request; the new connection and onboarding paths have not passed PostgreSQL or live Telegram validation. An isolated PostgreSQL lifecycle test and headless browser preview passed in an earlier run; no Telegram test-environment payment, live webhook, real creator pilot, backup restore, or production support flow has been demonstrated.

## P0 — Public website, discovery and positioning

- [ ] Professional public MemberOS website. Partial: the embedded Mini App has a public preview at `/app/`.
- [ ] Homepage explaining the product and its benefits.
- [ ] Validated MemberOS SaaS pricing page.
- [ ] Live demo or interactive walkthrough.
- [ ] Creator signup and login entry points. Partial: Telegram Mini App sessions exist, while creator provisioning uses a CLI.
- [ ] Published Terms of Service, Privacy Policy, and Support pages.
- [ ] Creator contact form.
- [ ] Public links to a configured Telegram bot and Mini App.

## P0 — Creator accounts, onboarding and workspaces

- [ ] Self-service creator registration with verified Telegram identity. Partial: the Mini App now creates a workspace and connects a private chat; live Telegram launch and complete onboarding remain unverified.
- [x] Editable creator name and support contact in Studio, scoped to the workspace owner and audited.
- [x] Creator workspace creation through an authenticated API, with owner membership and a migrated workspace for existing creators.
- [ ] Creator onboarding wizard. Partial: Studio now has a saved setup checklist and forms for workspace, chat, profile, and offers; no step-by-step wizard or live Telegram acceptance has been completed.
- [ ] Multiple communities within one workspace, subject to a SaaS plan. Partial: Studio and CLI can attach several verified chats; SaaS limits are absent.
- [x] Persisted onboarding progress derived from saved workspaces, communities and active plans; Studio resumes from current server state.
- [x] Onboarding checklist showing missing workspace, profile, connection, publication and offer steps.
- [ ] Account settings for profile, language, notifications, and timezone.
- [ ] Creator deactivation and deletion-request flow.

## P0 — Telegram integration and community connections

- [ ] Documented, deployed MemberOS bot configured through BotFather. Partial: the app accepts a bot token and checks `getMe` during provisioning; no live bot setup is verified.
- [ ] Connect private groups, supergroups, and channels. Partial: native mode handles private channels; managed mode handles private channels and supergroups; basic groups are not supported.
- [x] Verify the connecting Telegram user is the chat owner during CLI and Studio provisioning (`community_verification.go`).
- [x] Store a unique numeric Telegram chat ID (`communities.telegram_chat_id`).
- [x] Verify the configured bot is an administrator during provisioning.
- [x] Check `can_invite_users` and, for managed mode, `can_restrict_members` before publishing.
- [x] Return a clear provisioning error for missing permissions.
- [x] Creator-facing recheck of owner/bot permissions and publication of a saved draft after webhook recovery. Live Telegram recovery remains unverified.
- [ ] Detect bot removal or demotion after setup.
- [ ] Connection health states: healthy, degraded, disconnected.
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
- [ ] Distinct pending, successful, and failed payment outcomes. Partial: invoice-close polling and a generic error message exist; no persistent payment-result screen.
- [x] Show the native link or managed join link when applicable.
- [ ] All active memberships and expiration dates in one home screen. Partial: one selected community can show its status and date.
- [ ] Renewal and cancellation options for each mode. Partial: managed mode can turn off renewal; native mode only shows observed expiry.
- [ ] In-app support, refund-policy, and terms links. Partial: support contact text is shown.
- [ ] Verified mobile and light/dark Telegram layouts. Partial: one mobile light-theme preview was checked in headless Chrome; Telegram dark theme was not tested.
- [ ] Interrupted-payment reopening flow. Partial: membership state is fetched on reopen, but there is no payment history or recovery screen.

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
- [ ] Authorized refund issuance and policy workflow. Partial: incoming `refunded_payment` is recorded and entitlement is recalculated; no creator/admin refund request path exists.
- [ ] Reconcile bot charges against `getStarTransactions` and investigate gaps.
- [ ] Creator payment ledger and history screen. Partial: dashboard shows aggregate recorded gross/refunded Stars.
- [ ] Separate MemberOS SaaS revenue records from creator Stars revenue. MemberOS SaaS billing is absent.

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
- [ ] Complete immutable subscription event history. Partial: raw Telegram updates and selected audit events exist; there is no dedicated `subscription_events` ledger.
- [ ] Authorized exception inspection and resolution interface.
- [ ] Full delayed/out-of-order event handling. Partial: subscription update IDs prevent one regression path; refunds arriving before the charge are quarantined, without replay resolution.

## P0 — Telegram access control

- [x] Generate a paid native invite link or managed join-request link during provisioning.
- [x] Bind a managed join request to the Telegram numeric user ID.
- [x] Recheck current entitlement before approving a managed join request.
- [x] Queue eligible approval work.
- [ ] Ineligible join-request explanation. Current code holds the request; it does not send guidance to the subscriber.
- [ ] Confirm admission as an explicit completed access outcome. `chat_member` observations are stored, but approval jobs finish before confirmed membership is observed.
- [x] Retry failed Telegram access calls with bounded attempts.
- [ ] Detect lost bot permissions and alert the creator.
- [ ] Proven safe expiration and removal in Telegram. Local jobs check current entitlements and protect creator/admin accounts.
- [ ] Proven unban and rejoin after reactivation. Local unban code exists.
- [x] Scope access jobs by stored community chat and configured billing bot.
- [x] Protect Telegram creator and administrator accounts from automatic removal.
- [ ] Voluntary leave and re-entry workflow with live tests. Join requests can be processed again locally.
- [ ] Reconcile expected and observed access beyond expiration. Only targeted membership reads and expiration processing exist.
- [ ] Audited creator access overrides.

## P0 — Subscriber communication and onboarding

- [ ] Idempotent payment confirmation message.
- [ ] Welcome message after **confirmed** access.
- [ ] Purchased community link in a transactional message.
- [ ] Community rules, content location, and support onboarding.
- [ ] Appropriate upcoming-expiry notice.
- [ ] Successful renewal confirmation.
- [ ] Failed-renewal notice and recovery path.
- [ ] Cancellation confirmation with final paid-through date.
- [ ] Access-revocation notice.
- [ ] Expired-member reactivation message and link.
- [ ] Notification deduplication.
- [ ] Notification delivery-attempt and error records.

No notification table, opt-in record, Telegram private-message eligibility check, or delivery worker is present.

## P0 — Subscriber CRM and administration

- [ ] Searchable subscriber directory. Partial: creator-scoped endpoint returns up to 50 known members without search or pagination.
- [x] Store numeric Telegram identity and verified profile fields for members who open the Mini App.
- [ ] Show the named subscribed plan and community. Partial: the purchased version is stored, but the membership view does not yet display its plan name.
- [x] Show known subscription state in managed mode and observed access in both modes.
- [ ] Full subscribed-at and paid-through history. Partial: latest paid-through is available.
- [ ] Creator-visible payment and renewal history per subscriber.
- [x] Show latest observed Telegram access status when available.
- [ ] Filter by plan, status, and community. Partial: route is scoped to one community.
- [ ] Individual subscriber profile.
- [ ] Private administrative notes with role controls.
- [ ] Manual access interventions with audit trail.
- [ ] Authorized CSV export.

## P0 — Creator dashboard and analytics

- [ ] Accurate **distinct** active paid subscriber count. Current aggregate counts subscription rows and can double-count one buyer with multiple subscriptions.
- [x] Display observed member count with a coverage label.
- [ ] New subscriptions in a selected period.
- [ ] Cancellations and expirations in a selected period.
- [ ] Renewal success rate with a defined denominator and complete event coverage.
- [ ] Reconciled revenue and transaction totals. Partial: recorded gross and refunded Stars are shown, clearly labeled unreconciled.
- [ ] Paid counts by named plan. Plan versions exist, but the dashboard does not aggregate by plan.
- [ ] Subscribers awaiting access.
- [ ] Failed access jobs and reconciliation outcomes in the dashboard.
- [ ] Date-range filtering.
- [x] Label native billing metrics unavailable and managed Stars totals as recorded bot events.
- [ ] Downloadable reports.

## P0 — MemberOS's own SaaS subscriptions

- [ ] Define and validate commercial plans.
- [ ] Free/trial and paid SaaS entitlements.
- [ ] Creator SaaS subscription status and enforcement.
- [ ] Community and feature limits by SaaS plan.
- [ ] Creator billing and plan-management screen.
- [ ] Supported SaaS billing integration and settlement contract.
- [ ] SaaS payment-failure handling that preserves creator data.
- [ ] Policy and workflow for a creator who stops paying MemberOS.

The Free/Starter/Growth/Business figures in the blueprint are hypotheses, not configured prices.

## P0 — Support, refunds and disputes

- [ ] `/help`, `/support`, `/terms`, and `/paysupport` bot commands.
- [ ] Working support contact channel. Partial: creator support contact is displayed as text.
- [ ] Mini App support request form.
- [ ] Paid-but-no-access reporting flow.
- [ ] Creator discrepancy investigation interface.
- [ ] Controlled refund issuance with role, reason, and audit record.
- [ ] Support and refund action logs.
- [ ] Escalation path for unresolved payment issues.
- [ ] Published refund and cancellation terms before checkout.
- [ ] Dispute evidence retention and retrieval policy.

## P0 — MemberOS internal administration

- [ ] Protected internal administrator dashboard.
- [ ] Creator and community overview for MemberOS staff.
- [ ] Searchable subscription and access incidents.
- [ ] Webhook failures and retry/dead-letter interface.
- [ ] Integration health monitor.
- [ ] Non-sensitive audit-record viewer.
- [ ] Support request operations.
- [ ] Internal administrator roles and authorization.
- [ ] Audit of privileged staff actions.

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
- [ ] Rate limiting on public and payment endpoints.
- [ ] Personal-data retention and deletion policy, plus enforcement jobs.
- [ ] Backup schedule and tested restore.
- [ ] Legally reviewed Terms and Privacy Policy.
- [ ] Published dispute and refund policy.
- [ ] Kenyan and international data-protection assessment.
- [ ] Security incident response procedure.

## P0 — Backend infrastructure and reliability

- [x] Modular Go executable with separate identity, Telegram, lifecycle, and Mini App packages.
- [x] Ordered PostgreSQL migrations (`001_initial` through `004_workspaces_plans`).
- [ ] Full tenant-aware data model and repository boundary. Partial: explicit workspaces, plan ownership constraints, and tenant API tests exist; repository boundaries and comprehensive cross-tenant tests remain.
- [x] Durable Telegram webhook inbox before acknowledgement.
- [x] Reject invalid webhook secrets and deduplicate `(bot_id, update_id)`.
- [x] Record one payment per `(bot_id, telegram_charge_id)`.
- [ ] Complete provider-independent subscription state machine. Current transitions live in Telegram-specific handlers.
- [x] Separate entitlement records from payments and observed access.
- [x] Durable PostgreSQL access job queue with retries.
- [ ] Operator-visible dead-letter handling and replay for quarantined updates and failed jobs. Terminal states exist, but no recovery interface.
- [x] Separate billing event processing from Telegram access jobs.
- [ ] Complete expected-versus-observed access reconciliation. Current job checks and targeted reads cover only part of it.
- [ ] Structured request logs with correlation IDs. Partial: `slog` records worker/startup errors without request IDs.
- [x] Liveness and database readiness endpoints.
- [ ] Backup and restore tooling.
- [ ] Complete API validation, authorization, and abuse controls. Partial: body limits and selected role checks exist.
- [ ] CI/CD pipeline and staging deployment.
- [ ] Operational alerts for webhook lag, payment gaps, failed access, and permission loss.

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
- [ ] Self-service reactivation.
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
| 5. Creator dashboard | **Partial:** onboarding checklist, profile editor, community connection, named plan controls, overview and latest 50 known members | Live onboarding validation, CRM, payment history, access exceptions |
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
3. Add payment/Star-transaction reconciliation, subscription event history, dead-letter replay, access mismatch checks, and permission-loss alerts.
4. Publish legal/support/refund terms and build `/paysupport`, tickets, and controlled refund decisions.
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
