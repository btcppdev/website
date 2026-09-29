package getters

import (
	"fmt"

	"btcpp-web/internal/config"
	"github.com/jackc/pgx/v5"
)

// Attach managers additively: manual speakers and judges remain attached, and
// removing management permission does not erase recorded participation.
func attachHackathonManagerProposals(ctx *config.AppContext, tx pgx.Tx, personID, competitionID string) error {
	rows, err := tx.Query(ctx.DatabaseContext(), `
  SELECT DISTINCT r.person_id::text, c.id::text, seg.proposal_id::text
  FROM people_roles r
  JOIN conferences c ON r.scope = c.tag OR r.scope = 'global'
  JOIN competitions h ON h.conference_id = c.id
  JOIN competition_schedule_segments seg ON seg.competition_id = h.id
  WHERE r.position = 'hackathon' AND seg.proposal_id IS NOT NULL
    AND ($1 = '' OR r.person_id::text = $1)
    AND ($2 = '' OR h.id::text = $2)
  ORDER BY 1, 2, 3
 `, personID, competitionID)
	if err != nil {
		return fmt.Errorf("find manager proposal links: %w", err)
	}
	type link struct{ person, conf, proposal string }
	var links []link
	for rows.Next() {
		var l link
		if err := rows.Scan(&l.person, &l.conf, &l.proposal); err != nil {
			rows.Close()
			return err
		}
		links = append(links, l)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, l := range links {
		// Serialize manager attachments for a person to reuse their conference profile.
		if _, err := tx.Exec(ctx.DatabaseContext(), `SELECT id FROM people WHERE id=$1::uuid FOR NO KEY UPDATE`, l.person); err != nil {
			return err
		}
		var sc string
		err := tx.QueryRow(ctx.DatabaseContext(), `
   SELECT sc.id::text FROM speaker_confs sc
   WHERE sc.speaker_id=$1::uuid AND (
     EXISTS (SELECT 1 FROM speaker_confs_conferences scc
             WHERE scc.speaker_conf_id=sc.id AND scc.conference_id=$2::uuid)
     OR EXISTS (SELECT 1 FROM proposals_speaker_confs psc
                JOIN proposals p ON p.id=psc.proposal_id
                WHERE psc.speaker_conf_id=sc.id AND p.conference_id=$2::uuid)
   )
   ORDER BY sc.created_at,sc.id LIMIT 1`, l.person, l.conf).Scan(&sc)
		if err == pgx.ErrNoRows {
			if err = tx.QueryRow(ctx.DatabaseContext(), `INSERT INTO speaker_confs(speaker_id) VALUES ($1::uuid) RETURNING id::text`, l.person).Scan(&sc); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx.DatabaseContext(), `INSERT INTO speaker_confs_conferences(speaker_conf_id,conference_id) VALUES ($1::uuid,$2::uuid)`, sc, l.conf); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx.DatabaseContext(), `INSERT INTO proposals_speaker_confs(proposal_id,speaker_conf_id) VALUES ($1::uuid,$2::uuid) ON CONFLICT DO NOTHING`, l.proposal, sc); err != nil {
			return err
		}
	}
	return nil
}

func attachCompetitionManagers(ctx *config.AppContext, competitionID string) error {
	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx.DatabaseContext())
	if err := attachHackathonManagerProposals(ctx, tx, "", competitionID); err != nil {
		return err
	}
	return tx.Commit(ctx.DatabaseContext())
}
