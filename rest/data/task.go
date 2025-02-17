package data

import (
	"fmt"
	"net/http"

	"github.com/evergreen-ci/evergreen/model"
	serviceModel "github.com/evergreen-ci/evergreen/model"
	"github.com/evergreen-ci/evergreen/model/task"
	"github.com/evergreen-ci/gimlet"
	"github.com/pkg/errors"
)

// FindTasksByBuildId uses the service layer's task type to query the backing database for a
// list of task that matches buildId. It accepts the startTaskId and a limit
// to allow for pagination of the queries. It returns results sorted by taskId.
func FindTasksByBuildId(buildId, taskId, status string, limit int, sortDir int) ([]task.Task, error) {
	pipeline := task.TasksByBuildIdPipeline(buildId, taskId, status, limit, sortDir)
	res := []task.Task{}

	err := task.Aggregate(pipeline, &res)
	if err != nil {
		return []task.Task{}, err
	}

	if taskId != "" {
		found := false
		for _, t := range res {
			if t.Id == taskId {
				found = true
				break
			}
		}
		if !found {
			return []task.Task{}, gimlet.ErrorResponse{
				StatusCode: http.StatusNotFound,
				Message:    fmt.Sprintf("task '%s' not found", taskId),
			}
		}
	}
	return res, nil
}

// FindTasksByProjectAndCommit is a method to find a set of tasks which ran as part of
// certain version in a project. It takes the projectId, commit hash, and a taskId
// for paginating through the results.
func FindTasksByProjectAndCommit(opts task.GetTasksByProjectAndCommitOptions) ([]task.Task, error) {
	projectId, err := model.GetIdForProject(opts.Project)
	if err != nil {
		return nil, gimlet.ErrorResponse{
			StatusCode: http.StatusNotFound,
			Message:    errors.Wrapf(err, "project '%s' not found", projectId).Error(),
		}
	}

	opts.Project = projectId
	pipeline := task.TasksByProjectAndCommitPipeline(opts)

	res := []task.Task{}
	err = task.Aggregate(pipeline, &res)
	if err != nil {
		return []task.Task{}, err
	}
	if len(res) == 0 {
		var message string
		if opts.Status != "" {
			message = fmt.Sprintf("task from project '%s' and commit '%s' with status '%s' "+
				"not found", projectId, opts.CommitHash, opts.Status)
		} else {
			message = fmt.Sprintf("task from project '%s' and commit '%s' not found",
				projectId, opts.CommitHash)
		}
		return []task.Task{}, gimlet.ErrorResponse{
			StatusCode: http.StatusNotFound,
			Message:    message,
		}
	}
	return res, nil
}

func CheckTaskSecret(taskID string, r *http.Request) (int, error) {
	_, code, err := serviceModel.ValidateTask(taskID, true, r)
	return code, errors.Wrapf(err, "invalid task '%s'", taskID)
}

func FindTask(taskID string) (*task.Task, error) {
	foundTask, err := task.FindOneId(taskID)
	if err != nil {
		return nil, gimlet.ErrorResponse{
			StatusCode: http.StatusInternalServerError,
			Message:    errors.Wrap(err, "finding task").Error(),
		}
	}
	if foundTask == nil {
		return nil, gimlet.ErrorResponse{
			StatusCode: http.StatusNotFound,
			Message:    fmt.Sprintf("task '%s' not found", taskID),
		}
	}

	return foundTask, nil
}
