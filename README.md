# MemberOS

Go and PostgreSQL service for Telegram paid-community membership. The [product implementation checklist](docs/PRODUCT_IMPLEMENTATION_CHECKLIST.md) tracks the full blueprint and release gates; the [lifecycle specification](docs/MEMBEROS_SUBSCRIPTION_LIFECYCLE_ENGINE_V1.md) defines the subscription engine.

## Project structure

| Path | Responsibility |
| --- | --- |
| `cmd/memberos` | Executable entrypoint |
| `internal/identity` | Mini App signature verification and short-lived sessions |
| `internal/telegram` | Bot API client and membership types |
| `internal/models` | Validated plan terms and subscription period rules |
| `internal/eventing` | Shared invalid-event marker for quarantine decisions |
| `internal/store` | Embedded, ordered PostgreSQL migrations |
| `internal/memberos` | HTTP API, creator setup, checkout, webhook dispatch, and service wiring |
| `internal/memberos/billing` | Payment, subscription, refund, entitlement, and billing audit processing |
| `internal/memberos/access` | Telegram access jobs and entitlement expiry reconciliation |
| `web/miniapp` | Embedded subscriber and creator Mini App |
| `web/publicsite` | Embedded public homepage, walkthrough, and creator contact form |
| `docs` | Engineering specification and product boundaries |
| `scripts` | PostgreSQL backup and empty-target restore tools |

### File boundaries

| Area | Files |
| --- | --- |
| HTTP routes and session identity | `internal/memberos/http.go`, `session.go` |
| Creator setup | `workspaces.go`, `workspace_profile.go`, `onboarding.go`, `community_connection.go`, `community_verification.go` |
| Offers and checkout | `plans.go`, `checkout.go`, `precheckout.go` |
| Billing lifecycle | `internal/memberos/billing/` |
| Telegram access | `internal/memberos/access/` for access jobs and expiry; `webhook.go`, `webhook_worker.go`, `chat_member_event.go`, `join_events.go` for ingress and observations |
| Mini App | `web/miniapp/app.js` starts the app; `shared.js` holds helpers, `subscriber*.js` and `member_status.js` the member journey, `creator*.js` the creator setup and dashboard, `operations*.js` the admin screen |

## Current behavior

- Verifies Telegram Mini App `initData` and issues a 30-minute bearer session.
- Provisions a creator-owned private channel after checking Telegram owner and bot administrator rights, creates a native Stars subscription link, and registers the webhook.
- Stores webhook updates durably before acknowledgement; deduplicates by bot and update ID; processes `chat_member` observations.
- Refreshes a subscriber's status with `getChatMember` and shows known subscription expiry, observation time, and freshness.
- Exposes a creator overview of **observed** members. Telegram native-channel billing amounts, renewal events, and refunds are unavailable.
- Supports a separately provisioned managed mode for a private channel or supergroup: Stars invoice checkout, pre-checkout validation, verified payment and refund updates, subscription changes, entitlement projection, join approval, expiry reconciliation, and guarded removal.
- Serves a Telegram Mini App at `/app/` with community discovery, subscription status, native or managed checkout, join access, renewal cancellation, support requests, and a creator overview with known members.
- Creates creator workspaces from verified Telegram sessions. Managed communities can publish named plans with immutable price and terms versions; checkout and the first payment retain the purchased version. Creators can pause or archive new plan sales.
- The creator Manage tab can connect a private channel or supergroup after checking the signed-in chat owner and bot permissions. A webhook failure leaves a draft that the owner can recheck and publish later.
- the Manage tab shows saved onboarding progress, lets owners edit their public creator name and support contact, and manages named offers for managed communities. Owners can select a private channel with Telegram's native chat picker instead of finding its numeric chat ID; Telegram can prompt for the bot's invite permission, and MemberOS verifies ownership and bot permissions before connecting it. Owners can publish a new plan version, pause or resume new sales, archive a plan, and copy the enrollment link.
- the Manage tab also shows a searchable member directory, profile history, access overrides, verified payment records, support requests, CSV exports, and date-filtered observed metrics. The protected Admin screen exposes failed work, incident search, connection health, and Stars discrepancies.
- The public homepage at `/` explains the product and offers an interactive walkthrough and creator inquiry form. Admin can review those inquiries. Creators can save category, rules, and content guidance for subscriber onboarding.
- A free MemberOS account allows one connected community. An operator can grant a recorded pilot allowance; the database enforces its community limit. Paid MemberOS SaaS billing is not configured.
- Admin now provides recent webhook history, live webhook status inspection, assigned support escalation, audited dispute evidence, and a separate empty SaaS finance view. the Manage tab can recheck degraded bot permissions and requeue failed access work.
- The webhook inbox deduplicates by bot and update ID. Refund and subscription updates that arrive before their payment retry for up to 24 hours; billing event order protects cancellation from delayed renewal updates. Pre-checkout answers are claimed before calling Telegram and uncertain outcomes are surfaced to Admin. Notification sends are claimed before delivery; a lost response becomes `uncertain` and needs an operator decision before retrying, because Telegram does not provide an idempotency key for `sendMessage`. Native channel permissions are also checked periodically. These are code-level safeguards; live payment, outage, and replay behavior still require acceptance testing.

Managed mode uses the one configured bot per service deployment as the Stars recipient. The operator must verify that the bot belongs to the creator before enabling it. Automated creator-owned bot onboarding, retention campaigns, and Telegram live-flow validation are still outstanding. Financial dashboard figures cover only bot events recorded by MemberOS; the Admin reconciliation can compare those records with bot transaction observations, but no live reconciliation has been verified.

## Access and billing rules

- **Who has access** is defined once, by the `member_has_access` SQL function.
- **Renewal grace:** a member whose renewal is still on keeps access for 24 hours after the paid-through date, so a late Telegram charge or a webhook outage cannot remove them. Canceled or failed renewals get no grace.
- **One subscription per community:** checkout and pre-checkout refuse a member who already has a live subscription. If two invoices are still paid at the same moment, the charge is recorded, the second renewal is turned off, the member is told, and Admin shows an alert.
- **Removal:** only people who once held a subscription or an access grant are removed automatically. People already in a chat when it was connected are never banned; they appear as access mismatches for the creator to review.
- **Refunds:** a refund that ends access also turns off renewal at Telegram.
- **Restarts:** expiry and reconciliation wait for Telegram's redelivery queue to drain, for at most 30 minutes.
- **Admin alerts** count unresolved items only. Ordinary messages to the bot and users who blocked it need no action. An unchanged backlog sends one reminder a day, and each exception can be acknowledged with a note.

## Configuration

The service reads environment variables. For local use, fill in the Git-ignored `.env` and run `scripts/run-local.sh`; the script exports its values before starting the Go service. Do not commit `.env` or put real credentials in documentation.

| Variable | Purpose |
| --- | --- |
| `DATABASE_URL` | PostgreSQL connection URI with migration privileges |
| `TELEGRAM_BOT_TOKEN` | Bot token from BotFather |
| `TELEGRAM_BOT_ID` | Numeric ID of that bot |
| `TELEGRAM_WEBHOOK_SECRET` | Random webhook secret accepted by Telegram (letters, digits, `_`, `-`) |
| `SESSION_SECRET` | Random secret of at least 32 bytes for bearer sessions |
| `LISTEN_ADDR` | Optional listen address, default `:8080` |
| `TRUSTED_PROXY_CIDRS` | Comma-separated CIDR ranges of your reverse proxy or load balancer. Required behind a proxy: without it, anonymous visitors share one rate limit. Only list proxies you control, because `X-Forwarded-For` is believed from these addresses |
| `MANAGED_BILLING_ENABLED` | Set to `true` to permit new managed checkouts and managed provisioning |
| `BOT_STARS_RECIPIENT_ACK` | Must equal `TELEGRAM_BOT_ID` for managed mode; explicit acknowledgement of the receiving bot |
| `MANAGED_BILLING_WORKSPACE_ID` | Operator-approved workspace UUID allowed to connect managed billing through the configured recipient bot in the Mini App |
| `PUBLIC_WEBHOOK_URL` | HTTPS public origin used by the Manage tab connection and CLI provisioning to register the Telegram webhook |
| `TELEGRAM_BOT_USERNAME` | Optional bot username for public website links into the Telegram Mini App |
| `MEMBEROS_ADMIN_TELEGRAM_ID` | Optional numeric Telegram ID allowed to use the protected operations APIs; without it those APIs are disabled |
| `TERMS_URL`, `PRIVACY_URL`, `REFUND_POLICY_URL` | HTTPS links to your published Terms of Service, Privacy Policy and refund policy. Shown on the site footer, in the Mini App before checkout and in the `/terms` reply. Required when `MANAGED_BILLING_ENABLED=true` |

Operations APIs require a fresh Mini App bearer session for `MEMBEROS_ADMIN_TELEGRAM_ID`. `GET /api/v1/admin/updates`, `GET /api/v1/admin/access-jobs`, and `GET /api/v1/admin/notifications` list exceptions. Retrying an uncertain notification requires `acknowledge_duplicate_risk=true` and creates an audit record. `POST /api/v1/admin/reconcile-access` checks known members, while `POST /api/v1/admin/reconcile-stars` pages through the bot transactions returned by Telegram and records the operator. `GET /api/v1/admin/star-discrepancies` reports unmatched observations and recorded charges. `GET /api/v1/admin/refund-requests` lists requests; `POST /api/v1/admin/refund-requests/{id}/approve` calls `refundStarPayment` once and then waits for Telegram's refund update. An uncertain refund result remains `needs_review` and requires operator investigation before any further action.

For the one-time `provision` or `provision-managed` command also set `CREATOR_TELEGRAM_ID`, `CHANNEL_CHAT_ID`, `CHANNEL_PRICE_STARS` (1–10000), `CREATOR_NAME`, `SUPPORT_CONTACT`, `CHANNEL_TITLE`, and `CHANNEL_DESCRIPTION`. The creator ID must own the private channel or supergroup. Native mode requires the bot's `can_invite_users` right. Managed mode also requires `can_restrict_members` and the two managed billing variables above. Provisioning calls Telegram and creates an invite link; run it once per community.

Set `CREATOR_WORKSPACE_ID` during CLI provisioning to attach another verified community to an existing workspace owned by `CREATOR_TELEGRAM_ID`. Without it, CLI provisioning creates a new workspace. In the Mini App, a creator can create a workspace and connect a verified private chat. Managed billing setup in the Mini App is available only when the operator binds that workspace to the recipient bot through `MANAGED_BILLING_WORKSPACE_ID`; native channel setup does not require this binding.

```sh
# Apply migrations only (needs DATABASE_URL); suitable as a deploy release step:
go run ./cmd/memberos migrate
go run ./cmd/memberos provision
# Or, for a private channel or supergroup with the creator-owned billing bot:
go run ./cmd/memberos provision-managed
go run ./cmd/memberos
```

The service applies ordered migrations in `internal/store/migrations` at startup. For local verification with a restricted Go cache:

To create a PostgreSQL archive, set `DATABASE_URL` and run `scripts/backup-db.sh /secure/path/memberos.dump`. The script refuses to replace an existing file. To restore, create a separate empty database, set `RESTORE_DATABASE_URL` to its connection URI, and run `scripts/restore-db.sh /secure/path/memberos.dump`. The restore script refuses a database with tables in its public schema. Schedule encrypted offsite copies and exercise a real restore before production.

```sh
GOCACHE=/tmp/memberos-gocache GOPROXY=off go test ./...
GOCACHE=/tmp/memberos-gocache GOPROXY=off go vet ./...
# If installed, format and lint all Go packages:
rg --files -g '*.go' -0 | xargs -0 goimports -w
GOLANGCI_LINT_CACHE=/tmp/memberos-lintcache GOCACHE=/tmp/memberos-gocache GOPROXY=off golangci-lint run ./...
```

Set `TEST_DATABASE_URL` to an isolated PostgreSQL database to run the integration, regression and SQL checks. Without it these tests skip locally, so a plain `go test ./...` covers very little; with `CI` set they fail instead. `TestAllSQLPrepares` asks PostgreSQL to plan every SQL statement in the code. Do not point it at a database containing user data.

Set Telegram's Mini App URL to `https://YOUR_DOMAIN/app/`. A direct community link can append `?community=COMMUNITY_UUID`; a Telegram Mini App start parameter may use `c_COMMUNITY_UUID`. Telegram supplies `initData` when the app is opened there. Register `chat_member` and the payment update types as allowed updates; provisioning sets these for the configured bot. The public webhook URL must reach this service over HTTPS.

## HTTP endpoints

| Method and path | Description |
| --- | --- |
| `GET /health/live` | Process liveness |
| `GET /health/ready` | PostgreSQL readiness |
| `GET /` | Public MemberOS homepage, walkthrough, and creator contact entry |
| `POST /api/v1/creator-inquiries` | Rate-limited public creator inquiry intake |
| `POST /api/v1/telegram/session` | JSON `{ "init_data": "..." }`; returns bearer token |
| `GET /api/v1/communities/{id}` | Published channel profile, Stars price, native subscription link, seller/support |
| `GET /api/v1/me/membership?community_id={id}` | Bearer session required; live or last known Telegram access observation |
| `GET /api/v1/creator/communities/{id}/overview` | Bearer session and creator ownership required; observed membership counts |
| `GET /api/v1/communities` | Published community discovery list |
| `GET /api/v1/communities/{id}/plans` | Active offers with current version, price, benefits and terms |
| `GET, POST /api/v1/creator/workspaces` | List owned workspaces or create one from a verified Telegram session |
| `PATCH /api/v1/creator/workspaces/{id}` | Owner updates public creator name and support contact |
| `GET /api/v1/creator/workspaces/{id}/onboarding` | Saved setup checklist derived from workspace, chat and active offer records |
| `POST /api/v1/creator/workspaces/{id}/communities` | Owner connects a private Telegram chat after live owner/bot checks; managed mode requires approved Stars recipient workspace |
| `POST /api/v1/creator/communities/{id}/recheck` | Owner rechecks bot rights and republishes a saved draft after webhook recovery |
| `GET /api/v1/creator/communities/{id}/plans` | Owner/admin sees every plan status and its current version |
| `GET /api/v1/creator/communities` | Communities accessible to the signed-in creator |
| `POST /api/v1/creator/communities/{id}/plans` | Create a named managed-billing plan with first version; owner/admin only |
| `POST /api/v1/creator/plans/{id}/versions` | Publish a new price and terms version; owner/admin only |
| `PATCH /api/v1/creator/plans/{id}` | Set `active`, `paused`, or `archived`; owner/admin only |
| `GET /api/v1/creator/communities/{id}/members` | Up to 50 known members, scoped to the creator |
| `POST /api/v1/telegram/webhooks/{bot_id}` | Telegram webhook with configured secret header |
| `POST /api/v1/checkouts` | Bearer session and managed mode required; JSON `{ "community_id": "...", "plan_id": "..." }`, returns a recurring Stars invoice URL and purchased plan version |
| `POST /api/v1/me/subscriptions/{id}/cancel` | Subscriber disables future renewal for their managed subscription; paid access stays through its expiration |
| `GET /app/` | Telegram Mini App and public preview |

The Mini App should treat the returned bearer token as sensitive and keep it in memory. Native payment occurs on Telegram's paid channel link. Managed payment uses the invoice URL, followed by the managed join-request link once the backend reports an active entitlement. Refresh the membership endpoint after payment or joining. Native billing metrics remain unavailable even if membership is observed.
