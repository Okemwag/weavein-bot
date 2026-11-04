package access

import (
	"context"
)

func (s Service) ExpireOverrides(ctx context.Context) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `UPDATE access_overrides SET active=false,revoked_at=now()
        WHERE active=true AND valid_until<=now() RETURNING id,community_id,telegram_user_id`)
	if err != nil {
		return err
	}
	type expired struct {
		id, community string
		user          int64
	}
	var items []expired
	for rows.Next() {
		var item expired
		if err := rows.Scan(&item.id, &item.community, &item.user); err != nil {
			rows.Close()
			return err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, item := range items {
		if _, err := tx.Exec(ctx, `INSERT INTO access_jobs(community_id,telegram_user_id,action,dedupe_key)
            VALUES($1,$2,'remove',$3) ON CONFLICT (dedupe_key) DO NOTHING`, item.community, item.user, "remove:override-expiry:"+item.id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO audit_events(creator_id,subject_telegram_user_id,action,details)
            SELECT creator_id,$2,'access_override_expired',jsonb_build_object('override_id',$3,'community_id',$1)
            FROM communities WHERE id=$1`, item.community, item.user, item.id); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
