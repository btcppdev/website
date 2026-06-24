package getters

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"context"
	"fmt"
	"strings"
)

func ListJobTypes(ctx *config.AppContext) ([]*types.JobType, error) {
	return ListJobs(ctx)
}

func ListJobs(ctx *config.AppContext) ([]*types.JobType, error) {
	return queryJobsPostgres(ctx, "job types", "", nil)
}

func GetJobByTag(ctx *config.AppContext, tag string) (*types.JobType, error) {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return nil, nil
	}
	jobs, err := queryJobsPostgres(ctx, "job type by tag", "WHERE tag = $1", []any{tag})
	if err != nil || len(jobs) == 0 {
		return nil, err
	}
	return jobs[0], nil
}

func queryJobsPostgres(ctx *config.AppContext, label string, whereSQL string, args []any) ([]*types.JobType, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	rows, err := ctx.DB.Query(context.Background(), `
		SELECT id::text, tag, display_order, title, tooltip, long_desc, show
		FROM job_types
		`+whereSQL+`
		ORDER BY display_order, title
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("query %s: %w", label, err)
	}
	defer rows.Close()

	var jobs []*types.JobType
	for rows.Next() {
		var job types.JobType
		err := rows.Scan(
			&job.Ref,
			&job.Tag,
			&job.DisplayOrder,
			&job.Title,
			&job.Tooltip,
			&job.LongDesc,
			&job.Show,
		)
		if err != nil {
			return nil, fmt.Errorf("scan %s: %w", label, err)
		}
		jobs = append(jobs, &job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s: %w", label, err)
	}
	return jobs, nil
}
