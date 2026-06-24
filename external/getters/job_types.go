package getters

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

func ListJobTypes(ctx *config.AppContext) ([]*types.JobType, error) {
	if UsePostgresBackend(ctx) {
		return listJobsPostgres(ctx)
	}
	return ListJobsNotion(ctx.Notion)
}

func GetJobByTag(ctx *config.AppContext, tag string) (*types.JobType, error) {
	if UsePostgresBackend(ctx) {
		return getJobByTagPostgres(ctx, tag)
	}
	jobs, err := ListJobTypes(ctx)
	if err != nil {
		return nil, err
	}
	for _, j := range jobs {
		if j != nil && j.Tag == tag {
			return j, nil
		}
	}
	return nil, nil
}

func ListJobs(n *types.Notion) ([]*types.JobType, error) {
	return ListJobsNotion(n)
}
