package memberos

import (
	"context"

	"github.com/jackc/pgx/v5"
)

func createDefaultPlan(ctx context.Context, tx pgx.Tx, workspaceID, communityID string, price int, description, terms string) error {
	var planID string
	if err := tx.QueryRow(ctx, `INSERT INTO plans(workspace_id,community_id,name) VALUES($1,$2,'Membership') RETURNING id`, workspaceID, communityID).Scan(&planID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO plan_versions(plan_id,version,price_stars,description,terms) VALUES($1,1,$2,$3,$4)`, planID, price, description, terms)
	return err
}
