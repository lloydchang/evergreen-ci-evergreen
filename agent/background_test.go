package agent

import (
	"context"
	"testing"
	"time"

	"github.com/evergreen-ci/evergreen/agent/command"
	"github.com/evergreen-ci/evergreen/agent/internal"
	"github.com/evergreen-ci/evergreen/agent/internal/client"
	"github.com/evergreen-ci/evergreen/model"
	"github.com/mongodb/grip/send"
	"github.com/mongodb/jasper"
	"github.com/stretchr/testify/suite"
	"go.opentelemetry.io/otel"
)

type BackgroundSuite struct {
	suite.Suite
	a                *Agent
	mockCommunicator *client.Mock
	tc               *taskContext
	sender           *send.InternalSender
}

func TestBackgroundSuite(t *testing.T) {
	suite.Run(t, new(BackgroundSuite))
}

func (s *BackgroundSuite) SetupTest() {
	var err error
	s.a = &Agent{
		opts: Options{
			HostID:     "host",
			HostSecret: "secret",
			StatusPort: 2286,
			LogOutput:  LogOutputStdout,
			LogPrefix:  "agent",
		},
		comm:   client.NewMock("url"),
		tracer: otel.GetTracerProvider().Tracer("noop_tracer"),
	}
	s.a.jasper, err = jasper.NewSynchronizedManager(true)
	s.Require().NoError(err)
	s.mockCommunicator = s.a.comm.(*client.Mock)

	s.tc = &taskContext{}
	s.tc.taskConfig = &internal.TaskConfig{}
	s.tc.taskConfig.Project = &model.Project{}
	s.tc.taskConfig.Project.CallbackTimeout = 0
	s.sender = send.MakeInternalLogger()
	s.tc.logger = client.NewSingleChannelLogHarness("test", s.sender)
}

func (s *BackgroundSuite) TestWithCallbackTimeoutDefault() {
	ctx, _ := s.a.withCallbackTimeout(context.Background(), s.tc)
	deadline, ok := ctx.Deadline()
	s.True(deadline.Sub(time.Now()) > (defaultCallbackCmdTimeout - time.Second)) // nolint
	s.True(ok)
}

func (s *BackgroundSuite) TestWithCallbackTimeoutSetByProject() {
	s.tc.taskConfig.Project.CallbackTimeout = 100
	ctx, _ := s.a.withCallbackTimeout(context.Background(), s.tc)
	deadline, ok := ctx.Deadline()
	s.True(deadline.Sub(time.Now()) > 99) // nolint
	s.True(ok)
}

const (
	defaultAbortCheckInterval = 100 * time.Millisecond
	defaultNumAbortChecks     = 3
)

func (s *BackgroundSuite) TestAbortedTaskStillHeartbeats() {
	s.mockCommunicator.HeartbeatShouldAbort = true
	s.a.opts.HeartbeatInterval = time.Millisecond

	heartbeatCtx, heartbeatCancel := context.WithTimeout(context.Background(), time.Second)
	defer heartbeatCancel()

	childCtx, childCancel := context.WithCancel(context.Background())

	go s.a.startHeartbeat(heartbeatCtx, childCancel, s.tc)

	lastHeartbeatCount := 0
	s.checkHeartbeatCondition(heartbeatCheckOptions{
		heartbeatCtx:      heartbeatCtx,
		checkInterval:     defaultAbortCheckInterval,
		numRequiredChecks: defaultNumAbortChecks,
		checkCondition: func() bool {
			if childCtx.Err() == nil {
				// If the child context has not errored, the heartbeat has not
				// yet signaled for the task to abort.
				return false
			}

			// This is checking that the task was signaled to abort (via context
			// cancellation) due to getting an explicit abort message.
			// Furthermore, even though the task is aborting, the heartbeat
			// should continue running.
			currentHeartbeatCount := s.mockCommunicator.GetHeartbeatCount()
			s.Greater(currentHeartbeatCount, lastHeartbeatCount, "heartbeat should still be running")
			lastHeartbeatCount = currentHeartbeatCount

			return true
		},
		exitCondition: func() {
			s.FailNow("heartbeat exited before it could finish checks")
		},
	})
}

func (s *BackgroundSuite) TestHeartbeatSignalsAbortOnTaskConflict() {
	s.mockCommunicator.HeartbeatShouldConflict = true
	s.a.opts.HeartbeatInterval = time.Millisecond

	heartbeatCtx, heartbeatCancel := context.WithTimeout(context.Background(), time.Second)
	defer heartbeatCancel()

	childCtx, childCancel := context.WithCancel(context.Background())

	go s.a.startHeartbeat(heartbeatCtx, childCancel, s.tc)

	lastHeartbeatCount := 0
	s.checkHeartbeatCondition(heartbeatCheckOptions{
		heartbeatCtx:      heartbeatCtx,
		checkInterval:     defaultAbortCheckInterval,
		numRequiredChecks: defaultNumAbortChecks,
		checkCondition: func() bool {
			if childCtx.Err() == nil {
				// If the child context has not errored, the heartbeat has not
				// yet signaled for the task to abort.
				return false
			}

			// This is checking that the task was signaled to abort (via context
			// cancellation) due to getting a task conflict (i.e. abort and
			// restart task). Furthermore, even though the task is aborting, the
			// heartbeat should continue running.
			currentHeartbeatCount := s.mockCommunicator.GetHeartbeatCount()
			s.Greater(currentHeartbeatCount, lastHeartbeatCount, "heartbeat should still be running")
			lastHeartbeatCount = currentHeartbeatCount

			return true
		},
		exitCondition: func() {
			s.FailNow("heartbeat exited before it could finish checks")
		},
	})
}

func (s *BackgroundSuite) TestHeartbeatSignalsAbortOnHittingMaxFailedHeartbeats() {
	s.mockCommunicator.HeartbeatShouldErr = true
	s.a.opts.HeartbeatInterval = time.Millisecond

	heartbeatCtx, heartbeatCancel := context.WithTimeout(context.Background(), time.Second)
	defer heartbeatCancel()

	childCtx, childCancel := context.WithCancel(context.Background())

	go s.a.startHeartbeat(heartbeatCtx, childCancel, s.tc)

	lastHeartbeatCount := 0
	s.checkHeartbeatCondition(heartbeatCheckOptions{
		heartbeatCtx:      heartbeatCtx,
		checkInterval:     defaultAbortCheckInterval,
		numRequiredChecks: defaultNumAbortChecks,
		checkCondition: func() bool {
			if childCtx.Err() == nil {
				// If the child context has not errored, the heartbeat has not
				// yet signaled for the task to abort.
				return false
			}

			// This is checking that the task was signaled to abort (via context
			// cancellation) due to consistently failing to heartbeat.
			// Furthermore, even though the task is aborting, the heartbeat
			// should continue running.
			currentHeartbeatCount := s.mockCommunicator.GetHeartbeatCount()
			s.Greater(currentHeartbeatCount, lastHeartbeatCount, "heartbeat should still be running")
			lastHeartbeatCount = currentHeartbeatCount

			return true
		},
		exitCondition: func() {
			s.FailNow("heartbeat exited before it could finish checks")
		},
	})
}

func (s *BackgroundSuite) TestHeartbeatSignalsAbortWhenHeartbeatStops() {
	s.a.opts.HeartbeatInterval = time.Millisecond

	heartbeatCtx, heartbeatCancel := context.WithTimeout(context.Background(), time.Second)
	defer heartbeatCancel()

	childCtx, childCancel := context.WithCancel(context.Background())

	go s.a.startHeartbeat(heartbeatCtx, childCancel, s.tc)

	s.checkHeartbeatCondition(heartbeatCheckOptions{
		heartbeatCtx:      heartbeatCtx,
		checkInterval:     defaultAbortCheckInterval,
		numRequiredChecks: defaultNumAbortChecks,
		checkCondition: func() bool {
			// This is checking that the task does not abort. There should be no
			// reason for the task to abort until the heartbeat exits.
			s.NoError(childCtx.Err())
			return true
		},
		exitCondition: func() {
			// Check that once the heartbeat exits, the task is signaled to
			// abort (if it is still running) in a timely manner.
			checkChildCtx, checkChildCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer checkChildCancel()
			select {
			case <-childCtx.Done():
			case <-checkChildCtx.Done():
				s.FailNow("child context should be done in a timely manner")
			}
		},
	})
}

func (s *BackgroundSuite) TestHeartbeatSometimesFailsDoesNotFailTask() {
	s.mockCommunicator.HeartbeatShouldSometimesErr = true
	s.a.opts.HeartbeatInterval = time.Millisecond

	heartbeatCtx, heartbeatCancel := context.WithTimeout(context.Background(), time.Second)
	defer heartbeatCancel()

	childCtx, childCancel := context.WithCancel(context.Background())

	go s.a.startHeartbeat(heartbeatCtx, childCancel, s.tc)

	s.checkHeartbeatCondition(heartbeatCheckOptions{
		heartbeatCtx:      heartbeatCtx,
		checkInterval:     defaultAbortCheckInterval,
		numRequiredChecks: defaultNumAbortChecks,
		checkCondition: func() bool {
			// This is checking that, even though the heartbeat is sporadically
			// failing, as long as it's succeeding sometimes, the task does not
			// abort. There should be no reason for the task to abort until the
			// heartbeat exits.
			s.NoError(childCtx.Err())
			return true
		},
		exitCondition: func() {
			// Check that once the heartbeat exits, the task is signaled to
			// abort (if it is still running) in a timely manner.
			checkChildCtx, checkChildCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer checkChildCancel()
			select {
			case <-childCtx.Done():
			case <-checkChildCtx.Done():
				s.FailNow("child context should be done in a timely manner")
			}
		},
	})
}

func (s *BackgroundSuite) TestGetCurrentTimeout() {
	s.tc.taskConfig.Timeout = &internal.Timeout{}
	cmdFactory, exists := command.GetCommandFactory("shell.exec")
	s.True(exists)
	cmd := cmdFactory()
	cmd.SetIdleTimeout(time.Second)
	s.tc.setCurrentCommand(cmd)
	s.tc.setCurrentIdleTimeout(cmd)
	s.Equal(time.Second, s.tc.getCurrentTimeout())
}

func (s *BackgroundSuite) TestGetTimeoutDefault() {
	s.Equal(defaultIdleTimeout, s.tc.getCurrentTimeout())
}

type heartbeatCheckOptions struct {
	heartbeatCtx context.Context

	checkInterval     time.Duration
	numRequiredChecks int
	checkCondition    func() bool

	exitCondition func()
}

// checkHeartbeatCondition periodically checks a condition until the heartbeat
// exits. When the timer fires, it will check the abort condition, which can be
// used to check the current heartbeat/abort state. When the heartbeat exits, it
// will check the exit condition.
func (s *BackgroundSuite) checkHeartbeatCondition(abortCheck heartbeatCheckOptions) {
	timer := time.NewTimer(abortCheck.checkInterval)
	defer timer.Stop()

	numChecksPassed := 0
	for {
		select {
		case <-timer.C:
			timer.Reset(abortCheck.checkInterval)
			if checkPassed := abortCheck.checkCondition(); !checkPassed {
				continue
			}

			numChecksPassed++
			if numChecksPassed >= abortCheck.numRequiredChecks {
				return
			}
		case <-abortCheck.heartbeatCtx.Done():
			abortCheck.exitCondition()
		}
	}
}
