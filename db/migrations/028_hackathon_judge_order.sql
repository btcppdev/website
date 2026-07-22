ALTER TABLE competition_judges
ADD COLUMN IF NOT EXISTS display_order integer NOT NULL DEFAULT 0;

WITH judge_people AS (
	SELECT competition_id, person_id, min(created_at) AS first_created
	FROM competition_judges
	GROUP BY competition_id, person_id
),
ordered AS (
	SELECT competition_id,
		person_id,
		row_number() OVER (
			PARTITION BY competition_id
			ORDER BY first_created, person_id
		)::integer AS display_order
	FROM judge_people
)
UPDATE competition_judges judges
SET display_order = ordered.display_order
FROM ordered
WHERE judges.competition_id = ordered.competition_id
	AND judges.person_id = ordered.person_id
	AND judges.display_order = 0;

CREATE INDEX IF NOT EXISTS competition_judges_order_idx
ON competition_judges (competition_id, display_order, person_id);
