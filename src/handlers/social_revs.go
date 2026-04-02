package handlers

import (
	"context"
	"fmt"

	"servertest/db"
)

const (
	revFriends      = "friends_rev"
	revFriendPending = "friend_pending_rev"
	revFriendSent   = "friend_sent_rev"
	revGroups       = "groups_rev"
	revGroupPending = "group_pending_rev"
	revGroupSent    = "group_sent_rev"
)

func ensureSocialMetaRow(ctx context.Context, userID string) error {
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO user_social_meta (user_id)
		VALUES ($1::uuid)
		ON CONFLICT (user_id) DO NOTHING
	`, userID)
	return err
}

func bumpSocialRevs(ctx context.Context, userID string, columns ...string) error {
	if userID == "" || len(columns) == 0 {
		return nil
	}
	if err := ensureSocialMetaRow(ctx, userID); err != nil {
		return err
	}

	setClause := ""
	for i, c := range columns {
		if i > 0 {
			setClause += ", "
		}
		setClause += fmt.Sprintf("%s = user_social_meta.%s + 1", c, c)
	}
	_, err := db.Pool.Exec(ctx, "UPDATE user_social_meta SET "+setClause+" WHERE user_id = $1::uuid", userID)
	return err
}

