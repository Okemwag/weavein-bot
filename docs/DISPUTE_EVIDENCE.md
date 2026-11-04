# MemberOS dispute evidence handling

Payment and access disputes are handled through a support ticket tied to a Telegram numeric user ID and community ID. An authorized platform operator can retrieve a bounded, audited evidence snapshot using `GET /api/v1/admin/support-tickets/{id}/evidence` in Ops. The snapshot contains MemberOS checkout attempts, verified bot payment charge IDs, subscription dates, access observations, and access job outcomes. It does not claim a complete native paid-channel billing history.

Financial payment rows and immutable subscription events are preserved as business records. Support tickets, refund requests, audit events, and webhook inbox records are retained while a dispute is open. Operators must not delete or alter source rows to resolve a case; corrections are appended or recorded as audited actions. Raw webhook payloads and the evidence snapshot must be shared only with authorized responders over approved channels. Exported copies should be minimized to the affected case and protected as personal data.

A legally reviewed retention schedule and deletion workflow are still required before commercial release. Until then, do not run an automated purge of financial or dispute evidence. Data access and disclosure decisions remain with the MemberOS operator and applicable legal obligations.
