package getters

import (
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

func getJobs(ctx *config.AppContext) {
	var err error
	ctx.Infos.Printf("getting jobs...")
	if UsePostgresBackend(ctx) {
		jobs, err = listJobsPostgres(ctx)
	} else {
		jobs, err = ListJobsNotion(ctx.Notion)
	}

	if err != nil {
		ctx.Err.Printf("error fetching jobs %s", err)
	} else {
		ctx.Infos.Printf("Loaded %d jobs!", len(jobs))
	}
}

/* This may return nil */
func FetchJobsCached(ctx *config.AppContext) ([]*types.JobType, error) {
	now := time.Now()
	deadline := now.Add(-cacheTTL)
	if jobs == nil || lastJobTypeFetch.Before(deadline) {
		lastJobTypeFetch = time.Now()
		queueRefresh(JobJobs)
	}

	return jobs, nil
}

func ListJobTypes(ctx *config.AppContext) ([]*types.JobType, error) {
	if UsePostgresBackend(ctx) {
		return listJobsPostgres(ctx)
	}
	return FetchJobsCached(ctx)
}

func GetJobByTag(ctx *config.AppContext, tag string) (*types.JobType, error) {
	if UsePostgresBackend(ctx) {
		return getJobByTagPostgres(ctx, tag)
	}
	jobs, err := FetchJobsCached(ctx)
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
