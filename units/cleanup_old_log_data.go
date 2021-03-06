package units

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"

	"github.com/evergreen-ci/logkeeper"
	"github.com/mongodb/amboy"
	"github.com/mongodb/amboy/dependency"
	"github.com/mongodb/amboy/job"
	"github.com/mongodb/amboy/registry"
	"github.com/mongodb/grip"
	"github.com/mongodb/grip/message"
	"github.com/pkg/errors"
)

const (
	cleanupJobsName = "cleanup-old-log-data-job"
	urlBase         = "https://evergreen.mongodb.com/rest/v2/tasks"
)

var (
	apiUser = os.Getenv("EVG_API_USER")
	apiKey  = os.Getenv("EVG_API_KEY")
)

func init() {
	registry.AddJobType(cleanupJobsName,
		func() amboy.Job { return makeCleanupOldLogDataJob() })
}

type cleanupOldLogDataJob struct {
	BuildID  interface{} `bson:"build_id" json:"build_id" yaml:"build_id"`
	TaskID   interface{} `bson:"task_id" json:"task_id" yaml:"task_id"`
	job.Base `bson:"job_base" json:"job_base" yaml:"job_base"`
}

func NewCleanupOldLogDataJob(buildID, taskID, oid interface{}) amboy.Job {
	j := makeCleanupOldLogDataJob()
	j.BuildID = buildID
	j.TaskID = taskID
	j.SetID(fmt.Sprintf("%s.%s.%s.oid=%s", cleanupJobsName, j.BuildID, j.TaskID, oid))
	return j
}

func makeCleanupOldLogDataJob() *cleanupOldLogDataJob {
	j := &cleanupOldLogDataJob{
		Base: job.Base{
			JobType: amboy.JobType{
				Name:    cleanupJobsName,
				Version: 1,
			},
		},
	}
	j.SetDependency(dependency.NewAlways())
	return j
}

// If the evergreen task is marked complete, delete the test and corresponding log objects
func (j *cleanupOldLogDataJob) Run(ctx context.Context) {
	defer j.MarkComplete()

	url := fmt.Sprintf("%s/%s", urlBase, j.TaskID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		j.AddError(err)
		return
	}

	req = req.WithContext(ctx)
	req.Header.Add("Api-User", apiUser)
	req.Header.Add("Api-Key", apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		j.AddError(err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		grip.Info(message.Fields{
			"job":   j.ID(),
			"code":  resp.StatusCode,
			"op":    "skipping build with missing evergreen task",
			"build": j.BuildID,
		})
		return
	}

	payload, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		j.AddError(err)
		return
	}

	taskInfo := struct {
		Status string `json:"status"`
	}{}

	if err = json.Unmarshal(payload, &taskInfo); err != nil {
		j.AddError(errors.Wrapf(err, "problem reading response from server for [task='%s' build='%s']", j.TaskID, j.BuildID))
	}

	var num int
	if taskInfo.Status != "success" {
		num, err = logkeeper.UpdateFailedTestsByBuildID(j.BuildID)
		if err != nil {
			j.AddError(errors.Wrapf(err, "error updating failed status of test [%d]", num))
		}
	} else {
		num, err = logkeeper.CleanupOldLogsByBuild(j.BuildID)
		if err != nil {
			j.AddError(errors.Wrapf(err, "error cleaning up old logs [%d]", num))
		}
	}
	grip.Info(message.Fields{
		"job_type": j.Type().Name,
		"op":       "deletion complete",
		"task":     j.TaskID,
		"build":    j.BuildID,
		"errors":   j.HasErrors(),
		"job":      j.ID(),
		"num":      num,
		"status":   taskInfo.Status,
	})
}
