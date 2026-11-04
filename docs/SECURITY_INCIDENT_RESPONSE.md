# MemberOS security incident response

This is the operational procedure for a suspected compromise of MemberOS data, bot credentials, payment records, or member access. The configured MemberOS platform administrator is the incident lead until a named responder takes over. This procedure does not replace legal advice or breach-notification requirements.

## 1. Detect and record

Open an incident record with the time, reporter, affected environment, suspected workspaces, and current symptoms. Preserve the request IDs, deployment version, relevant Telegram update IDs, audit event IDs, and database backup identifiers. Restrict the record to responders; do not paste bot tokens, session secrets, or member payment details into chat.

Treat unauthorized access to a workspace, an exposed bot token, unexplained payment or refund actions, and widespread access failures as urgent. The incident lead assigns one person to containment and one to evidence collection when available.

## 2. Contain

- Stop affected new checkouts and community provisioning at the application or deployment boundary. Preserve existing financial rows and paid-through entitlements.
- If a bot token is suspected exposed, rotate it through BotFather, replace the managed secret, and re-register the webhook with a new secret. Verify the configured bot ID before resuming.
- Revoke compromised operator sessions and access credentials. Restrict administrative routes until authorization is verified.
- For an access-control failure, inspect current entitlements and Telegram membership before removing anyone. Do not bulk-ban members based solely on incomplete observations.

Record every containment action and its operator. Avoid deleting webhook inbox rows, payment rows, subscription events, audit events, or backups during investigation.

## 3. Assess and recover

Compare the durable webhook inbox, subscription events, payment charge IDs, Stars observations, access jobs, and actual Telegram membership for affected communities. Resolve unmatched charges and refund states through the protected Ops queue. Restore from a verified backup into an isolated database first; compare record counts and charge uniqueness before any production recovery.

Resume new sales only after the payment recipient, webhook, bot permissions, database readiness, and support path are confirmed. Replay failed updates and access jobs through the audited Ops controls. Check that paid access remains valid through its authoritative paid-through date.

## 4. Communicate

The incident lead maintains a timeline and a list of affected creators and subscribers. Prepare factual customer updates that distinguish confirmed impact from investigation. The responsible business and legal owners determine whether, when, and how to notify affected people and regulators under applicable law. Support staff must have a route for payment and missing-access reports before any checkout resumes.

## 5. Close and improve

Record the root cause, affected records, financial reconciliation result, recovery evidence, notification decisions, and follow-up owners. Review whether alerts, authorization checks, backups, and runbooks need changes. Close the incident only after the operator confirms access and payment reconciliation for the affected scope.
