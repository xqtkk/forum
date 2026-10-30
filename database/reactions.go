package database

import (
	"database/sql"
	"fmt"
)

type ReactionCounts struct {
	Likes    int    `json:"likes"`
	Dislikes int    `json:"dislikes"`
	Reaction string `json:"reaction"`
}

func ReactionState(db *sql.DB, kind string, id, userID int) (ReactionCounts, error) {
	var state ReactionCounts
	table := "posts"
	key := "post_id"
	if kind == "comment" {
		table = "comments"
		key = "comment_id"
	} else if kind != "post" {
		return state, fmt.Errorf("invalid reaction target")
	}
	err := db.QueryRow("SELECT liked,disliked,COALESCE((SELECT reaction_type FROM "+kind+"_reactions WHERE "+key+"=? AND user_id=?),'') FROM "+table+" WHERE id=?", id, userID, id).Scan(&state.Likes, &state.Dislikes, &state.Reaction)
	return state, err
}
func toggleReaction(db *sql.DB, kind string, id, userID int, reaction string) error {
	if reaction != "like" && reaction != "dislike" {
		return fmt.Errorf("invalid reaction")
	}
	table, key, ownerColumn := "posts", "post_id", "author_id"
	if kind == "comment" {
		table, key, ownerColumn = "comments", "comment_id", "user_id"
	} else if kind != "post" {
		return fmt.Errorf("invalid target")
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var owner, postID int
	if kind == "post" {
		postID = id
		err = tx.QueryRow("SELECT "+ownerColumn+" FROM "+table+" WHERE id=?", id).Scan(&owner)
	} else {
		err = tx.QueryRow("SELECT user_id,post_id FROM comments WHERE id=?", id).Scan(&owner, &postID)
	}
	if err != nil {
		return err
	}
	reactions := kind + "_reactions"
	var old string
	err = tx.QueryRow("SELECT reaction_type FROM "+reactions+" WHERE "+key+"=? AND user_id=?", id, userID).Scan(&old)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if old == reaction {
		_, err = tx.Exec("DELETE FROM "+reactions+" WHERE "+key+"=? AND user_id=?", id, userID)
	} else {
		_, err = tx.Exec("INSERT INTO "+reactions+"("+key+",user_id,reaction_type) VALUES(?,?,?) ON CONFLICT("+key+",user_id) DO UPDATE SET reaction_type=excluded.reaction_type", id, userID, reaction)
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec("UPDATE "+table+" SET liked=(SELECT COUNT(*) FROM "+reactions+" WHERE "+key+"=? AND reaction_type='like'),disliked=(SELECT COUNT(*) FROM "+reactions+" WHERE "+key+"=? AND reaction_type='dislike') WHERE id=?", id, id, id); err != nil {
		return err
	}
	if old != reaction {
		var commentID interface{}
		if kind == "comment" {
			commentID = id
		}
		if _, err = tx.Exec("INSERT INTO activities(user_id,activity_type,post_id,comment_id) VALUES(?,?,?,?)", userID, "You "+reaction+"d", postID, commentID); err != nil {
			return err
		}
		if owner != userID {
			if _, err = tx.Exec("INSERT INTO notifications(user_id,notification_type,post_id,comment_id) VALUES(?,?,?,?)", owner, "You received a "+reaction, postID, commentID); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
