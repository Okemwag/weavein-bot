package memberos

import (
	"context"
	"errors"
	"os"

	"github.com/jackc/pgx/v5"
)

// createProvisionWorkspace keeps CLI provisioning compatible with the explicit tenant model.
func createProvisionWorkspace(ctx context.Context, tx pgx.Tx, name, support string, ownerID int64) (string, string, error) {
	if existing := os.Getenv("CREATOR_WORKSPACE_ID"); existing != "" {
		var creatorID string
		err := tx.QueryRow(ctx, `SELECT w.creator_id FROM workspaces w JOIN workspace_members wm ON wm.workspace_id=w.id
			WHERE w.id::text=$1 AND wm.telegram_user_id=$2 AND wm.role='owner'`, existing, ownerID).Scan(&creatorID)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", errors.New("CREATOR_WORKSPACE_ID is not owned by CREATOR_TELEGRAM_ID")
		}
		return creatorID, existing, err
	}
	var creatorID, workspaceID string
	if err := tx.QueryRow(ctx, `INSERT INTO creators(display_name,support_contact) VALUES($1,$2) RETURNING id`, name, support).Scan(&creatorID); err != nil {
		return "", "", err
	}
	if err := tx.QueryRow(ctx, `INSERT INTO workspaces(creator_id,name) VALUES($1,$2) RETURNING id`, creatorID, name).Scan(&workspaceID); err != nil {
		return "", "", err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO creator_users(creator_id,telegram_user_id,role) VALUES($1,$2,'owner')`, creatorID, ownerID); err != nil {
		return "", "", err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO workspace_members(workspace_id,telegram_user_id,role) VALUES($1,$2,'owner')`, workspaceID, ownerID); err != nil {
		return "", "", err
	}
	return creatorID, workspaceID, nil
}
