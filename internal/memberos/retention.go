package memberos

import (
	"context"
	"log/slog"
	"time"
)

// Retention periods. The Privacy Policy states these same figures, so change
// both together. Payment, refund, subscription and audit records are kept for
// accounting and dispute purposes and are not touched here.
const (
	webhookPayloadDays     = 90  // raw Telegram updates after processing
	stuckPayloadDays       = 180 // failed or quarantined updates awaiting review
	notificationTextDays   = 90  // text of delivered or abandoned bot messages
	resolvedInquiryMonths  = 12  // public creator contact form
	openInquiryMonths      = 24
	resolvedTicketMonths   = 24 // support ticket descriptions and resolutions
	unlinkedProfileMonths  = 12 // Mini App visitors who never joined, paid or created
	retentionCheckInterval = 6 * time.Hour
)

const redacted = "[removed under the data retention policy]"

type retentionStep struct {
	name  string
	query string
	args  []any
}

func retentionSteps() []retentionStep {
	return []retentionStep{
		{"webhook payloads", `UPDATE telegram_updates SET payload=jsonb_build_object('redacted',true)
			WHERE NOT payload ? 'redacted' AND (
				(state IN ('processed','ignored','acknowledged') AND received_at<now()-make_interval(days => $1))
				OR (state IN ('failed','quarantined','answer_unknown') AND received_at<now()-make_interval(days => $2)))`,
			[]any{webhookPayloadDays, stuckPayloadDays}},
		{"notification text", `UPDATE notifications SET message=$2
			WHERE status IN ('sent','failed','uncertain') AND message<>$2
			AND coalesce(sent_at,last_attempt_at,run_after)<now()-make_interval(days => $1)`,
			[]any{notificationTextDays, redacted}},
		{"creator inquiries", `DELETE FROM creator_inquiries
			WHERE (status='resolved' AND coalesce(resolved_at,created_at)<now()-make_interval(months => $1))
			OR created_at<now()-make_interval(months => $2)`,
			[]any{resolvedInquiryMonths, openInquiryMonths}},
		{"support tickets", `UPDATE support_tickets SET description=$2,resolution=CASE WHEN resolution IS NULL THEN NULL ELSE $2 END
			WHERE status='resolved' AND description<>$2 AND updated_at<now()-make_interval(months => $1)`,
			[]any{resolvedTicketMonths, redacted}},
		{"unlinked profiles", `DELETE FROM telegram_profiles p WHERE p.verified_at<now()-make_interval(months => $1)
			AND NOT EXISTS (SELECT 1 FROM members m WHERE m.telegram_user_id=p.telegram_user_id)
			AND NOT EXISTS (SELECT 1 FROM workspace_members wm WHERE wm.telegram_user_id=p.telegram_user_id)
			AND NOT EXISTS (SELECT 1 FROM subscriptions s WHERE s.telegram_user_id=p.telegram_user_id)
			AND NOT EXISTS (SELECT 1 FROM support_tickets t WHERE t.telegram_user_id=p.telegram_user_id)`,
			[]any{unlinkedProfileMonths}},
	}
}

func (a *app) runRetention(ctx context.Context) {
	ticker := time.NewTicker(retentionCheckInterval)
	defer ticker.Stop()
	for {
		a.applyRetention(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// applyRetention runs each step on its own so one failure cannot block the rest.
func (a *app) applyRetention(ctx context.Context) map[string]int64 {
	counts := make(map[string]int64)
	for _, step := range retentionSteps() {
		tag, err := a.db.Exec(ctx, step.query, step.args...)
		if err != nil {
			slog.Error("retention step failed", "step", step.name, "error", err)
			continue
		}
		counts[step.name] = tag.RowsAffected()
		if tag.RowsAffected() > 0 {
			slog.Info("retention applied", "step", step.name, "rows", tag.RowsAffected())
		}
	}
	return counts
}
