package task

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"runtime/debug"
	"strings"
	"time"

	"github.com/evergreen-ci/evergreen"
	"github.com/evergreen-ci/evergreen/apimodels"
	"github.com/evergreen-ci/evergreen/db"
	mgobson "github.com/evergreen-ci/evergreen/db/mgo/bson"
	"github.com/evergreen-ci/evergreen/model/distro"
	"github.com/evergreen-ci/evergreen/model/event"
	"github.com/evergreen-ci/evergreen/model/testresult"
	"github.com/evergreen-ci/evergreen/util"
	"github.com/evergreen-ci/tarjan"
	"github.com/evergreen-ci/utility"
	"github.com/mongodb/anser/bsonutil"
	adb "github.com/mongodb/anser/db"
	"github.com/mongodb/grip"
	"github.com/mongodb/grip/message"
	"github.com/mongodb/grip/recovery"
	"github.com/pkg/errors"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"gonum.org/v1/gonum/graph"
	"gonum.org/v1/gonum/graph/simple"
	"gonum.org/v1/gonum/graph/topo"
)

const (
	dependencyKey = "dependencies"

	// UnschedulableThreshold is the threshold after which a task waiting to
	// dispatch should be unscheduled due to staleness.
	UnschedulableThreshold = 7 * 24 * time.Hour

	// indicates the window of completed tasks we want to use in computing
	// average task duration. By default we use tasks that have
	// completed within the last 7 days
	taskCompletionEstimateWindow = 24 * 7 * time.Hour

	// if we have no data on a given task, default to 10 minutes so we
	// have some new hosts spawned
	defaultTaskDuration = 10 * time.Minute

	// length of time to cache the expected duration in the task document
	predictionTTL = 8 * time.Hour
)

var (
	// A regex that matches either / or \ for splitting directory paths
	// on either windows or linux paths.
	eitherSlash = regexp.MustCompile(`[/\\]`)
)

type Task struct {
	Id     string `bson:"_id" json:"id"`
	Secret string `bson:"secret" json:"secret"`

	// time information for task
	// CreateTime - the creation time for the task, derived from the commit time or the patch creation time.
	// DispatchTime - the time the task runner starts up the agent on the host.
	// ScheduledTime - the time the task is scheduled.
	// StartTime - the time the agent starts the task on the host after spinning it up.
	// FinishTime - the time the task was completed on the remote host.
	// ActivatedTime - the time the task was marked as available to be scheduled, automatically or by a developer.
	// DependenciesMet - for tasks that have dependencies, the time all dependencies are met.
	// ContainerAllocated - for tasks that run on containers, the time the container was allocated.
	CreateTime             time.Time `bson:"create_time" json:"create_time"`
	IngestTime             time.Time `bson:"injest_time" json:"ingest_time"`
	DispatchTime           time.Time `bson:"dispatch_time" json:"dispatch_time"`
	ScheduledTime          time.Time `bson:"scheduled_time" json:"scheduled_time"`
	StartTime              time.Time `bson:"start_time" json:"start_time"`
	FinishTime             time.Time `bson:"finish_time" json:"finish_time"`
	ActivatedTime          time.Time `bson:"activated_time" json:"activated_time"`
	DependenciesMetTime    time.Time `bson:"dependencies_met_time,omitempty" json:"dependencies_met_time,omitempty"`
	ContainerAllocatedTime time.Time `bson:"container_allocated_time,omitempty" json:"container_allocated_time,omitempty"`

	Version           string `bson:"version" json:"version,omitempty"`
	Project           string `bson:"branch" json:"branch,omitempty"`
	Revision          string `bson:"gitspec" json:"gitspec"`
	Priority          int64  `bson:"priority" json:"priority"`
	TaskGroup         string `bson:"task_group" json:"task_group"`
	TaskGroupMaxHosts int    `bson:"task_group_max_hosts,omitempty" json:"task_group_max_hosts,omitempty"`
	TaskGroupOrder    int    `bson:"task_group_order,omitempty" json:"task_group_order,omitempty"`
	LogServiceVersion *int   `bson:"log_service_version" json:"log_service_version"`
	ResultsService    string `bson:"results_service,omitempty" json:"results_service,omitempty"`
	HasCedarResults   bool   `bson:"has_cedar_results,omitempty" json:"has_cedar_results,omitempty"`
	ResultsFailed     bool   `bson:"results_failed,omitempty" json:"results_failed,omitempty"`
	MustHaveResults   bool   `bson:"must_have_results,omitempty" json:"must_have_results,omitempty"`
	// only relevant if the task is running.  the time of the last heartbeat
	// sent back by the agent
	LastHeartbeat time.Time `bson:"last_heartbeat" json:"last_heartbeat"`

	// Activated indicates whether the task should be scheduled to run or not.
	Activated                bool   `bson:"activated" json:"activated"`
	ActivatedBy              string `bson:"activated_by" json:"activated_by"`
	DeactivatedForDependency bool   `bson:"deactivated_for_dependency" json:"deactivated_for_dependency"`

	// StepbackDepth indicates how far into stepback this task was activated, starting at 1 for stepback tasks.
	// After EVG-17949, should either remove this field/logging or use it to limit stepback depth.
	StepbackDepth int `bson:"stepback_depth" json:"stepback_depth"`

	// ContainerAllocated indicates whether this task has been allocated a
	// container to run it. It only applies to tasks running in containers.
	ContainerAllocated bool `bson:"container_allocated" json:"container_allocated"`
	// ContainerAllocationAttempts is the number of times this task has
	// been allocated a container to run it (for a single execution).
	ContainerAllocationAttempts int `bson:"container_allocation_attempts" json:"container_allocation_attempts"`

	BuildId  string `bson:"build_id" json:"build_id"`
	DistroId string `bson:"distro" json:"distro"`
	// Container is the name of the container configuration for running a
	// container task.
	Container string `bson:"container,omitempty" json:"container,omitempty"`
	// ContainerOpts contains the options to configure the container that will
	// run the task.
	ContainerOpts           ContainerOptions `bson:"container_options,omitempty" json:"container_options,omitempty"`
	BuildVariant            string           `bson:"build_variant" json:"build_variant"`
	BuildVariantDisplayName string           `bson:"build_variant_display_name" json:"-"`
	DependsOn               []Dependency     `bson:"depends_on" json:"depends_on"`
	// UnattainableDependency caches the contents of DependsOn for more efficient querying.
	UnattainableDependency bool `bson:"unattainable_dependency" json:"unattainable_dependency"`
	NumDependents          int  `bson:"num_dependents,omitempty" json:"num_dependents,omitempty"`
	// OverrideDependencies indicates whether a task should override its dependencies. If set, it will not
	// wait for its dependencies to finish before running.
	OverrideDependencies bool `bson:"override_dependencies,omitempty" json:"override_dependencies,omitempty"`

	// SecondaryDistros refer to the optional secondary distros that can be
	// associated with a task. This is used for running tasks in case there are
	// idle hosts in a distro with an empty primary queue. This is a distinct concept
	// from distro aliases (i.e. alternative distro names).
	// Tags refer to outdated naming; maintained for compatibility.
	SecondaryDistros []string `bson:"distro_aliases,omitempty" json:"distro_aliases,omitempty"`

	// Human-readable name
	DisplayName string `bson:"display_name" json:"display_name"`

	// Tags that describe the task
	Tags []string `bson:"tags,omitempty" json:"tags,omitempty"`

	// The host the task was run on. This value is only set for host tasks.
	HostId string `bson:"host_id,omitempty" json:"host_id"`

	// PodID is the pod that was assigned to run the task. This value is only
	// set for container tasks.
	PodID string `bson:"pod_id,omitempty" json:"pod_id"`

	// ExecutionPlatform determines the execution environment that the task runs
	// in.
	ExecutionPlatform ExecutionPlatform `bson:"execution_platform,omitempty" json:"execution_platform,omitempty"`

	// The version of the agent this task was run on.
	AgentVersion string `bson:"agent_version,omitempty" json:"agent_version,omitempty"`

	// Set to true if the task should be considered for mainline github checks
	IsGithubCheck bool `bson:"is_github_check,omitempty" json:"is_github_check,omitempty"`

	// CanReset indicates that the task has successfully archived and is in a valid state to be reset.
	CanReset bool `bson:"can_reset,omitempty" json:"can_reset,omitempty"`

	Execution           int    `bson:"execution" json:"execution"`
	OldTaskId           string `bson:"old_task_id,omitempty" json:"old_task_id,omitempty"`
	Archived            bool   `bson:"archived,omitempty" json:"archived,omitempty"`
	RevisionOrderNumber int    `bson:"order,omitempty" json:"order,omitempty"`

	// task requester - this is used to help tell the
	// reason this task was created. e.g. it could be
	// because the repotracker requested it (via tracking the
	// repository) or it was triggered by a developer
	// patch request
	Requester string `bson:"r" json:"r"`

	// tasks that are part of a child patch will store the id and patch number of the parent patch
	ParentPatchID     string `bson:"parent_patch_id,omitempty" json:"parent_patch_id,omitempty"`
	ParentPatchNumber int    `bson:"parent_patch_number,omitempty" json:"parent_patch_number,omitempty"`

	// Status represents the various stages the task could be in. Note that this
	// task status is distinct from the way a task status is displayed in the
	// UI. For example, a task that has failed will have a status of
	// evergreen.TaskFailed regardless of the specific cause of failure.
	// However, in the UI, the displayed status supports more granular failure
	// type such as system failed and setup failed by checking this status and
	// the task status details.
	Status    string                  `bson:"status" json:"status"`
	Details   apimodels.TaskEndDetail `bson:"details" json:"task_end_details"`
	Aborted   bool                    `bson:"abort,omitempty" json:"abort"`
	AbortInfo AbortInfo               `bson:"abort_info,omitempty" json:"abort_info,omitempty"`

	// HostCreateDetails stores information about why host.create failed for this task
	HostCreateDetails []HostCreateDetail `bson:"host_create_details,omitempty" json:"host_create_details,omitempty"`
	// DisplayStatus is not persisted to the db. It is the status to display in the UI.
	// It may be added via aggregation
	DisplayStatus string `bson:"display_status,omitempty" json:"display_status,omitempty"`
	// BaseTask is not persisted to the db. It is the data of the task on the base commit
	// It may be added via aggregation
	BaseTask BaseTaskInfo `bson:"base_task" json:"base_task"`

	// TimeTaken is how long the task took to execute (if it has finished) or how long the task has been running (if it has started)
	TimeTaken time.Duration `bson:"time_taken" json:"time_taken"`
	// WaitSinceDependenciesMet is populated in GetDistroQueueInfo, used for host allocation
	WaitSinceDependenciesMet time.Duration `bson:"wait_since_dependencies_met,omitempty" json:"wait_since_dependencies_met,omitempty"`

	// how long we expect the task to take from start to
	// finish. expected duration is the legacy value, but the UI
	// probably depends on it, so we maintain both values.
	ExpectedDuration       time.Duration            `bson:"expected_duration,omitempty" json:"expected_duration,omitempty"`
	ExpectedDurationStdDev time.Duration            `bson:"expected_duration_std_dev,omitempty" json:"expected_duration_std_dev,omitempty"`
	DurationPrediction     util.CachedDurationValue `bson:"duration_prediction,omitempty" json:"-"`

	// test results embedded from the testresults collection
	LocalTestResults []testresult.TestResult `bson:"-" json:"test_results"`

	// display task fields
	DisplayOnly           bool     `bson:"display_only,omitempty" json:"display_only,omitempty"`
	ExecutionTasks        []string `bson:"execution_tasks,omitempty" json:"execution_tasks,omitempty"`
	LatestParentExecution int      `bson:"latest_parent_execution" json:"latest_parent_execution"`

	// ResetWhenFinished indicates that a task should be reset once it is
	// finished running. This is typically to deal with tasks that should be
	// reset but cannot do so yet because they're currently running.
	ResetWhenFinished       bool  `bson:"reset_when_finished,omitempty" json:"reset_when_finished,omitempty"`
	ResetFailedWhenFinished bool  `bson:"reset_failed_when_finished,omitempty" json:"reset_failed_when_finished,omitempty"`
	DisplayTask             *Task `bson:"-" json:"-"` // this is a local pointer from an exec to display task

	// DisplayTaskId is set to the display task ID if the task is an execution task, the empty string if it's not an execution task,
	// and is nil if we haven't yet checked whether or not this task has a display task.
	DisplayTaskId *string `bson:"display_task_id,omitempty" json:"display_task_id,omitempty"`

	// GenerateTask indicates that the task generates other tasks, which the
	// scheduler will use to prioritize this task. This will not be set for
	// tasks where the generate.tasks command runs outside of the main task
	// block (e.g. pre, timeout).
	GenerateTask bool `bson:"generate_task,omitempty" json:"generate_task,omitempty"`
	// GeneratedTasks indicates that the task has already generated other tasks. This fields
	// allows us to noop future requests, since a task should only generate others once.
	GeneratedTasks bool `bson:"generated_tasks,omitempty" json:"generated_tasks,omitempty"`
	// GeneratedBy, if present, is the ID of the task that generated this task.
	GeneratedBy string `bson:"generated_by,omitempty" json:"generated_by,omitempty"`
	// GeneratedJSONAsString is the configuration information to create new tasks from.
	GeneratedJSONAsString []string `bson:"generated_json,omitempty" json:"generated_json,omitempty"`
	// GenerateTasksError any encountered while generating tasks.
	GenerateTasksError string `bson:"generate_error,omitempty" json:"generate_error,omitempty"`
	// GeneratedTasksToActivate is only populated if we want to override activation for these generated tasks, because of stepback.
	// Maps the build variant to a list of task names.
	GeneratedTasksToActivate map[string][]string `bson:"generated_tasks_to_stepback,omitempty" json:"generated_tasks_to_stepback,omitempty"`

	// Fields set if triggered by an upstream build
	TriggerID    string `bson:"trigger_id,omitempty" json:"trigger_id,omitempty"`
	TriggerType  string `bson:"trigger_type,omitempty" json:"trigger_type,omitempty"`
	TriggerEvent string `bson:"trigger_event,omitempty" json:"trigger_event,omitempty"`

	CommitQueueMerge bool `bson:"commit_queue_merge,omitempty" json:"commit_queue_merge,omitempty"`

	CanSync       bool             `bson:"can_sync" json:"can_sync"`
	SyncAtEndOpts SyncAtEndOptions `bson:"sync_at_end_opts,omitempty" json:"sync_at_end_opts,omitempty"`

	// IsEssentialToSucceed indicates that this task must finish in order for
	// its build and version to be considered successful. For example, tasks
	// selected by the GitHub PR alias must succeed for the GitHub PR requester
	// before its build or version can be reported as successful, but tasks
	// manually scheduled by the user afterwards are not required.
	IsEssentialToSucceed bool `bson:"is_essential_to_succeed" json:"is_essential_to_succeed"`
}

// ExecutionPlatform indicates the type of environment that the task runs in.
type ExecutionPlatform string

const (
	// ExecutionPlatformHost indicates that the task runs in a host.
	ExecutionPlatformHost ExecutionPlatform = "host"
	// ExecutionPlatformContainer indicates that the task runs in a container.
	ExecutionPlatformContainer ExecutionPlatform = "container"
)

// ContainerOptions represent options to create the container to run a task.
type ContainerOptions struct {
	CPU        int    `bson:"cpu,omitempty" json:"cpu"`
	MemoryMB   int    `bson:"memory_mb,omitempty" json:"memory_mb"`
	WorkingDir string `bson:"working_dir,omitempty" json:"working_dir"`
	Image      string `bson:"image,omitempty" json:"image"`
	// RepoCredsName is the name of the project container secret containing the
	// repository credentials.
	RepoCredsName  string                   `bson:"repo_creds_name,omitempty" json:"repo_creds_name"`
	OS             evergreen.ContainerOS    `bson:"os,omitempty" json:"os"`
	Arch           evergreen.ContainerArch  `bson:"arch,omitempty" json:"arch"`
	WindowsVersion evergreen.WindowsVersion `bson:"windows_version,omitempty" json:"windows_version"`
}

// IsZero implements the bsoncodec.Zeroer interface for the sake of defining the
// zero value for BSON marshalling.
func (o ContainerOptions) IsZero() bool {
	return o == ContainerOptions{}
}

func (t *Task) MarshalBSON() ([]byte, error)  { return mgobson.Marshal(t) }
func (t *Task) UnmarshalBSON(in []byte) error { return mgobson.Unmarshal(in, t) }

func (t *Task) GetTaskGroupString() string {
	return fmt.Sprintf("%s_%s_%s_%s", t.TaskGroup, t.BuildVariant, t.Project, t.Version)
}

// S3Path returns the path to a task's directory dump in S3.
func (t *Task) S3Path(bv, name string) string {
	return strings.Join([]string{t.Project, t.Version, bv, name, "latest"}, "/")
}

type SyncAtEndOptions struct {
	Enabled  bool          `bson:"enabled,omitempty" json:"enabled,omitempty"`
	Statuses []string      `bson:"statuses,omitempty" json:"statuses,omitempty"`
	Timeout  time.Duration `bson:"timeout,omitempty" json:"timeout,omitempty"`
}

// Dependency represents a task that must be completed before the owning
// task can be scheduled.
type Dependency struct {
	TaskId       string `bson:"_id" json:"id"`
	Status       string `bson:"status" json:"status"`
	Unattainable bool   `bson:"unattainable" json:"unattainable"`
	// Finished indicates if the task's dependency has finished running or not.
	Finished bool `bson:"finished" json:"finished"`
	// OmitGeneratedTasks causes tasks that depend on a generator task to not depend on
	// the generated tasks if this is set
	OmitGeneratedTasks bool `bson:"omit_generated_tasks,omitempty" json:"omit_generated_tasks,omitempty"`
}

// BaseTaskInfo is a subset of task fields that should be returned for patch tasks.
// The bson keys must match those of the actual task document
type BaseTaskInfo struct {
	Id     string `bson:"_id" json:"id"`
	Status string `bson:"status" json:"status"`
}

type HostCreateDetail struct {
	HostId string `bson:"host_id" json:"host_id"`
	Error  string `bson:"error" json:"error"`
}

func (d *Dependency) UnmarshalBSON(in []byte) error {
	return mgobson.Unmarshal(in, d)
}

// SetBSON allows us to use dependency representation of both
// just task Ids and of true Dependency structs.
//
//	TODO eventually drop all of this switching
func (d *Dependency) SetBSON(raw mgobson.Raw) error {
	// copy the Dependency type to remove this SetBSON method but preserve bson struct tags
	type nakedDep Dependency
	var depCopy nakedDep
	if err := raw.Unmarshal(&depCopy); err == nil {
		if depCopy.TaskId != "" {
			*d = Dependency(depCopy)
			return nil
		}
	}

	// hack to support the legacy depends_on, since we can't just unmarshal a string
	strBytes, _ := mgobson.Marshal(mgobson.RawD{{Name: "str", Value: raw}})
	var strStruct struct {
		String string `bson:"str"`
	}
	if err := mgobson.Unmarshal(strBytes, &strStruct); err == nil {
		if strStruct.String != "" {
			d.TaskId = strStruct.String
			d.Status = evergreen.TaskSucceeded
			return nil
		}
	}

	return mgobson.SetZero
}

type DisplayTaskCache struct {
	execToDisplay map[string]*Task
	displayTasks  []*Task
}

func (c *DisplayTaskCache) Get(t *Task) (*Task, error) {
	if parent, exists := c.execToDisplay[t.Id]; exists {
		return parent, nil
	}
	displayTask, err := t.GetDisplayTask()
	if err != nil {
		return nil, err
	}
	if displayTask == nil {
		return nil, nil
	}
	for _, execTask := range displayTask.ExecutionTasks {
		c.execToDisplay[execTask] = displayTask
	}
	c.displayTasks = append(c.displayTasks, displayTask)
	return displayTask, nil
}
func (c *DisplayTaskCache) List() []*Task { return c.displayTasks }

func NewDisplayTaskCache() DisplayTaskCache {
	return DisplayTaskCache{execToDisplay: map[string]*Task{}, displayTasks: []*Task{}}
}

type AbortInfo struct {
	User       string `bson:"user,omitempty" json:"user,omitempty"`
	TaskID     string `bson:"task_id,omitempty" json:"task_id,omitempty"`
	NewVersion string `bson:"new_version,omitempty" json:"new_version,omitempty"`
	PRClosed   bool   `bson:"pr_closed,omitempty" json:"pr_closed,omitempty"`
}

var (
	AllStatuses = "*"
)

// IsAbortable returns true if the task can be aborted.
func (t *Task) IsAbortable() bool {
	return t.Status == evergreen.TaskStarted ||
		t.Status == evergreen.TaskDispatched
}

// IsFinished returns true if the task is no longer running
func (t *Task) IsFinished() bool {
	return evergreen.IsFinishedTaskStatus(t.Status)
}

// IsDispatchable returns true if the task should make progress towards
// dispatching to run.
func (t *Task) IsDispatchable() bool {
	return t.IsHostDispatchable() || t.ShouldAllocateContainer() || t.IsContainerDispatchable()
}

// IsHostDispatchable returns true if the task should run on a host and can be
// dispatched.
func (t *Task) IsHostDispatchable() bool {
	return t.IsHostTask() && t.WillRun()
}

// IsHostTask returns true if it's a task that runs on hosts.
func (t *Task) IsHostTask() bool {
	return (t.ExecutionPlatform == "" || t.ExecutionPlatform == ExecutionPlatformHost) && !t.DisplayOnly
}

// IsContainerTask returns true if it's a task that runs on containers.
func (t *Task) IsContainerTask() bool {
	return t.ExecutionPlatform == ExecutionPlatformContainer
}

// IsRestartFailedOnly returns true if the task should only restart failed tests.
func (t *Task) IsRestartFailedOnly() bool {
	return t.ResetFailedWhenFinished && !t.ResetWhenFinished
}

// ShouldAllocateContainer indicates whether a task should be allocated a
// container or not.
func (t *Task) ShouldAllocateContainer() bool {
	if t.ContainerAllocated {
		return false
	}
	if t.RemainingContainerAllocationAttempts() == 0 {
		return false
	}

	return t.isContainerScheduled()
}

// RemainingContainerAllocationAttempts returns the number of times this task
// execution is allowed to try allocating a container.
func (t *Task) RemainingContainerAllocationAttempts() int {
	return maxContainerAllocationAttempts - t.ContainerAllocationAttempts
}

// IsContainerDispatchable returns true if the task should run in a container
// and can be dispatched.
func (t *Task) IsContainerDispatchable() bool {
	if !t.ContainerAllocated {
		return false
	}
	return t.isContainerScheduled()
}

// isContainerTaskScheduled returns whether the task is in a state where it
// should eventually dispatch to run on a container and is logically equivalent
// to IsContainerTaskScheduledQuery. This encompasses two potential states:
//  1. A container is not yet allocated to the task but it's ready to be
//     allocated one. Note that this is a subset of all container tasks that
//     could eventually run (i.e. evergreen.TaskWillRun from
//     (Task).GetDisplayStatus), because a container task is not scheduled until
//     all of its dependencies have been met.
//  2. The container is allocated but the agent has not picked up the task yet.
func (t *Task) isContainerScheduled() bool {
	if !t.IsContainerTask() {
		return false
	}
	if t.Status != evergreen.TaskUndispatched {
		return false
	}
	if !t.Activated {
		return false
	}
	if t.Priority <= evergreen.DisabledTaskPriority {
		return false
	}
	if !t.OverrideDependencies {
		for _, dep := range t.DependsOn {
			if dep.Unattainable {
				return false
			}
			if !dep.Finished {
				return false
			}
		}
	}

	return true
}

// SatisfiesDependency checks a task the receiver task depends on
// to see if its status satisfies a dependency. If the "Status" field is
// unset, default to checking that is succeeded.
func (t *Task) SatisfiesDependency(depTask *Task) bool {
	for _, dep := range t.DependsOn {
		if dep.TaskId == depTask.Id {
			switch dep.Status {
			case evergreen.TaskSucceeded, "":
				return depTask.Status == evergreen.TaskSucceeded
			case evergreen.TaskFailed:
				return depTask.Status == evergreen.TaskFailed
			case AllStatuses:
				return depTask.Status == evergreen.TaskFailed || depTask.Status == evergreen.TaskSucceeded || depTask.Blocked()
			}
		}
	}
	return false
}

func (t *Task) IsPatchRequest() bool {
	return utility.StringSliceContains(evergreen.PatchRequesters, t.Requester)
}

// IsUnfinishedSystemUnresponsive returns true only if this is an unfinished system unresponsive task (i.e. not on max execution)
func (t *Task) IsUnfinishedSystemUnresponsive() bool {
	return t.isSystemUnresponsive() && t.Execution < evergreen.MaxTaskExecution
}

func (t *Task) isSystemUnresponsive() bool {
	// this is a legacy case
	if t.Status == evergreen.TaskSystemUnresponse {
		return true
	}

	if t.Details.Type == evergreen.CommandTypeSystem && t.Details.TimedOut && t.Details.Description == evergreen.TaskDescriptionHeartbeat {
		return true
	}
	return false
}

func (t *Task) SetOverrideDependencies(userID string) error {
	t.OverrideDependencies = true
	event.LogTaskDependenciesOverridden(t.Id, t.Execution, userID)
	return UpdateOne(
		bson.M{
			IdKey: t.Id,
		},
		bson.M{
			"$set": bson.M{
				OverrideDependenciesKey: true,
			},
		},
	)
}

func (t *Task) AddDependency(d Dependency) error {
	// ensure the dependency doesn't already exist
	for _, existingDependency := range t.DependsOn {
		if d.TaskId == t.Id {
			grip.Error(message.Fields{
				"message": "task is attempting to add a dependency on itself, skipping this dependency",
				"task_id": t.Id,
				"stack":   string(debug.Stack()),
			})
			return nil
		}
		if existingDependency.TaskId == d.TaskId && existingDependency.Status == d.Status {
			if existingDependency.Unattainable == d.Unattainable {
				return nil // nothing to be done
			}
			return errors.Wrapf(t.MarkUnattainableDependency(existingDependency.TaskId, d.Unattainable),
				"updating matching dependency '%s' for task '%s'", existingDependency.TaskId, t.Id)
		}
	}
	t.DependsOn = append(t.DependsOn, d)
	return UpdateOne(
		bson.M{
			IdKey: t.Id,
		},
		bson.M{
			"$push": bson.M{
				DependsOnKey: d,
			},
		},
	)
}

func (t *Task) RemoveDependency(dependencyId string) error {
	found := false
	for i := len(t.DependsOn) - 1; i >= 0; i-- {
		d := t.DependsOn[i]
		if d.TaskId == dependencyId {
			var dependsOn []Dependency
			dependsOn = append(dependsOn, t.DependsOn[:i]...)
			dependsOn = append(dependsOn, t.DependsOn[i+1:]...)
			t.DependsOn = dependsOn
			found = true
			break
		}
	}
	if !found {
		return errors.Errorf("dependency '%s' not found", dependencyId)
	}

	query := bson.M{IdKey: t.Id}
	update := bson.M{
		"$pull": bson.M{
			DependsOnKey: bson.M{
				DependencyTaskIdKey: dependencyId,
			},
		},
	}
	return db.Update(Collection, query, update)
}

// DependenciesMet checks whether the dependencies for the task have all completed successfully.
// If any of the dependencies exist in the map that is passed in, they are
// used to check rather than fetching from the database. All queries
// are cached back into the map for later use.
func (t *Task) DependenciesMet(depCaches map[string]Task) (bool, error) {
	if len(t.DependsOn) == 0 || t.OverrideDependencies || !utility.IsZeroTime(t.DependenciesMetTime) {
		return true, nil
	}

	_, err := t.populateDependencyTaskCache(depCaches)
	if err != nil {
		return false, errors.WithStack(err)
	}

	for _, dependency := range t.DependsOn {
		depTask, exists := depCaches[dependency.TaskId]
		if !exists {
			foundTask, err := FindOneId(dependency.TaskId)
			if err != nil {
				return false, errors.Wrap(err, "finding dependency")
			}
			if foundTask == nil {
				return false, errors.Errorf("dependency '%s' not found", dependency.TaskId)
			}
			depTask = *foundTask
			depCaches[depTask.Id] = depTask
		}
		if !t.SatisfiesDependency(&depTask) {
			return false, nil
		}
	}
	// this is not exact, but depTask.FinishTime is not always set in time to use that
	t.DependenciesMetTime = time.Now()
	err = UpdateOne(
		bson.M{IdKey: t.Id},
		bson.M{
			"$set": bson.M{DependenciesMetTimeKey: t.DependenciesMetTime},
		})
	grip.Error(message.WrapError(err, message.Fields{
		"message": "task.DependenciesMet() failed to update task",
		"task_id": t.Id}))

	return true, nil
}

func (t *Task) populateDependencyTaskCache(depCache map[string]Task) ([]Task, error) {
	var deps []Task
	depIdsToQueryFor := make([]string, 0, len(t.DependsOn))
	for _, dep := range t.DependsOn {
		if cachedDep, ok := depCache[dep.TaskId]; !ok {
			depIdsToQueryFor = append(depIdsToQueryFor, dep.TaskId)
		} else {
			deps = append(deps, cachedDep)
		}
	}

	if len(depIdsToQueryFor) > 0 {
		newDeps, err := FindWithFields(ByIds(depIdsToQueryFor), StatusKey, DependsOnKey, ActivatedKey)
		if err != nil {
			return nil, errors.WithStack(err)
		}

		// add queried dependencies to the cache
		for _, newDep := range newDeps {
			deps = append(deps, newDep)
			depCache[newDep.Id] = newDep
		}
	}

	return deps, nil
}

// RefreshBlockedDependencies manually rechecks first degree dependencies
// when a task isn't marked as blocked. It returns a slice of this task's dependencies that
// need to recursively update their dependencies
func (t *Task) RefreshBlockedDependencies(depCache map[string]Task) ([]Task, error) {
	if len(t.DependsOn) == 0 || t.OverrideDependencies {
		return nil, nil
	}

	// do this early to avoid caching tasks we won't need.
	for _, dep := range t.DependsOn {
		if dep.Unattainable {
			return nil, nil
		}
	}

	_, err := t.populateDependencyTaskCache(depCache)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	blockedDeps := []Task{}
	for _, dep := range t.DependsOn {
		depTask, ok := depCache[dep.TaskId]
		if !ok {
			return nil, errors.Errorf("task '%s' is not in the cache", dep.TaskId)
		}
		if !t.SatisfiesDependency(&depTask) && (depTask.IsFinished() || depTask.Blocked()) {
			blockedDeps = append(blockedDeps, depTask)
		}
	}

	return blockedDeps, nil
}

func (t *Task) BlockedOnDeactivatedDependency(depCache map[string]Task) ([]string, error) {
	_, err := t.populateDependencyTaskCache(depCache)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	blockingDeps := []string{}
	for _, dep := range t.DependsOn {
		depTask, exists := depCache[dep.TaskId]
		if !exists {
			foundTask, err := FindOneId(dep.TaskId)
			if err != nil {
				return nil, errors.Wrap(err, "finding dependency")
			}
			if foundTask == nil {
				return nil, errors.Errorf("dependency '%s' not found", depTask.Id)
			}
			depTask = *foundTask
			depCache[depTask.Id] = depTask
		}
		if !depTask.IsFinished() && !depTask.Activated {
			blockingDeps = append(blockingDeps, depTask.Id)
		}
	}

	return blockingDeps, nil
}

// AllDependenciesSatisfied inspects the tasks first-order
// dependencies with regards to the cached tasks, and reports if all
// of the dependencies have been satisfied.
//
// If the cached tasks do not include a dependency specified by one of
// the tasks, the function returns an error.
func (t *Task) AllDependenciesSatisfied(cache map[string]Task) (bool, error) {
	if len(t.DependsOn) == 0 {
		return true, nil
	}

	catcher := grip.NewBasicCatcher()
	deps := []Task{}
	for _, dep := range t.DependsOn {
		cachedDep, ok := cache[dep.TaskId]
		if !ok {
			foundTask, err := FindOneId(dep.TaskId)
			if err != nil {
				return false, errors.Wrap(err, "finding dependency")
			}
			if foundTask == nil {
				return false, errors.Errorf("dependency '%s' not found", dep.TaskId)
			}
			cachedDep = *foundTask
			cache[dep.TaskId] = cachedDep
		}
		deps = append(deps, cachedDep)
	}

	if catcher.HasErrors() {
		return false, catcher.Resolve()
	}

	for _, depTask := range deps {
		if !t.SatisfiesDependency(&depTask) {
			return false, nil
		}
	}

	return true, nil
}

// MarkDependenciesFinished updates all direct dependencies on this task to
// cache whether or not this task has finished running.
func (t *Task) MarkDependenciesFinished(finished bool) error {
	if t.DisplayOnly {
		// This update can be skipped for display tasks since tasks are not
		// allowed to have dependencies on display tasks.
		return nil
	}

	env := evergreen.GetEnvironment()
	ctx, cancel := env.Context()
	defer cancel()

	_, err := env.DB().Collection(Collection).UpdateMany(ctx,
		bson.M{
			DependsOnKey: bson.M{"$elemMatch": bson.M{
				DependencyTaskIdKey: t.Id,
			}},
		},
		bson.M{
			"$set": bson.M{bsonutil.GetDottedKeyName(DependsOnKey, "$[elem]", DependencyFinishedKey): finished},
		},
		options.Update().SetArrayFilters(options.ArrayFilters{Filters: []interface{}{
			bson.M{bsonutil.GetDottedKeyName("elem", DependencyTaskIdKey): t.Id},
		}}),
	)
	if err != nil {
		return errors.Wrap(err, "marking finished dependencies")
	}

	return nil
}

// FindTaskOnBaseCommit returns the task that is on the base commit.
func (t *Task) FindTaskOnBaseCommit() (*Task, error) {
	return FindOne(db.Query(ByCommit(t.Revision, t.BuildVariant, t.DisplayName, t.Project, evergreen.RepotrackerVersionRequester)))
}

func (t *Task) FindTaskOnPreviousCommit() (*Task, error) {
	return FindOne(db.Query(ByPreviousCommit(t.BuildVariant, t.DisplayName, t.Project, evergreen.RepotrackerVersionRequester, t.RevisionOrderNumber)))
}

// FindIntermediateTasks returns the tasks from most recent to least recent between two tasks.
func (current *Task) FindIntermediateTasks(previous *Task) ([]Task, error) {
	intermediateTasks, err := Find(ByIntermediateRevisions(previous.RevisionOrderNumber, current.RevisionOrderNumber, current.BuildVariant,
		current.DisplayName, current.Project, current.Requester))
	if err != nil {
		return nil, err
	}

	// reverse the slice of tasks
	intermediateTasksReversed := make([]Task, len(intermediateTasks))
	for idx, t := range intermediateTasks {
		intermediateTasksReversed[len(intermediateTasks)-idx-1] = t
	}
	return intermediateTasksReversed, nil
}

// CountSimilarFailingTasks returns a count of all tasks with the same project,
// same display name, and in other buildvariants, that have failed in the same
// revision
func (t *Task) CountSimilarFailingTasks() (int, error) {
	return Count(db.Query(ByDifferentFailedBuildVariants(t.Revision, t.BuildVariant, t.DisplayName,
		t.Project, t.Requester)))
}

// Find the previously completed task for the same project +
// build variant + display name combination as the specified task
func (t *Task) PreviousCompletedTask(project string, statuses []string) (*Task, error) {
	if len(statuses) == 0 {
		statuses = evergreen.TaskCompletedStatuses
	}
	query := db.Query(ByBeforeRevisionWithStatusesAndRequesters(t.RevisionOrderNumber, statuses, t.BuildVariant,
		t.DisplayName, project, evergreen.SystemVersionRequesterTypes)).Sort([]string{"-" + RevisionOrderNumberKey})
	return FindOne(query)
}

func (t *Task) cacheExpectedDuration() error {
	return UpdateOne(
		bson.M{
			IdKey: t.Id,
		},
		bson.M{
			"$set": bson.M{
				DurationPredictionKey:     t.DurationPrediction,
				ExpectedDurationKey:       t.DurationPrediction.Value,
				ExpectedDurationStddevKey: t.DurationPrediction.StdDev,
			},
		},
	)
}

// MarkAsContainerDispatched marks that the container task has been dispatched
// to a pod.
func (t *Task) MarkAsContainerDispatched(ctx context.Context, env evergreen.Environment, podID, agentVersion string) error {
	dispatchedAt := time.Now()
	query := IsContainerTaskScheduledQuery()
	query[IdKey] = t.Id
	query[StatusKey] = evergreen.TaskUndispatched
	query[ContainerAllocatedKey] = true
	update := bson.M{
		"$set": bson.M{
			StatusKey:        evergreen.TaskDispatched,
			DispatchTimeKey:  dispatchedAt,
			LastHeartbeatKey: dispatchedAt,
			PodIDKey:         podID,
			AgentVersionKey:  agentVersion,
		},
	}
	res, err := env.DB().Collection(Collection).UpdateOne(ctx, query, update)
	if err != nil {
		return errors.Wrap(err, "updating task")
	}
	if res.ModifiedCount == 0 {
		return errors.New("task was not updated")
	}

	t.Status = evergreen.TaskDispatched
	t.DispatchTime = dispatchedAt
	t.LastHeartbeat = dispatchedAt
	t.PodID = podID
	t.AgentVersion = agentVersion

	return nil
}

// MarkAsHostDispatched marks that the task has been dispatched onto a
// particular host. If the task is part of a display task, the display task is
// also marked as dispatched to a host. Returns an error if any of the database
// updates fail.
func (t *Task) MarkAsHostDispatched(hostID, distroID, agentRevision string, dispatchTime time.Time) error {
	doUpdate := func(update bson.M) error {
		return UpdateOne(bson.M{IdKey: t.Id}, update)
	}
	if err := t.markAsHostDispatchedWithFunc(doUpdate, hostID, distroID, agentRevision, dispatchTime); err != nil {
		return err
	}

	//when dispatching an execution task, mark its parent as dispatched
	if dt, _ := t.GetDisplayTask(); dt != nil && dt.DispatchTime == utility.ZeroTime {
		return dt.MarkAsHostDispatched("", "", "", dispatchTime)
	}
	return nil
}

// MarkAsHostDispatchedWithContext marks that the task has been dispatched onto
// a particular host. Unlike MarkAsHostDispatched, this does not update the
// parent display task.
func (t *Task) MarkAsHostDispatchedWithContext(ctx context.Context, env evergreen.Environment, hostID, distroID, agentRevision string, dispatchTime time.Time) error {
	doUpdate := func(update bson.M) error {
		_, err := env.DB().Collection(Collection).UpdateByID(ctx, t.Id, update)
		return err
	}
	return t.markAsHostDispatchedWithFunc(doUpdate, hostID, distroID, agentRevision, dispatchTime)
}

func (t *Task) markAsHostDispatchedWithFunc(doUpdate func(update bson.M) error, hostID, distroID, agentRevision string, dispatchTime time.Time) error {
	if err := doUpdate(bson.M{
		"$set": bson.M{
			DispatchTimeKey:  dispatchTime,
			StatusKey:        evergreen.TaskDispatched,
			HostIdKey:        hostID,
			LastHeartbeatKey: dispatchTime,
			DistroIdKey:      distroID,
			AgentVersionKey:  agentRevision,
		},
		"$unset": bson.M{
			AbortedKey:   "",
			AbortInfoKey: "",
			DetailsKey:   "",
		},
	}); err != nil {
		return err
	}

	t.DispatchTime = dispatchTime
	t.Status = evergreen.TaskDispatched
	t.HostId = hostID
	t.AgentVersion = agentRevision
	t.LastHeartbeat = dispatchTime
	t.DistroId = distroID
	t.Aborted = false
	t.AbortInfo = AbortInfo{}
	t.Details = apimodels.TaskEndDetail{}

	return nil
}

// MarkAsHostUndispatchedWithContext marks that the host task is undispatched.
// If the task is already dispatched to a host, it aborts the dispatch by
// undoing the dispatch updates. This is the inverse operation of
// MarkAsHostDispatchedWithContext.
func (t *Task) MarkAsHostUndispatchedWithContext(ctx context.Context, env evergreen.Environment) error {
	doUpdate := func(update bson.M) error {
		_, err := env.DB().Collection(Collection).UpdateByID(ctx, t.Id, update)
		return err
	}
	return t.markAsHostUndispatchedWithFunc(doUpdate)
}

func (t *Task) markAsHostUndispatchedWithFunc(doUpdate func(update bson.M) error) error {
	update := bson.M{
		"$set": bson.M{
			StatusKey:        evergreen.TaskUndispatched,
			DispatchTimeKey:  utility.ZeroTime,
			LastHeartbeatKey: utility.ZeroTime,
		},
		"$unset": bson.M{
			HostIdKey:       "",
			AgentVersionKey: "",
			AbortedKey:      "",
			AbortInfoKey:    "",
			DetailsKey:      "",
		},
	}

	if err := doUpdate(update); err != nil {
		return err
	}

	t.Status = evergreen.TaskUndispatched
	t.DispatchTime = utility.ZeroTime
	t.LastHeartbeat = utility.ZeroTime
	t.HostId = ""
	t.AgentVersion = ""
	t.Aborted = false
	t.AbortInfo = AbortInfo{}
	t.Details = apimodels.TaskEndDetail{}

	return nil
}

// maxContainerAllocationAttempts is the maximum number of times a container
// task is allowed to try to allocate a container for a single execution.
const maxContainerAllocationAttempts = 5

// MarkAsContainerAllocated marks a container task as allocated a container.
// This will fail if the task is not in a state where it needs a container to be
// allocated to it.
func (t *Task) MarkAsContainerAllocated(ctx context.Context, env evergreen.Environment) error {
	if t.ContainerAllocated {
		return errors.New("cannot allocate a container task if it's currently allocated")
	}
	if t.RemainingContainerAllocationAttempts() == 0 {
		return errors.Errorf("task execution has hit the max allowed allocation attempts (%d)", maxContainerAllocationAttempts)
	}
	q := needsContainerAllocation()
	q[IdKey] = t.Id
	q[ContainerAllocationAttemptsKey] = bson.M{"$lt": maxContainerAllocationAttempts}

	allocatedAt := time.Now()
	update, err := env.DB().Collection(Collection).UpdateOne(ctx, q, bson.M{
		"$set": bson.M{
			ContainerAllocatedKey:     true,
			ContainerAllocatedTimeKey: allocatedAt,
		},
		"$inc": bson.M{
			ContainerAllocationAttemptsKey: 1,
		},
	})
	if err != nil {
		return err
	}
	if update.ModifiedCount == 0 {
		return errors.New("task was not updated")
	}

	t.ContainerAllocated = true
	t.ContainerAllocatedTime = allocatedAt

	return nil
}

func containerDeallocatedUpdate() bson.M {
	return bson.M{
		"$set": bson.M{
			ContainerAllocatedKey: false,
		},
		"$unset": bson.M{
			ContainerAllocatedTimeKey: 1,
		},
	}
}

// MarkAsContainerDeallocated marks a container task that was allocated as no
// longer allocated a container.
func (t *Task) MarkAsContainerDeallocated(ctx context.Context, env evergreen.Environment) error {
	if !t.ContainerAllocated {
		return errors.New("cannot deallocate a container task if it's not currently allocated")
	}

	res, err := env.DB().Collection(Collection).UpdateOne(ctx, bson.M{
		IdKey:                 t.Id,
		ExecutionPlatformKey:  ExecutionPlatformContainer,
		ContainerAllocatedKey: true,
	}, containerDeallocatedUpdate())
	if err != nil {
		return errors.Wrap(err, "updating task")
	}
	if res.ModifiedCount == 0 {
		return errors.New("task was not updated")
	}

	t.ContainerAllocated = false
	t.ContainerAllocatedTime = time.Time{}

	return nil
}

// MarkTasksAsContainerDeallocated marks multiple container tasks as no longer
// allocated containers.
func MarkTasksAsContainerDeallocated(taskIDs []string) error {
	if len(taskIDs) == 0 {
		return nil
	}

	if _, err := UpdateAll(bson.M{
		IdKey:                bson.M{"$in": taskIDs},
		ExecutionPlatformKey: ExecutionPlatformContainer,
	}, containerDeallocatedUpdate()); err != nil {
		return errors.Wrap(err, "updating tasks")
	}

	return nil
}

// MarkGeneratedTasks marks that the task has generated tasks.
func MarkGeneratedTasks(taskID string) error {
	query := bson.M{
		IdKey:             taskID,
		GeneratedTasksKey: bson.M{"$exists": false},
	}
	update := bson.M{
		"$set": bson.M{
			GeneratedTasksKey: true,
		},
		"$unset": bson.M{
			GenerateTasksErrorKey: 1,
		},
	}
	err := UpdateOne(query, update)
	if adb.ResultsNotFound(err) {
		return nil
	}
	return errors.Wrap(err, "marking generate.tasks complete")
}

// MarkGeneratedTasksErr marks that the task hit errors generating tasks.
func MarkGeneratedTasksErr(taskID string, errorToSet error) error {
	if errorToSet == nil || adb.ResultsNotFound(errorToSet) || db.IsDuplicateKey(errorToSet) {
		return nil
	}
	query := bson.M{
		IdKey:             taskID,
		GeneratedTasksKey: bson.M{"$exists": false},
	}
	update := bson.M{
		"$set": bson.M{
			GenerateTasksErrorKey: errorToSet.Error(),
		},
	}
	err := UpdateOne(query, update)
	if adb.ResultsNotFound(err) {
		return nil
	}
	return errors.Wrap(err, "setting generate.tasks error")
}

// GenerateNotRun returns tasks that have requested to generate tasks.
func GenerateNotRun() ([]Task, error) {
	const maxGenerateTimeAgo = 24 * time.Hour
	return FindAll(db.Query(bson.M{
		StatusKey:                evergreen.TaskStarted,                              // task is running
		StartTimeKey:             bson.M{"$gt": time.Now().Add(-maxGenerateTimeAgo)}, // ignore older tasks, just in case
		GeneratedTasksKey:        bson.M{"$ne": true},                                // generate.tasks has not yet run
		GeneratedJSONAsStringKey: bson.M{"$exists": true},                            // config has been posted by generate.tasks command
	}))
}

// SetGeneratedJSON sets JSON data to generate tasks from.
func (t *Task) SetGeneratedJSON(json []json.RawMessage) error {
	if len(t.GeneratedJSONAsString) > 0 {
		return nil
	}
	s := []string{}
	for _, j := range json {
		s = append(s, string(j))
	}
	t.GeneratedJSONAsString = s
	return UpdateOne(
		bson.M{
			IdKey:                    t.Id,
			GeneratedJSONAsStringKey: bson.M{"$exists": false},
		},
		bson.M{
			"$set": bson.M{
				GeneratedJSONAsStringKey: s,
			},
		},
	)
}

// SetGeneratedTasksToActivate adds a task to stepback after activation
func (t *Task) SetGeneratedTasksToActivate(buildVariantName, taskName string) error {
	return UpdateOne(
		bson.M{
			IdKey: t.Id,
		},
		bson.M{
			"$addToSet": bson.M{
				bsonutil.GetDottedKeyName(GeneratedTasksToActivateKey, buildVariantName): taskName,
			},
		},
	)
}

// SetTasksScheduledTime takes a list of tasks and a time, and then sets
// the scheduled time in the database for the tasks if it is currently unset
func SetTasksScheduledTime(tasks []Task, scheduledTime time.Time) error {
	ids := []string{}
	for i := range tasks {
		tasks[i].ScheduledTime = scheduledTime
		ids = append(ids, tasks[i].Id)

		// Display tasks are considered scheduled when their first exec task is scheduled
		if tasks[i].IsPartOfDisplay() {
			ids = append(ids, utility.FromStringPtr(tasks[i].DisplayTaskId))
		}
	}
	_, err := UpdateAll(
		bson.M{
			IdKey: bson.M{
				"$in": ids,
			},
			ScheduledTimeKey: bson.M{
				"$lte": utility.ZeroTime,
			},
		},
		bson.M{
			"$set": bson.M{
				ScheduledTimeKey: scheduledTime,
			},
		},
	)
	if err != nil {
		return err
	}

	return nil
}

// UnscheduleStaleUnderwaterHostTasks Removes host tasks older than the unscheduable threshold (e.g. one week) from
// the scheduler queue.
// If you pass an empty string as an argument to this function, this operation
// will select tasks from all distros.
func UnscheduleStaleUnderwaterHostTasks(ctx context.Context, distroID string) (int, error) {
	query := schedulableHostTasksQuery()

	if err := addApplicableDistroFilter(ctx, distroID, DistroIdKey, query); err != nil {
		return 0, errors.WithStack(err)
	}

	query[ActivatedTimeKey] = bson.M{"$lte": time.Now().Add(-UnschedulableThreshold)}

	update := bson.M{
		"$set": bson.M{
			PriorityKey:  evergreen.DisabledTaskPriority,
			ActivatedKey: false,
		},
	}

	// Force the query to use 'distro_1_status_1_activated_1_priority_1_override_dependencies_1_unattainable_dependency_1'
	// instead of defaulting to 'status_1_depends_on.status_1_depends_on.unattainable_1'.
	info, err := UpdateAllWithHint(query, update, ActivatedTasksByDistroIndex)
	if err != nil {
		return 0, errors.Wrap(err, "unscheduling stale underwater tasks")
	}

	return info.Updated, nil
}

// LegacyDeactivateStepbackTasksForProject deactivates and aborts any scheduled/running tasks
// for this project that were activated by stepback.
// TODO: remove as part of EVG-17947
func LegacyDeactivateStepbackTasksForProject(projectId, caller string) error {
	tasks, err := FindActivatedStepbackTasks(projectId)
	if err != nil {
		return errors.Wrap(err, "finding activated stepback tasks")
	}

	if err = DeactivateTasks(tasks, true, caller); err != nil {
		return errors.Wrap(err, "deactivating active stepback tasks")
	}

	grip.InfoWhen(len(tasks) > 0, message.Fields{
		"message":    "deactivated active stepback tasks",
		"project_id": projectId,
		"user":       caller,
		"num_tasks":  len(tasks),
	})

	abortTaskIds := []string{}
	for _, t := range tasks {
		if t.IsAbortable() {
			abortTaskIds = append(abortTaskIds, t.Id)
			event.LogTaskAbortRequest(t.Id, t.Execution, caller)
		}
	}
	if err = SetManyAborted(abortTaskIds, AbortInfo{User: caller}); err != nil {
		return errors.Wrap(err, "aborting in progress tasks")
	}

	return nil
}

// DeactivateStepbackTask deactivates and aborts the matching stepback task.
func DeactivateStepbackTask(projectId, buildVariantName, taskName, caller string) error {
	t, err := FindActivatedStepbackTaskByName(projectId, buildVariantName, taskName)
	if err != nil {
		return err
	}
	if t == nil {
		return errors.Errorf("no stepback task '%s' for variant '%s' found", taskName, buildVariantName)
	}

	if err = t.DeactivateTask(caller); err != nil {
		return errors.Wrap(err, "deactivating stepback task")
	}
	if t.IsAbortable() {
		event.LogTaskAbortRequest(t.Id, t.Execution, caller)
		if err = t.SetAborted(AbortInfo{User: caller}); err != nil {
			return errors.Wrap(err, "setting task aborted")
		}
	}
	return nil
}

// MarkFailed changes the state of the task to failed.
func (t *Task) MarkFailed() error {
	t.Status = evergreen.TaskFailed
	return UpdateOne(
		bson.M{
			IdKey: t.Id,
		},
		bson.M{
			"$set": bson.M{
				StatusKey: evergreen.TaskFailed,
			},
		},
	)
}

func (t *Task) MarkSystemFailed(description string) error {
	t.FinishTime = time.Now()
	t.Details = GetSystemFailureDetails(description)

	switch t.ExecutionPlatform {
	case ExecutionPlatformHost:
		event.LogHostTaskFinished(t.Id, t.Execution, t.HostId, evergreen.TaskSystemFailed)
	case ExecutionPlatformContainer:
		event.LogContainerTaskFinished(t.Id, t.Execution, t.PodID, evergreen.TaskSystemFailed)
	default:
		event.LogTaskFinished(t.Id, t.Execution, evergreen.TaskSystemFailed)
	}
	grip.Info(message.Fields{
		"message":            "marking task system failed",
		"included_on":        evergreen.ContainerHealthDashboard,
		"task_id":            t.Id,
		"execution":          t.Execution,
		"status":             t.Status,
		"host_id":            t.HostId,
		"pod_id":             t.PodID,
		"description":        description,
		"execution_platform": t.ExecutionPlatform,
	})

	return t.MarkEnd(t.FinishTime, &t.Details)
}

// GetSystemFailureDetails returns a task's end details based on an input description.
func GetSystemFailureDetails(description string) apimodels.TaskEndDetail {
	details := apimodels.TaskEndDetail{
		Status:      evergreen.TaskFailed,
		Type:        evergreen.CommandTypeSystem,
		Description: description,
	}
	if description == evergreen.TaskDescriptionHeartbeat {
		details.TimedOut = true
	}
	return details
}

func SetManyAborted(taskIds []string, reason AbortInfo) error {
	return UpdateOne(
		ByIds(taskIds),
		bson.M{
			"$set": bson.M{
				AbortedKey:   true,
				AbortInfoKey: reason,
			},
		},
	)
}

// SetAborted sets the abort field of task to aborted
func (t *Task) SetAborted(reason AbortInfo) error {
	t.Aborted = true
	return UpdateOne(
		bson.M{
			IdKey: t.Id,
		},
		bson.M{
			"$set": bson.M{
				AbortedKey:   true,
				AbortInfoKey: reason,
			},
		},
	)
}

// SetStepbackDepth adds the stepback depth to the task.
func (t *Task) SetStepbackDepth(stepbackDepth int) error {
	t.StepbackDepth = stepbackDepth
	return UpdateOne(
		bson.M{
			IdKey: t.Id,
		},
		bson.M{
			"$set": bson.M{
				StepbackDepthKey: stepbackDepth,
			},
		})
}

// SetLogServiceVersion sets the log service version used to write logs for the
// task.
func (t *Task) SetLogServiceVersion(ctx context.Context, env evergreen.Environment, version int) error {
	if t.DisplayOnly {
		return errors.New("cannot set log service version on a display task")
	}
	if t.LogServiceVersion != nil {
		return errors.New("log service version already set")
	}

	res, err := env.DB().Collection(Collection).UpdateByID(ctx, t.Id, []bson.M{
		{
			"$set": bson.M{LogServiceVersionKey: bson.M{
				"$ifNull": bson.A{
					"$" + LogServiceVersionKey,
					version,
				}},
			},
		},
	})
	if err != nil {
		return errors.Wrap(err, "setting the log service version")
	}
	if res.MatchedCount == 0 {
		return errors.New("programmatic error: task not found")
	}
	if res.ModifiedCount == 0 {
		return errors.New("log service version already set")
	}
	t.LogServiceVersion = utility.ToIntPtr(version)

	return nil
}

// SetResultsInfo sets the task's test results info.
//
// Note that if failedResults is false, ResultsFailed is not set. This is
// because in cases where multiple calls to attach test results are made for a
// task, only one call needs to have a test failure for the ResultsFailed field
// to be set to true.
func (t *Task) SetResultsInfo(service string, failedResults bool) error {
	if t.DisplayOnly {
		return errors.New("cannot set results info on a display task")
	}
	if t.ResultsService != "" {
		if t.ResultsService != service {
			return errors.New("cannot use more than one test results service for a task")
		}
		if !failedResults {
			return nil
		}
	}

	t.ResultsService = service
	set := bson.M{ResultsServiceKey: service}
	if failedResults {
		t.ResultsFailed = true
		set[ResultsFailedKey] = true
	}

	return errors.WithStack(UpdateOne(ById(t.Id), bson.M{"$set": set}))
}

// HasResults returns whether the task has test results or not.
func (t *Task) HasResults() bool {
	if t.DisplayOnly && len(t.ExecutionTasks) > 0 {
		hasResults := []bson.M{{ResultsServiceKey: bson.M{"$exists": true}}, {HasCedarResultsKey: true}}
		if t.Archived {
			execTasks, err := FindByExecutionTasksAndMaxExecution(t.ExecutionTasks, t.Execution, bson.E{Key: "$or", Value: hasResults})
			if err != nil {
				grip.Error(message.WrapError(err, message.Fields{
					"message": "getting execution tasks for archived display task",
				}))
			}

			return len(execTasks) > 0
		} else {
			query := ByIds(t.ExecutionTasks)
			query["$or"] = hasResults
			execTasksWithResults, err := Count(db.Query(query))
			if err != nil {
				grip.Error(message.WrapError(err, message.Fields{
					"message": "getting count of execution tasks with results for display task",
				}))
			}

			return execTasksWithResults > 0
		}
	}

	return t.ResultsService != "" || t.HasCedarResults
}

// ActivateTask will set the ActivatedBy field to the caller and set the active state to be true.
// Also activates dependencies of the task.
func (t *Task) ActivateTask(caller string) error {
	t.ActivatedBy = caller
	t.Activated = true
	t.ActivatedTime = time.Now()

	return ActivateTasks([]Task{*t}, t.ActivatedTime, true, caller)
}

// ActivateTasks sets all given tasks to active, logs them as activated, and proceeds to activate any dependencies that were deactivated.
func ActivateTasks(tasks []Task, activationTime time.Time, updateDependencies bool, caller string) error {
	tasksToActivate := make([]Task, 0, len(tasks))
	taskIDs := make([]string, 0, len(tasks))
	for _, t := range tasks {
		// Activating an activated task is a noop.
		if t.Activated {
			continue
		}
		tasksToActivate = append(tasksToActivate, t)
		taskIDs = append(taskIDs, t.Id)
	}
	err := activateTasks(taskIDs, caller, activationTime)
	if err != nil {
		return errors.Wrap(err, "activating tasks")
	}
	logs := []event.EventLogEntry{}
	for _, t := range tasksToActivate {
		logs = append(logs, event.GetTaskActivatedEvent(t.Id, t.Execution, caller))
	}
	grip.Error(message.WrapError(event.LogManyEvents(logs), message.Fields{
		"message":  "problem logging task activated events",
		"task_ids": taskIDs,
		"caller":   caller,
	}))

	if updateDependencies {
		return ActivateDeactivatedDependencies(taskIDs, caller)
	}
	return nil
}

// ActivateTasksByIdsWithDependencies activates the given tasks and their dependencies.
func ActivateTasksByIdsWithDependencies(ids []string, caller string) error {
	q := db.Query(bson.M{
		IdKey:     bson.M{"$in": ids},
		StatusKey: evergreen.TaskUndispatched,
	})

	tasks, err := FindAll(q.WithFields(IdKey, DependsOnKey, ExecutionKey, ActivatedKey))
	if err != nil {
		return errors.Wrap(err, "getting tasks for activation")
	}
	dependOn, err := GetRecursiveDependenciesUp(tasks, nil)
	if err != nil {
		return errors.Wrap(err, "getting recursive dependencies")
	}

	if err = ActivateTasks(append(tasks, dependOn...), time.Now(), true, caller); err != nil {
		return errors.Wrap(err, "updating tasks for activation")
	}
	return nil
}

// ActivateDeactivatedDependencies activates tasks that depend on these tasks which were deactivated because a task
// they depended on was deactivated. Only activate when all their dependencies are activated or are being activated
func ActivateDeactivatedDependencies(tasks []string, caller string) error {
	taskMap := make(map[string]bool)
	for _, t := range tasks {
		taskMap[t] = true
	}

	tasksDependingOnTheseTasks, err := getRecursiveDependenciesDown(tasks, nil)
	if err != nil {
		return errors.Wrap(err, "getting recursive dependencies down")
	}

	// do a topological sort so we've dealt with
	// all a task's dependencies by the time we get up to it
	sortedDependencies, err := topologicalSort(tasksDependingOnTheseTasks)
	if err != nil {
		return errors.WithStack(err)
	}

	// get dependencies we don't have yet and add them to a map
	tasksToGet := []string{}
	depTaskMap := make(map[string]bool)
	for _, t := range sortedDependencies {
		depTaskMap[t.Id] = true

		if t.Activated || !t.DeactivatedForDependency {
			continue
		}

		for _, dep := range t.DependsOn {
			if !taskMap[dep.TaskId] && !depTaskMap[dep.TaskId] {
				tasksToGet = append(tasksToGet, dep.TaskId)
			}
		}
	}

	missingTaskMap := make(map[string]Task)
	if len(tasksToGet) > 0 {
		var missingTasks []Task
		missingTasks, err = FindAll(db.Query(bson.M{IdKey: bson.M{"$in": tasksToGet}}).WithFields(ActivatedKey))
		if err != nil {
			return errors.Wrap(err, "getting missing tasks")
		}
		for _, t := range missingTasks {
			missingTaskMap[t.Id] = t
		}
	}

	tasksToActivate := make(map[string]Task)
	for _, t := range sortedDependencies {
		if t.Activated || !t.DeactivatedForDependency {
			continue
		}

		depsSatisfied := true
		for _, dep := range t.DependsOn {
			// not being activated now
			if _, ok := tasksToActivate[dep.TaskId]; !ok && !taskMap[dep.TaskId] {
				// and not already activated
				if depTask := missingTaskMap[dep.TaskId]; !depTask.Activated {
					depsSatisfied = false
					break
				}
			}
		}
		if depsSatisfied {
			tasksToActivate[t.Id] = t
		}
	}

	if len(tasksToActivate) == 0 {
		return nil
	}

	taskIDsToActivate := make([]string, 0, len(tasksToActivate))
	for _, t := range tasksToActivate {
		taskIDsToActivate = append(taskIDsToActivate, t.Id)
	}
	_, err = UpdateAll(
		bson.M{IdKey: bson.M{"$in": taskIDsToActivate}},
		[]bson.M{
			{
				"$set": bson.M{
					ActivatedKey:                true,
					DeactivatedForDependencyKey: false,
					ActivatedByKey:              caller,
					ActivatedTimeKey:            time.Now(),
					// TODO: (EVG-20334) Remove this field and the aggregation update once old tasks without the UnattainableDependency field have TTLed.
					UnattainableDependencyKey: bson.M{"$cond": bson.M{
						"if":   bson.M{"$isArray": "$" + bsonutil.GetDottedKeyName(DependsOnKey, DependencyUnattainableKey)},
						"then": bson.M{"$anyElementTrue": "$" + bsonutil.GetDottedKeyName(DependsOnKey, DependencyUnattainableKey)},
						"else": false,
					}},
				},
			},
		},
	)
	if err != nil {
		return errors.Wrap(err, "updating activation for dependencies")
	}

	logs := []event.EventLogEntry{}
	for _, t := range tasksToActivate {
		logs = append(logs, event.GetTaskActivatedEvent(t.Id, t.Execution, caller))
	}
	grip.Error(message.WrapError(event.LogManyEvents(logs), message.Fields{
		"message":  "problem logging task activated events",
		"task_ids": taskIDsToActivate,
		"caller":   caller,
	}))

	return nil
}

func topologicalSort(tasks []Task) ([]Task, error) {
	var fromTask, toTask string
	defer func() {
		taskIds := []string{}
		for _, t := range tasks {
			taskIds = append(taskIds, t.Id)
		}
		panicErr := recovery.HandlePanicWithError(recover(), nil, "problem adding edge")
		grip.Error(message.WrapError(panicErr, message.Fields{
			"function":       "topologicalSort",
			"from_task":      fromTask,
			"to_task":        toTask,
			"original_tasks": taskIds,
		}))
	}()
	depGraph := simple.NewDirectedGraph()
	taskNodeMap := make(map[string]graph.Node)
	nodeTaskMap := make(map[int64]Task)

	for _, task := range tasks {
		node := depGraph.NewNode()
		depGraph.AddNode(node)
		nodeTaskMap[node.ID()] = task
		taskNodeMap[task.Id] = node
	}

	for _, task := range tasks {
		for _, dep := range task.DependsOn {
			fromTask = dep.TaskId
			if toNode, ok := taskNodeMap[fromTask]; ok {
				toTask = task.Id
				edge := simple.Edge{
					F: simple.Node(toNode.ID()),
					T: simple.Node(taskNodeMap[toTask].ID()),
				}
				depGraph.SetEdge(edge)
			}
		}
	}

	sorted, err := topo.Sort(depGraph)
	if err != nil {
		return nil, errors.Wrap(err, "topologically sorting dependency graph")
	}
	sortedTasks := make([]Task, 0, len(tasks))
	for _, node := range sorted {
		sortedTasks = append(sortedTasks, nodeTaskMap[node.ID()])
	}

	return sortedTasks, nil
}

// DeactivateTask will set the ActivatedBy field to the caller and set the active state to be false and deschedule the task
func (t *Task) DeactivateTask(caller string) error {
	t.ActivatedBy = caller
	t.Activated = false
	t.ScheduledTime = utility.ZeroTime

	return DeactivateTasks([]Task{*t}, true, caller)
}

func DeactivateTasks(tasks []Task, updateDependencies bool, caller string) error {
	taskIDs := make([]string, 0, len(tasks))
	for _, t := range tasks {
		if t.DisplayOnly {
			taskIDs = append(taskIDs, t.ExecutionTasks...)
		}
		taskIDs = append(taskIDs, t.Id)
	}

	_, err := UpdateAll(
		bson.M{
			IdKey: bson.M{"$in": taskIDs},
		},
		bson.M{
			"$set": bson.M{
				ActivatedKey:     false,
				ActivatedByKey:   caller,
				ScheduledTimeKey: utility.ZeroTime,
			},
		},
	)
	if err != nil {
		return errors.Wrap(err, "deactivating tasks")
	}

	logs := []event.EventLogEntry{}
	for _, t := range tasks {
		logs = append(logs, event.GetTaskDeactivatedEvent(t.Id, t.Execution, caller))
	}
	grip.Error(message.WrapError(event.LogManyEvents(logs), message.Fields{
		"message":  "problem logging task deactivated events",
		"task_ids": taskIDs,
		"caller":   caller,
	}))

	if updateDependencies {
		return DeactivateDependencies(taskIDs, caller)
	}
	return nil
}

func DeactivateDependencies(tasks []string, caller string) error {
	tasksDependingOnTheseTasks, err := getRecursiveDependenciesDown(tasks, nil)
	if err != nil {
		return errors.Wrap(err, "getting recursive dependencies down")
	}

	tasksToUpdate := make([]Task, 0, len(tasksDependingOnTheseTasks))
	taskIDsToUpdate := make([]string, 0, len(tasksDependingOnTheseTasks))
	for _, t := range tasksDependingOnTheseTasks {
		if t.Activated {
			tasksToUpdate = append(tasksToUpdate, t)
			taskIDsToUpdate = append(taskIDsToUpdate, t.Id)
		}
	}

	if len(tasksToUpdate) == 0 {
		return nil
	}

	_, err = UpdateAll(
		bson.M{
			IdKey: bson.M{"$in": taskIDsToUpdate},
		},
		bson.M{"$set": bson.M{
			ActivatedKey:                false,
			DeactivatedForDependencyKey: true,
			ScheduledTimeKey:            utility.ZeroTime,
		}},
	)
	if err != nil {
		return errors.Wrap(err, "deactivating dependencies")
	}

	logs := []event.EventLogEntry{}
	for _, t := range tasksToUpdate {
		logs = append(logs, event.GetTaskDeactivatedEvent(t.Id, t.Execution, caller))
	}
	grip.Error(message.WrapError(event.LogManyEvents(logs), message.Fields{
		"message":  "problem logging task deactivated events",
		"task_ids": taskIDsToUpdate,
		"caller":   caller,
	}))

	return nil
}

// MarkEnd handles the Task updates associated with ending a task. If the task's start time is zero
// at this time, it will set it to the finish time minus the timeout time.
func (t *Task) MarkEnd(finishTime time.Time, detail *apimodels.TaskEndDetail) error {
	// if there is no start time set, either set it to the create time
	// or set 2 hours previous to the finish time.
	if utility.IsZeroTime(t.StartTime) {
		timedOutStart := finishTime.Add(-2 * time.Hour)
		t.StartTime = timedOutStart
		if timedOutStart.Before(t.IngestTime) {
			t.StartTime = t.IngestTime
		}
	}

	t.TimeTaken = finishTime.Sub(t.StartTime)

	grip.Debug(message.Fields{
		"message":   "marking task finished",
		"task_id":   t.Id,
		"execution": t.Execution,
		"project":   t.Project,
		"details":   t.Details,
	})
	if detail.IsEmpty() {
		grip.Debug(message.Fields{
			"message":   "detail status was empty, setting to failed",
			"task_id":   t.Id,
			"execution": t.Execution,
			"project":   t.Project,
			"details":   t.Details,
		})
		detail = &apimodels.TaskEndDetail{
			Status: evergreen.TaskFailed,
		}
	}

	// record that the task has finished, in memory and in the db
	t.Status = detail.Status
	t.FinishTime = finishTime
	t.Details = *detail
	t.ContainerAllocated = false
	t.ContainerAllocatedTime = time.Time{}
	return UpdateOne(
		bson.M{
			IdKey: t.Id,
		},
		bson.M{
			"$set": bson.M{
				FinishTimeKey:         finishTime,
				StatusKey:             detail.Status,
				TimeTakenKey:          t.TimeTaken,
				DetailsKey:            detail,
				StartTimeKey:          t.StartTime,
				ContainerAllocatedKey: false,
			},
			"$unset": bson.M{
				ContainerAllocatedTimeKey: 1,
			},
		})

}

// GetDisplayStatus finds and sets DisplayStatus to the task. It should reflect
// the statuses assigned during the addDisplayStatus aggregation step.
func (t *Task) GetDisplayStatus() string {
	if t.DisplayStatus != "" {
		return t.DisplayStatus
	}
	t.DisplayStatus = t.findDisplayStatus()
	return t.DisplayStatus
}

func (t *Task) findDisplayStatus() string {
	if t.Aborted {
		return evergreen.TaskAborted
	}
	if t.Status == evergreen.TaskSucceeded {
		return evergreen.TaskSucceeded
	}
	if t.Details.Type == evergreen.CommandTypeSetup {
		return evergreen.TaskSetupFailed
	}
	if t.Details.Type == evergreen.CommandTypeSystem {
		if t.Details.TimedOut && t.Details.Description == evergreen.TaskDescriptionHeartbeat {
			return evergreen.TaskSystemUnresponse
		}
		if t.Details.TimedOut {
			return evergreen.TaskSystemTimedOut
		}
		return evergreen.TaskSystemFailed
	}
	if t.Details.TimedOut {
		return evergreen.TaskTimedOut
	}
	if t.Status == evergreen.TaskUndispatched {
		if !t.Activated {
			return evergreen.TaskUnscheduled
		}
		if t.Blocked() {
			return evergreen.TaskStatusBlocked
		}
		return evergreen.TaskWillRun
	}
	return t.Status
}

// displayTaskPriority answers the question "if there is a display task whose executions are
// in these statuses, which overall status would a user expect to see?"
// for example, if there are both successful and failed tasks, one would expect to see "failed"
func (t *Task) displayTaskPriority() int {
	switch t.GetDisplayStatus() {
	case evergreen.TaskStarted:
		return 10
	case evergreen.TaskFailed:
		return 20
	case evergreen.TaskTestTimedOut:
		return 30
	case evergreen.TaskTimedOut:
		return 40
	case evergreen.TaskSystemFailed:
		return 50
	case evergreen.TaskSystemTimedOut:
		return 60
	case evergreen.TaskSystemUnresponse:
		return 70
	case evergreen.TaskSetupFailed:
		return 80
	case evergreen.TaskUndispatched:
		return 90
	case evergreen.TaskInactive:
		return 100
	case evergreen.TaskSucceeded:
		return 110
	}
	// Note that this includes evergreen.TaskDispatched.
	return 1000
}

// Reset sets the task state to a state in which it is scheduled to re-run.
func (t *Task) Reset(ctx context.Context) error {
	return UpdateOneContext(ctx,
		bson.M{
			IdKey:       t.Id,
			StatusKey:   bson.M{"$in": evergreen.TaskCompletedStatuses},
			CanResetKey: true,
		},
		resetTaskUpdate(t),
	)
}

// ResetTasks performs the same DB updates as (*Task).Reset, but resets many
// tasks instead of a single one.
func ResetTasks(tasks []Task) error {
	if len(tasks) == 0 {
		return nil
	}
	var taskIDs []string
	for _, t := range tasks {
		taskIDs = append(taskIDs, t.Id)
	}

	if _, err := UpdateAll(
		bson.M{
			IdKey:       bson.M{"$in": taskIDs},
			StatusKey:   bson.M{"$in": evergreen.TaskCompletedStatuses},
			CanResetKey: true,
		},
		resetTaskUpdate(nil),
	); err != nil {
		return err
	}

	return nil
}

func resetTaskUpdate(t *Task) []bson.M {
	newSecret := utility.RandomString()
	now := time.Now()
	if t != nil {
		t.Activated = true
		t.ActivatedTime = now
		t.Secret = newSecret
		t.HostId = ""
		t.PodID = ""
		t.Status = evergreen.TaskUndispatched
		t.DispatchTime = utility.ZeroTime
		t.StartTime = utility.ZeroTime
		t.ScheduledTime = utility.ZeroTime
		t.FinishTime = utility.ZeroTime
		t.DependenciesMetTime = utility.ZeroTime
		t.TimeTaken = 0
		t.LastHeartbeat = utility.ZeroTime
		t.Details = apimodels.TaskEndDetail{}
		t.LogServiceVersion = nil
		t.ResultsService = ""
		t.ResultsFailed = false
		t.HasCedarResults = false
		t.ResetWhenFinished = false
		t.ResetFailedWhenFinished = false
		t.AgentVersion = ""
		t.HostCreateDetails = []HostCreateDetail{}
		t.OverrideDependencies = false
		t.ContainerAllocationAttempts = 0
		t.CanReset = false
	}
	update := []bson.M{
		{
			"$set": bson.M{
				ActivatedKey:                   true,
				ActivatedTimeKey:               now,
				SecretKey:                      newSecret,
				StatusKey:                      evergreen.TaskUndispatched,
				DispatchTimeKey:                utility.ZeroTime,
				StartTimeKey:                   utility.ZeroTime,
				ScheduledTimeKey:               utility.ZeroTime,
				FinishTimeKey:                  utility.ZeroTime,
				DependenciesMetTimeKey:         utility.ZeroTime,
				TimeTakenKey:                   0,
				LastHeartbeatKey:               utility.ZeroTime,
				ContainerAllocationAttemptsKey: 0,
				// TODO: (EVG-20334) Remove this field and the aggregation update once old tasks without the UnattainableDependency field have TTLed.
				UnattainableDependencyKey: bson.M{"$cond": bson.M{
					"if":   bson.M{"$isArray": "$" + bsonutil.GetDottedKeyName(DependsOnKey, DependencyUnattainableKey)},
					"then": bson.M{"$anyElementTrue": "$" + bsonutil.GetDottedKeyName(DependsOnKey, DependencyUnattainableKey)},
					"else": false,
				}},
			},
		},
		{
			"$unset": []string{
				DetailsKey,
				LogServiceVersionKey,
				ResultsServiceKey,
				ResultsFailedKey,
				HasCedarResultsKey,
				ResetWhenFinishedKey,
				ResetFailedWhenFinishedKey,
				AgentVersionKey,
				HostIdKey,
				PodIDKey,
				HostCreateDetailsKey,
				OverrideDependenciesKey,
				CanResetKey,
			},
		},
	}
	return update
}

// UpdateHeartbeat updates the heartbeat to be the current time
func (t *Task) UpdateHeartbeat() error {
	t.LastHeartbeat = time.Now()
	return UpdateOne(
		bson.M{
			IdKey: t.Id,
		},
		bson.M{
			"$set": bson.M{
				LastHeartbeatKey: t.LastHeartbeat,
			},
		},
	)
}

// GetRecursiveDependenciesUp returns all tasks recursively depended upon
// that are not in the original task slice (this includes earlier tasks in task groups, if applicable).
// depCache should originally be nil. We assume there are no dependency cycles.
func GetRecursiveDependenciesUp(tasks []Task, depCache map[string]Task) ([]Task, error) {
	if depCache == nil {
		depCache = make(map[string]Task)
	}
	for _, t := range tasks {
		depCache[t.Id] = t
	}

	tasksToFind := []string{}
	for _, t := range tasks {
		for _, dep := range t.DependsOn {
			if _, ok := depCache[dep.TaskId]; !ok {
				tasksToFind = append(tasksToFind, dep.TaskId)
			}
		}
		if t.IsPartOfSingleHostTaskGroup() {
			tasksInGroup, err := FindTaskGroupFromBuild(t.BuildId, t.TaskGroup)
			if err != nil {
				return nil, errors.Wrapf(err, "finding task group '%s'", t.TaskGroup)
			}
			for _, taskInGroup := range tasksInGroup {
				if taskInGroup.TaskGroupOrder < t.TaskGroupOrder {
					if _, ok := depCache[taskInGroup.Id]; !ok {
						tasksToFind = append(tasksToFind, taskInGroup.Id)
					}
				}
			}
		}
	}

	// leaf node
	if len(tasksToFind) == 0 {
		return nil, nil
	}

	deps, err := FindWithFields(ByIds(tasksToFind), IdKey, DependsOnKey, ExecutionKey, BuildIdKey, StatusKey, TaskGroupKey, ActivatedKey)
	if err != nil {
		return nil, errors.Wrap(err, "getting dependencies")
	}

	recursiveDeps, err := GetRecursiveDependenciesUp(deps, depCache)
	if err != nil {
		return nil, errors.Wrap(err, "getting recursive dependencies")
	}

	return append(deps, recursiveDeps...), nil
}

// getRecursiveDependenciesDown returns a slice containing all tasks recursively depending on tasks.
// taskMap should originally be nil.
// We assume there are no dependency cycles.
func getRecursiveDependenciesDown(tasks []string, taskMap map[string]bool) ([]Task, error) {
	if taskMap == nil {
		taskMap = make(map[string]bool)
	}
	for _, t := range tasks {
		taskMap[t] = true
	}

	// find the tasks that depend on these tasks
	query := db.Query(bson.M{
		bsonutil.GetDottedKeyName(DependsOnKey, DependencyTaskIdKey): bson.M{"$in": tasks},
	}).WithFields(IdKey, ActivatedKey, DeactivatedForDependencyKey, ExecutionKey, DependsOnKey, BuildIdKey)
	dependOnUsTasks, err := FindAll(query)
	if err != nil {
		return nil, errors.Wrap(err, "can't get dependencies")
	}

	// if the task hasn't yet been visited we need to recurse on it
	newDeps := []Task{}
	for _, t := range dependOnUsTasks {
		if !taskMap[t.Id] {
			newDeps = append(newDeps, t)
		}
	}

	// everything is aleady in the map or nothing depends on tasks
	if len(newDeps) == 0 {
		return nil, nil
	}

	newDepIDs := make([]string, 0, len(newDeps))
	for _, t := range newDeps {
		newDepIDs = append(newDepIDs, t.Id)
	}
	recurseTasks, err := getRecursiveDependenciesDown(newDepIDs, taskMap)
	if err != nil {
		return nil, errors.Wrap(err, "getting recursive dependencies")
	}

	return append(newDeps, recurseTasks...), nil
}

// MarkStart updates the task's start time and sets the status to started
func (t *Task) MarkStart(startTime time.Time) error {
	// record the start time in the in-memory task
	t.StartTime = startTime
	t.Status = evergreen.TaskStarted
	return UpdateOne(
		bson.M{
			IdKey: t.Id,
		},
		bson.M{
			"$set": bson.M{
				StatusKey:        evergreen.TaskStarted,
				LastHeartbeatKey: startTime,
				StartTimeKey:     startTime,
			},
		},
	)
}

// MarkUnscheduled marks the task as undispatched and updates it in the database
func (t *Task) MarkUnscheduled() error {
	t.Status = evergreen.TaskUndispatched
	return UpdateOne(
		bson.M{
			IdKey: t.Id,
		},
		bson.M{
			"$set": bson.M{
				StatusKey: evergreen.TaskUndispatched,
			},
		},
	)

}

// MarkUnattainableDependency updates the unattainable field for the dependency in the task's dependency list,
// and logs if the task is newly blocked.
func (t *Task) MarkUnattainableDependency(dependencyId string, unattainable bool) error {
	wasBlocked := t.Blocked()
	if err := t.updateAllMatchingDependenciesForTask(dependencyId, unattainable); err != nil {
		return errors.Wrapf(err, "updating matching dependencies for task '%s'", t.Id)
	}

	// Only want to log the task as blocked if it wasn't already blocked, and if we're not overriding dependencies.
	if !wasBlocked && unattainable && !t.OverrideDependencies {
		event.LogTaskBlocked(t.Id, t.Execution)
	}
	return nil
}

// AbortBuildTasks sets the abort flag on all tasks associated with the build which are in an abortable
func AbortBuildTasks(buildId string, reason AbortInfo) error {
	q := bson.M{
		BuildIdKey: buildId,
		StatusKey:  bson.M{"$in": evergreen.TaskInProgressStatuses},
	}
	if reason.TaskID != "" {
		q[IdKey] = bson.M{"$ne": reason.TaskID}
	}
	return errors.Wrapf(abortTasksByQuery(q, reason), "aborting tasks for build '%s'", buildId)
}

// AbortVersionTasks sets the abort flag on all tasks associated with the version which are in an
// abortable state
func AbortVersionTasks(versionId string, reason AbortInfo) error {
	q := ByVersionWithChildTasks(versionId)
	q[StatusKey] = bson.M{"$in": evergreen.TaskInProgressStatuses}
	if reason.TaskID != "" {
		q[IdKey] = bson.M{"$ne": reason.TaskID}
		// if the aborting task is part of a display task, we also don't want to mark it as aborted
		q[ExecutionTasksKey] = bson.M{"$ne": reason.TaskID}
	}
	return errors.Wrapf(abortTasksByQuery(q, reason), "aborting tasks for version '%s'", versionId)
}

func abortTasksByQuery(q bson.M, reason AbortInfo) error {
	ids, err := findAllTaskIDs(db.Query(q))
	if err != nil {
		return errors.Wrap(err, "finding updated tasks")
	}
	if len(ids) == 0 {
		return nil
	}
	_, err = UpdateAll(
		ByIds(ids),
		bson.M{"$set": bson.M{
			AbortedKey:   true,
			AbortInfoKey: reason,
		}},
	)
	if err != nil {
		return errors.Wrap(err, "setting aborted statuses")
	}
	event.LogManyTaskAbortRequests(ids, reason.User)
	return nil
}

// String represents the stringified version of a task
func (t *Task) String() (taskStruct string) {
	taskStruct += fmt.Sprintf("Id: %v\n", t.Id)
	taskStruct += fmt.Sprintf("Status: %v\n", t.Status)
	taskStruct += fmt.Sprintf("Host: %v\n", t.HostId)
	taskStruct += fmt.Sprintf("ScheduledTime: %v\n", t.ScheduledTime)
	taskStruct += fmt.Sprintf("ContainerAllocatedTime: %v\n", t.ContainerAllocatedTime)
	taskStruct += fmt.Sprintf("DispatchTime: %v\n", t.DispatchTime)
	taskStruct += fmt.Sprintf("StartTime: %v\n", t.StartTime)
	taskStruct += fmt.Sprintf("FinishTime: %v\n", t.FinishTime)
	taskStruct += fmt.Sprintf("TimeTaken: %v\n", t.TimeTaken)
	taskStruct += fmt.Sprintf("Activated: %v\n", t.Activated)
	taskStruct += fmt.Sprintf("Requester: %v\n", t.Requester)
	taskStruct += fmt.Sprintf("PredictedDuration: %v\n", t.DurationPrediction)

	return
}

// Insert writes the task to the db.
func (t *Task) Insert() error {
	return db.Insert(Collection, t)
}

// Archive modifies the current execution of the task so that it is no longer
// considered the latest execution. This task execution is inserted
// into the old_tasks collection. If this is a display task, its execution tasks
// are also archived.
func (t *Task) Archive() error {
	if !utility.StringSliceContains(evergreen.TaskCompletedStatuses, t.Status) {
		return nil
	}
	if t.DisplayOnly && len(t.ExecutionTasks) > 0 {
		return errors.Wrapf(ArchiveMany([]Task{*t}), "archiving display task '%s'", t.Id)
	} else {
		// Archiving a single task.
		archiveTask := t.makeArchivedTask()
		err := db.Insert(OldCollection, archiveTask)
		if err != nil && !db.IsDuplicateKey(err) {
			return errors.Wrap(err, "inserting archived task into old tasks")
		}
		t.Aborted = false
		err = UpdateOne(
			bson.M{
				IdKey:     t.Id,
				StatusKey: bson.M{"$in": evergreen.TaskCompletedStatuses},
				"$or": []bson.M{
					{
						CanResetKey: bson.M{"$exists": false},
					},
					{
						CanResetKey: false,
					},
				},
			},
			updateDisplayTasksAndTasksExpression,
		)
		// return nil if the task has already been archived
		if adb.ResultsNotFound(err) {
			return nil
		}
		return errors.Wrap(err, "updating task")
	}
}

// ArchiveMany accepts tasks and display tasks (no execution tasks). The function
// expects that each one is going to be archived and progressed to the next execution.
// For execution tasks in display tasks, it will properly account for archiving
// only tasks that should be if failed.
func ArchiveMany(tasks []Task) error {
	allTaskIds := []string{}          // Contains all tasks and display tasks IDs
	execTaskIds := []string{}         // Contains all exec tasks IDs
	toUpdateExecTaskIds := []string{} // Contains all exec tasks IDs that should update and have new execution
	archivedTasks := []interface{}{}  // Contains all archived tasks (task, display, and execution). Created by Task.makeArchivedTask()

	for _, t := range tasks {
		if !utility.StringSliceContains(evergreen.TaskCompletedStatuses, t.Status) {
			continue
		}
		allTaskIds = append(allTaskIds, t.Id)
		archivedTasks = append(archivedTasks, t.makeArchivedTask())
		if t.DisplayOnly && len(t.ExecutionTasks) > 0 {
			var execTasks []Task
			var err error

			if t.IsRestartFailedOnly() {
				execTasks, err = Find(FailedTasksByIds(t.ExecutionTasks))
			} else {
				execTasks, err = FindAll(db.Query(ByIdsAndStatus(t.ExecutionTasks, evergreen.TaskCompletedStatuses)))
			}

			if err != nil {
				return errors.Wrapf(err, "finding execution tasks for display task '%s'", t.Id)
			}
			execTaskIds = append(execTaskIds, t.ExecutionTasks...)
			for _, et := range execTasks {
				if !utility.StringSliceContains(evergreen.TaskCompletedStatuses, et.Status) {
					grip.Debug(message.Fields{
						"message":   "execution task is in incomplete state, skipping archiving",
						"task_id":   et.Id,
						"execution": et.Execution,
						"func":      "ArchiveMany",
					})
					continue
				}
				archivedTasks = append(archivedTasks, et.makeArchivedTask())
				toUpdateExecTaskIds = append(toUpdateExecTaskIds, et.Id)
			}
		}
	}

	grip.DebugWhen(len(utility.UniqueStrings(allTaskIds)) != len(allTaskIds), message.Fields{
		"ticket":           "EVG-17261",
		"message":          "archiving same task multiple times",
		"tasks_to_archive": allTaskIds,
	})

	return archiveAll(allTaskIds, execTaskIds, toUpdateExecTaskIds, archivedTasks)
}

// archiveAll takes in:
// - taskIds                : All tasks and display tasks IDs
// - execTaskIds            : All execution task IDs
// - toRestartExecTaskIds   : All execution task IDs for execution tasks that will be archived/restarted
// - archivedTasks          : All archived tasks created by Task.makeArchivedTask()
func archiveAll(taskIds, execTaskIds, toRestartExecTaskIds []string, archivedTasks []interface{}) error {
	mongoClient := evergreen.GetEnvironment().Client()
	ctx, cancel := evergreen.GetEnvironment().Context()
	defer cancel()
	session, err := mongoClient.StartSession()
	if err != nil {
		return errors.Wrap(err, "starting DB session")
	}
	defer session.EndSession(ctx)

	txFunc := func(sessCtx mongo.SessionContext) (interface{}, error) {
		var err error
		if len(archivedTasks) > 0 {
			oldTaskColl := evergreen.GetEnvironment().DB().Collection(OldCollection)
			_, err = oldTaskColl.InsertMany(sessCtx, archivedTasks)
			if err != nil && !db.IsDuplicateKey(err) {
				return nil, errors.Wrap(err, "archiving tasks")
			}
		}
		if len(taskIds) > 0 {
			_, err = evergreen.GetEnvironment().DB().Collection(Collection).UpdateMany(sessCtx,
				bson.M{
					IdKey:     bson.M{"$in": taskIds},
					StatusKey: bson.M{"$in": evergreen.TaskCompletedStatuses},
					"$or": []bson.M{
						{
							CanResetKey: bson.M{"$exists": false},
						},
						{
							CanResetKey: false,
						},
					},
				},
				updateDisplayTasksAndTasksExpression,
			)
			if err != nil {
				return nil, errors.Wrap(err, "archiving tasks")
			}
		}
		if len(execTaskIds) > 0 {
			_, err = evergreen.GetEnvironment().DB().Collection(Collection).UpdateMany(sessCtx,
				bson.M{IdKey: bson.M{"$in": execTaskIds}}, // Query all execution tasks
				bson.A{ // Pipeline
					bson.M{"$set": bson.M{ // Sets LatestParentExecution (LPE) = LPE + 1
						LatestParentExecutionKey: bson.M{"$add": bson.A{
							"$" + LatestParentExecutionKey, 1,
						}},
					}},
				})

			if err != nil {
				return nil, errors.Wrap(err, "updating latest parent executions")
			}

			// Call to update all tasks that are actually restarting
			_, err = evergreen.GetEnvironment().DB().Collection(Collection).UpdateMany(sessCtx,
				bson.M{IdKey: bson.M{"$in": toRestartExecTaskIds}}, // Query all archiving/restarting execution tasks
				bson.A{ // Pipeline
					bson.M{"$set": bson.M{ // Execution = LPE
						ExecutionKey: "$" + LatestParentExecutionKey,
						CanResetKey:  true,
					}},
					bson.M{"$unset": bson.A{
						AbortedKey,
						AbortInfoKey,
						OverrideDependenciesKey,
					}}})

			return nil, errors.Wrap(err, "updating restarting exec tasks")
		}
		return nil, errors.Wrap(err, "updating tasks")
	}

	_, err = session.WithTransaction(ctx, txFunc)

	return errors.Wrap(err, "archiving execution tasks and updating execution tasks")
}

func (t *Task) makeArchivedTask() *Task {
	archiveTask := *t
	archiveTask.Id = MakeOldID(t.Id, t.Execution)
	archiveTask.OldTaskId = t.Id
	archiveTask.Archived = true

	return &archiveTask
}

// Aggregation

// PopulateTestResults populates the task's LocalTestResults field with any
// test results the task may have. If the results are already populated, this
// function no-ops.
func (t *Task) PopulateTestResults() error {
	if len(t.LocalTestResults) > 0 {
		return nil
	}

	env := evergreen.GetEnvironment()
	ctx, cancel := env.Context()
	defer cancel()

	taskTestResults, err := t.GetTestResults(ctx, env, nil)
	if err != nil {
		return errors.Wrap(err, "populating test results")
	}
	t.LocalTestResults = taskTestResults.Results

	return nil
}

// GetTestResults returns the task's test results filtered, sorted, and
// paginated as specified by the optional filter options.
func (t *Task) GetTestResults(ctx context.Context, env evergreen.Environment, filterOpts *testresult.FilterOptions) (testresult.TaskTestResults, error) {
	taskOpts, err := t.CreateTestResultsTaskOptions()
	if err != nil {
		return testresult.TaskTestResults{}, errors.Wrap(err, "creating test results task options")
	}
	if len(taskOpts) == 0 {
		return testresult.TaskTestResults{}, nil
	}

	return testresult.GetMergedTaskTestResults(ctx, env, taskOpts, filterOpts)
}

// GetTestResultsStats returns basic statistics of the task's test results.
func (t *Task) GetTestResultsStats(ctx context.Context, env evergreen.Environment) (testresult.TaskTestResultsStats, error) {
	taskOpts, err := t.CreateTestResultsTaskOptions()
	if err != nil {
		return testresult.TaskTestResultsStats{}, errors.Wrap(err, "creating test results task options")
	}
	if len(taskOpts) == 0 {
		return testresult.TaskTestResultsStats{}, nil
	}

	return testresult.GetMergedTaskTestResultsStats(ctx, env, taskOpts)
}

// GetTestResultsStats returns a sample of test names (up to 10) that failed in
// the task. If the task does not have any results or does not have any failing
// tests, a nil slice is returned.
func (t *Task) GetFailedTestSample(ctx context.Context, env evergreen.Environment) ([]string, error) {
	taskOpts, err := t.CreateTestResultsTaskOptions()
	if err != nil {
		return nil, errors.Wrap(err, "creating test results task options")
	}
	if len(taskOpts) == 0 {
		return nil, nil
	}

	return testresult.GetMergedFailedTestSample(ctx, env, taskOpts)
}

// CreateTestResultsTaskOptions returns the options required for fetching test
// results for the task.
//
// Calling this function explicitly is typically not necessary. In cases where
// additional tasks are required for fetching test results, such as when
// sorting results by some base status, using this function to populate those
// task options is useful.
func (t *Task) CreateTestResultsTaskOptions() ([]testresult.TaskOptions, error) {
	var taskOpts []testresult.TaskOptions
	if t.DisplayOnly && len(t.ExecutionTasks) > 0 {
		var (
			execTasksWithResults []Task
			err                  error
		)
		hasResults := []bson.M{{ResultsServiceKey: bson.M{"$exists": true}}, {HasCedarResultsKey: true}}
		if t.Archived {
			execTasksWithResults, err = FindByExecutionTasksAndMaxExecution(t.ExecutionTasks, t.Execution, bson.E{Key: "$or", Value: hasResults})
		} else {
			query := ByIds(t.ExecutionTasks)
			query["$or"] = hasResults
			execTasksWithResults, err = FindWithFields(query, ExecutionKey, ResultsServiceKey, HasCedarResultsKey)
		}
		if err != nil {
			return nil, errors.Wrap(err, "getting execution tasks for display task")
		}

		for _, execTask := range execTasksWithResults {
			taskID := execTask.Id
			if execTask.Archived {
				taskID = execTask.OldTaskId
			}
			taskOpts = append(taskOpts, testresult.TaskOptions{
				TaskID:         taskID,
				Execution:      execTask.Execution,
				ResultsService: execTask.ResultsService,
			})
		}
	} else if t.HasResults() {
		taskID := t.Id
		if t.Archived {
			taskID = t.OldTaskId
		}
		taskOpts = append(taskOpts, testresult.TaskOptions{
			TaskID:         taskID,
			Execution:      t.Execution,
			ResultsService: t.ResultsService,
		})
	}

	return taskOpts, nil
}

// SetResetWhenFinished requests that a display task or single-host task group
// reset itself when finished. Will mark itself as system failed.
func (t *Task) SetResetWhenFinished() error {
	if t.ResetWhenFinished {
		return nil
	}
	t.ResetWhenFinished = true
	return UpdateOne(
		bson.M{
			IdKey: t.Id,
		},
		bson.M{
			"$set": bson.M{
				ResetWhenFinishedKey: true,
			},
		},
	)
}

// SetResetFailedWhenFinished requests that a display task
// only restarts failed tasks.
func (t *Task) SetResetFailedWhenFinished() error {
	if t.ResetFailedWhenFinished {
		return nil
	}
	t.ResetFailedWhenFinished = true
	return UpdateOne(
		bson.M{
			IdKey: t.Id,
		},
		bson.M{
			"$set": bson.M{
				ResetFailedWhenFinishedKey: true,
			},
		},
	)
}

// FindHostSchedulable finds all tasks that can be scheduled for a distro
// primary queue.
func FindHostSchedulable(ctx context.Context, distroID string) ([]Task, error) {
	query := schedulableHostTasksQuery()

	if err := addApplicableDistroFilter(ctx, distroID, DistroIdKey, query); err != nil {
		return nil, errors.WithStack(err)
	}

	return Find(query)
}

func addApplicableDistroFilter(ctx context.Context, id string, fieldName string, query bson.M) error {
	if id == "" {
		return nil
	}

	aliases, err := distro.FindApplicableDistroIDs(ctx, id)
	if err != nil {
		return errors.WithStack(err)
	}

	if len(aliases) == 1 {
		query[fieldName] = aliases[0]
	} else {
		query[fieldName] = bson.M{"$in": aliases}
	}

	return nil
}

// FindHostSchedulableForAlias finds all tasks that can be scheduled for a
// distro secondary queue.
func FindHostSchedulableForAlias(ctx context.Context, id string) ([]Task, error) {
	q := schedulableHostTasksQuery()

	if err := addApplicableDistroFilter(ctx, id, SecondaryDistrosKey, q); err != nil {
		return nil, errors.WithStack(err)
	}

	// Single-host task groups can't be put in an alias queue, because it can
	// cause a race when assigning tasks to hosts where the tasks in the task
	// group might be assigned to different hosts.
	q[TaskGroupMaxHostsKey] = bson.M{"$ne": 1}

	return FindAll(db.Query(q))
}

func (t *Task) IsPartOfSingleHostTaskGroup() bool {
	return t.TaskGroup != "" && t.TaskGroupMaxHosts == 1
}

func (t *Task) IsPartOfDisplay() bool {
	// if display task ID is nil, we need to check manually if we have an execution task
	if t.DisplayTaskId == nil {
		dt, err := t.GetDisplayTask()
		if err != nil {
			grip.Error(message.WrapError(err, message.Fields{
				"message":        "unable to get display task",
				"execution_task": t.Id,
			}))
			return false
		}
		return dt != nil
	}

	return utility.FromStringPtr(t.DisplayTaskId) != ""
}

func (t *Task) GetDisplayTask() (*Task, error) {
	if t.DisplayTask != nil {
		return t.DisplayTask, nil
	}
	dtId := utility.FromStringPtr(t.DisplayTaskId)
	if t.DisplayTaskId != nil && dtId == "" {
		// display task ID is explicitly set to empty if it's not a display task
		return nil, nil
	}
	var dt *Task
	var err error
	if t.Archived {
		if dtId != "" {
			dt, err = FindOneOldByIdAndExecution(dtId, t.Execution)
		} else {
			dt, err = FindOneOld(ByExecutionTask(t.OldTaskId))
			if dt != nil {
				dtId = dt.OldTaskId // save the original task ID to cache
			}
		}
	} else {
		if dtId != "" {
			dt, err = FindOneId(dtId)
		} else {
			dt, err = FindOne(db.Query(ByExecutionTask(t.Id)))
			if dt != nil {
				dtId = dt.Id
			}
		}
	}
	if err != nil {
		return nil, err
	}

	if t.DisplayTaskId == nil {
		// Cache display task ID for future use. If we couldn't find the display task,
		// we cache the empty string to show that it doesn't exist.
		grip.Error(message.WrapError(t.SetDisplayTaskID(dtId), message.Fields{
			"message":         "failed to cache display task ID for task",
			"task_id":         t.Id,
			"display_task_id": dtId,
		}))
	}

	t.DisplayTask = dt
	return dt, nil
}

// GetAllDependencies returns all the dependencies the tasks in taskIDs rely on
func GetAllDependencies(taskIDs []string, taskMap map[string]*Task) ([]Dependency, error) {
	// fill in the gaps in taskMap
	tasksToFetch := []string{}
	for _, tID := range taskIDs {
		if _, ok := taskMap[tID]; !ok {
			tasksToFetch = append(tasksToFetch, tID)
		}
	}
	missingTaskMap := make(map[string]*Task)
	if len(tasksToFetch) > 0 {
		missingTasks, err := FindAll(db.Query(ByIds(tasksToFetch)).WithFields(DependsOnKey))
		if err != nil {
			return nil, errors.Wrap(err, "getting tasks missing from map")
		}
		if missingTasks == nil {
			return nil, errors.New("no missing tasks found")
		}
		for i, t := range missingTasks {
			missingTaskMap[t.Id] = &missingTasks[i]
		}
	}

	// extract the set of dependencies
	depSet := make(map[Dependency]bool)
	for _, tID := range taskIDs {
		t, ok := taskMap[tID]
		if !ok {
			t, ok = missingTaskMap[tID]
		}
		if !ok {
			return nil, errors.Errorf("task '%s' does not exist", tID)
		}
		for _, dep := range t.DependsOn {
			depSet[dep] = true
		}
	}

	deps := make([]Dependency, 0, len(depSet))
	for dep := range depSet {
		deps = append(deps, dep)
	}

	return deps, nil
}

func (t *Task) FetchExpectedDuration() util.DurationStats {
	if t.DurationPrediction.TTL == 0 {
		t.DurationPrediction.TTL = utility.JitterInterval(predictionTTL)
	}

	if t.DurationPrediction.Value == 0 && t.ExpectedDuration != 0 {
		// this is probably just backfill, if we have an
		// expected duration, let's assume it was collected
		// before now slightly.
		t.DurationPrediction.Value = t.ExpectedDuration
		t.DurationPrediction.CollectedAt = time.Now().Add(-time.Minute)

		if err := t.cacheExpectedDuration(); err != nil {
			grip.Error(message.WrapError(err, message.Fields{
				"task":    t.Id,
				"message": "caching expected duration",
			}))
		}

		return util.DurationStats{Average: t.ExpectedDuration, StdDev: t.ExpectedDurationStdDev}
	}

	refresher := func(previous util.DurationStats) (util.DurationStats, bool) {
		defaultVal := util.DurationStats{Average: defaultTaskDuration, StdDev: 0}
		vals, err := getExpectedDurationsForWindow(t.DisplayName, t.Project, t.BuildVariant,
			time.Now().Add(-taskCompletionEstimateWindow), time.Now())
		grip.Notice(message.WrapError(err, message.Fields{
			"name":      t.DisplayName,
			"id":        t.Id,
			"project":   t.Project,
			"variant":   t.BuildVariant,
			"operation": "fetching expected duration, expect stale scheduling data",
		}))
		if err != nil {
			return defaultVal, false
		}

		if len(vals) != 1 {
			if previous.Average == 0 {
				return defaultVal, true
			}

			return previous, true
		}

		avg := time.Duration(vals[0].ExpectedDuration)
		if avg == 0 {
			return defaultVal, true
		}
		stdDev := time.Duration(vals[0].StdDev)
		return util.DurationStats{Average: avg, StdDev: stdDev}, true
	}

	grip.Error(message.WrapError(t.DurationPrediction.SetRefresher(refresher), message.Fields{
		"message": "problem setting cached value refresher",
		"cause":   "programmer error",
	}))

	stats, ok := t.DurationPrediction.Get()
	if ok {
		if err := t.cacheExpectedDuration(); err != nil {
			grip.Error(message.WrapError(err, message.Fields{
				"task":    t.Id,
				"message": "caching expected duration",
			}))
		}
	}
	t.ExpectedDuration = stats.Average
	t.ExpectedDurationStdDev = stats.StdDev

	return stats
}

// TaskStatusCount holds counts for task statuses
type TaskStatusCount struct {
	Succeeded    int `json:"succeeded"`
	Failed       int `json:"failed"`
	Started      int `json:"started"`
	Undispatched int `json:"undispatched"`
	Inactive     int `json:"inactive"`
	Dispatched   int `json:"dispatched"`
	TimedOut     int `json:"timed_out"`
}

func (tsc *TaskStatusCount) IncrementStatus(status string, statusDetails apimodels.TaskEndDetail) {
	switch status {
	case evergreen.TaskSucceeded:
		tsc.Succeeded++
	case evergreen.TaskFailed, evergreen.TaskSetupFailed:
		if statusDetails.TimedOut && statusDetails.Description == evergreen.TaskDescriptionHeartbeat {
			tsc.TimedOut++
		} else {
			tsc.Failed++
		}
	case evergreen.TaskStarted, evergreen.TaskDispatched:
		tsc.Started++
	case evergreen.TaskUndispatched:
		tsc.Undispatched++
	case evergreen.TaskInactive:
		tsc.Inactive++
	}
}

const jqlBFQuery = "(project in (%v)) and ( %v ) order by updatedDate desc"

// Generates a jira JQL string from the task
// When we search in jira for a task we search in the specified JIRA project
// If there are any test results, then we only search by test file
// name of all of the failed tests.
// Otherwise we search by the task name.
func (t *Task) GetJQL(searchProjects []string) string {
	var jqlParts []string
	var jqlClause string
	for _, testResult := range t.LocalTestResults {
		if testResult.Status == evergreen.TestFailedStatus {
			fileParts := eitherSlash.Split(testResult.TestName, -1)
			jqlParts = append(jqlParts, fmt.Sprintf("text~\"%v\"", util.EscapeJQLReservedChars(fileParts[len(fileParts)-1])))
		}
	}
	if jqlParts != nil {
		jqlClause = strings.Join(jqlParts, " or ")
	} else {
		jqlClause = fmt.Sprintf("text~\"%v\"", util.EscapeJQLReservedChars(t.DisplayName))
	}

	return fmt.Sprintf(jqlBFQuery, strings.Join(searchProjects, ", "), jqlClause)
}

// Blocked returns if a task cannot run given the state of the task
func (t *Task) Blocked() bool {
	if t.OverrideDependencies {
		return false
	}

	for _, dependency := range t.DependsOn {
		if dependency.Unattainable {
			return true
		}
	}
	return false
}

// WillRun returns true if the task will run eventually, but has not started
// running yet. This is logically equivalent to evergreen.TaskWillRun from
// (Task).GetDisplayStatus.
func (t *Task) WillRun() bool {
	return t.Status == evergreen.TaskUndispatched && t.Activated && !t.Blocked()
}

// IsUnscheduled returns true if a task is unscheduled and will not run. This is
// logically equivalent to evergreen.TaskUnscheduled from
// (Task).GetDisplayStatus.
func (t *Task) IsUnscheduled() bool {
	return t.Status == evergreen.TaskUndispatched && !t.Activated
}

// IsInProgress returns true if the task has been dispatched and is about to
// run, or is already running.
func (t *Task) IsInProgress() bool {
	return utility.StringSliceContains(evergreen.TaskInProgressStatuses, t.Status)
}

func (t *Task) BlockedState(dependencies map[string]*Task) (string, error) {
	if t.Blocked() {
		return evergreen.TaskStatusBlocked, nil
	}

	for _, dep := range t.DependsOn {
		depTask, ok := dependencies[dep.TaskId]
		if !ok {
			continue
		}
		if !t.SatisfiesDependency(depTask) {
			return evergreen.TaskStatusPending, nil
		}
	}

	return "", nil
}

// CircularDependencies detects if any tasks in this version are part of a dependency cycle
// Note that it does not check inter-version dependencies, because only evergreen can add those
func (t *Task) CircularDependencies() error {
	var err error
	tasksWithDeps, err := FindAllTasksFromVersionWithDependencies(t.Version)
	if err != nil {
		return errors.Wrap(err, "finding tasks with dependencies")
	}
	if len(tasksWithDeps) == 0 {
		return nil
	}
	dependencyMap := map[string][]string{}
	for _, versionTask := range tasksWithDeps {
		for _, dependency := range versionTask.DependsOn {
			dependencyMap[versionTask.Id] = append(dependencyMap[versionTask.Id], dependency.TaskId)
		}
	}
	catcher := grip.NewBasicCatcher()
	cycles := tarjan.Connections(dependencyMap)
	for _, cycle := range cycles {
		if len(cycle) > 1 {
			catcher.Errorf("dependency cycle detected: %s", strings.Join(cycle, ","))
		}
	}
	return catcher.Resolve()
}

func (t *Task) ToTaskNode() TaskNode {
	return TaskNode{
		Name:    t.DisplayName,
		Variant: t.BuildVariant,
		ID:      t.Id,
	}
}

func AnyActiveTasks(tasks []Task) bool {
	for _, t := range tasks {
		if t.Activated {
			return true
		}
	}
	return false
}

func TaskSliceToMap(tasks []Task) map[string]Task {
	taskMap := make(map[string]Task, len(tasks))
	for _, t := range tasks {
		taskMap[t.Id] = t
	}

	return taskMap
}

func GetLatestExecution(taskId string) (int, error) {
	var t *Task
	var err error
	t, err = FindOneId(taskId)
	if err != nil {
		return -1, err
	}
	if t == nil {
		pieces := strings.Split(taskId, "_")
		pieces = pieces[:len(pieces)-1]
		taskId = strings.Join(pieces, "_")
		t, err = FindOneId(taskId)
		if err != nil {
			return -1, errors.Wrap(err, "getting task")
		}
	}
	if t == nil {
		return -1, errors.Errorf("task '%s' not found", taskId)
	}
	return t.Execution, nil
}

// GetTimeSpent returns the total time_taken and makespan of tasks
func GetTimeSpent(tasks []Task) (time.Duration, time.Duration) {
	var timeTaken time.Duration
	earliestStartTime := utility.MaxTime
	latestFinishTime := utility.ZeroTime
	for _, t := range tasks {
		if t.DisplayOnly {
			continue
		}
		timeTaken += t.TimeTaken
		if !utility.IsZeroTime(t.StartTime) && t.StartTime.Before(earliestStartTime) {
			earliestStartTime = t.StartTime
		}
		if t.FinishTime.After(latestFinishTime) {
			latestFinishTime = t.FinishTime
		}
	}

	if earliestStartTime == utility.MaxTime || latestFinishTime == utility.ZeroTime {
		return 0, 0
	}

	return timeTaken, latestFinishTime.Sub(earliestStartTime)
}

// GetFormattedTimeSpent returns the total time_taken and makespan of tasks as a formatted string
func GetFormattedTimeSpent(tasks []Task) (string, string) {
	timeTaken, makespan := GetTimeSpent(tasks)

	t := timeTaken.Round(time.Second).String()
	m := makespan.Round(time.Second).String()

	return formatDuration(t), formatDuration(m)
}

func formatDuration(duration string) string {
	regex := regexp.MustCompile(`\d*[dhms]`)
	return strings.TrimSpace(regex.ReplaceAllStringFunc(duration, func(m string) string {
		return m + " "
	}))
}

type TasksSortOrder struct {
	Key   string
	Order int
}

type GetTasksByProjectAndCommitOptions struct {
	Project        string
	CommitHash     string
	StartingTaskId string
	Status         string
	VariantName    string
	VariantRegex   string
	TaskName       string
	Limit          int
}

func AddParentDisplayTasks(tasks []Task) ([]Task, error) {
	if len(tasks) == 0 {
		return tasks, nil
	}
	taskIDs := []string{}
	tasksCopy := tasks
	for _, t := range tasks {
		taskIDs = append(taskIDs, t.Id)
	}
	parents, err := FindAll(db.Query(ByExecutionTasks(taskIDs)))
	if err != nil {
		return nil, errors.Wrap(err, "finding parent display tasks")
	}
	childrenToParents := map[string]*Task{}
	for i, dt := range parents {
		for _, et := range dt.ExecutionTasks {
			childrenToParents[et] = &parents[i]
		}
	}
	for i, t := range tasksCopy {
		if childrenToParents[t.Id] != nil {
			t.DisplayTask = childrenToParents[t.Id]
			tasksCopy[i] = t
		}
	}
	return tasksCopy, nil
}

// UpdateDependsOn appends new dependencies to tasks that already depend on this task
// if the task does not explicitly omit having generated tasks as dependencies
func (t *Task) UpdateDependsOn(status string, newDependencyIDs []string) error {
	newDependencies := make([]Dependency, 0, len(newDependencyIDs))
	for _, depID := range newDependencyIDs {
		if depID == t.Id {
			grip.Error(message.Fields{
				"message": "task is attempting to add a dependency on itself, skipping this dependency",
				"task_id": t.Id,
				"stack":   string(debug.Stack()),
			})
			continue
		}

		newDependencies = append(newDependencies, Dependency{
			TaskId: depID,
			Status: status,
		})
	}

	_, err := UpdateAll(
		bson.M{
			DependsOnKey: bson.M{"$elemMatch": bson.M{
				DependencyTaskIdKey:             t.Id,
				DependencyStatusKey:             status,
				DependencyOmitGeneratedTasksKey: bson.M{"$ne": true},
			}},
		},
		bson.M{"$push": bson.M{DependsOnKey: bson.M{"$each": newDependencies}}},
	)

	return errors.Wrap(err, "updating dependencies")
}

func (t *Task) SetTaskGroupInfo() error {
	return errors.WithStack(UpdateOne(bson.M{IdKey: t.Id},
		bson.M{"$set": bson.M{
			TaskGroupOrderKey:    t.TaskGroupOrder,
			TaskGroupMaxHostsKey: t.TaskGroupMaxHosts,
		}}))
}

func (t *Task) SetDisplayTaskID(id string) error {
	t.DisplayTaskId = utility.ToStringPtr(id)
	return errors.WithStack(UpdateOne(bson.M{IdKey: t.Id},
		bson.M{"$set": bson.M{
			DisplayTaskIdKey: id,
		}}))
}

func (t *Task) SetNumDependents() error {
	update := bson.M{
		"$set": bson.M{
			NumDepsKey: t.NumDependents,
		},
	}
	if t.NumDependents == 0 {
		update = bson.M{"$unset": bson.M{
			NumDepsKey: "",
		}}
	}
	return UpdateOne(bson.M{
		IdKey: t.Id,
	}, update)
}

func AddDisplayTaskIdToExecTasks(displayTaskId string, execTasksToUpdate []string) error {
	if len(execTasksToUpdate) == 0 {
		return nil
	}
	_, err := UpdateAll(bson.M{
		IdKey: bson.M{"$in": execTasksToUpdate},
	},
		bson.M{"$set": bson.M{
			DisplayTaskIdKey: displayTaskId,
		}},
	)
	return err
}

func AddExecTasksToDisplayTask(displayTaskId string, execTasks []string, displayTaskActivated bool) error {
	if len(execTasks) == 0 {
		return nil
	}
	update := bson.M{"$addToSet": bson.M{
		ExecutionTasksKey: bson.M{"$each": execTasks},
	}}

	if displayTaskActivated {
		// verify that the display task isn't already activated
		dt, err := FindOneId(displayTaskId)
		if err != nil {
			return errors.Wrap(err, "getting display task")
		}
		if dt == nil {
			return errors.Errorf("display task not found")
		}
		if !dt.Activated {
			update["$set"] = bson.M{
				ActivatedKey:     true,
				ActivatedTimeKey: time.Now(),
			}
		}
	}

	return UpdateOne(
		bson.M{IdKey: displayTaskId},
		update,
	)
}

// in the process of aborting and will eventually reset themselves.
func (t *Task) FindAbortingAndResettingDependencies() ([]Task, error) {
	recursiveDeps, err := GetRecursiveDependenciesUp([]Task{*t}, map[string]Task{})
	if err != nil {
		return nil, errors.Wrap(err, "getting recursive parent dependencies")
	}
	var taskIDs []string
	for _, dep := range recursiveDeps {
		taskIDs = append(taskIDs, dep.Id)
	}
	if len(taskIDs) == 0 {
		return nil, nil
	}

	// GetRecursiveDependenciesUp only populates a subset of the task's
	// in-memory fields, so query for them again with the necessary keys.
	q := db.Query(bson.M{
		IdKey:      bson.M{"$in": taskIDs},
		AbortedKey: true,
		"$or": []bson.M{
			{ResetWhenFinishedKey: true},
			{ResetFailedWhenFinishedKey: true},
		},
	})
	return FindAll(q)
}
