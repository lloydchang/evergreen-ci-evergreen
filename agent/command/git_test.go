package command

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/evergreen-ci/evergreen"
	"github.com/evergreen-ci/evergreen/agent/internal"
	"github.com/evergreen-ci/evergreen/agent/internal/client"
	agentutil "github.com/evergreen-ci/evergreen/agent/internal/testutil"
	"github.com/evergreen-ci/evergreen/db"
	"github.com/evergreen-ci/evergreen/model"
	"github.com/evergreen-ci/evergreen/model/build"
	"github.com/evergreen-ci/evergreen/model/host"
	"github.com/evergreen-ci/evergreen/model/patch"
	"github.com/evergreen-ci/evergreen/model/task"
	modelutil "github.com/evergreen-ci/evergreen/model/testutil"
	"github.com/evergreen-ci/evergreen/testutil"
	"github.com/evergreen-ci/evergreen/thirdparty"
	"github.com/evergreen-ci/evergreen/util"
	"github.com/evergreen-ci/utility"
	"github.com/mongodb/grip/level"
	"github.com/mongodb/grip/send"
	"github.com/mongodb/jasper"
	"github.com/pkg/errors"
	"github.com/smartystreets/goconvey/convey/reporting"
	"github.com/stretchr/testify/suite"
)

const (
	globalGitHubToken  = "GLOBALTOKEN"
	projectGitHubToken = "PROJECTTOKEN"
	githubAppToken     = "APPTOKEN"
)

type GitGetProjectSuite struct {
	settings    *evergreen.Settings
	modelData1  *modelutil.TestModelData // test model for TestGitPlugin
	taskConfig1 *internal.TaskConfig
	modelData2  *modelutil.TestModelData // test model for TestValidateGitCommands
	taskConfig2 *internal.TaskConfig
	modelData3  *modelutil.TestModelData
	taskConfig3 *internal.TaskConfig
	modelData4  *modelutil.TestModelData
	taskConfig4 *internal.TaskConfig
	modelData5  *modelutil.TestModelData
	taskConfig5 *internal.TaskConfig
	modelData6  *modelutil.TestModelData
	taskConfig6 *internal.TaskConfig     // used for TestMergeMultiplePatches
	modelData7  *modelutil.TestModelData // GitHub merge queue
	taskConfig7 *internal.TaskConfig     // GitHub merge queue

	comm   *client.Mock
	jasper jasper.Manager
	ctx    context.Context
	cancel context.CancelFunc
	suite.Suite
}

func init() {
	reporting.QuietMode()
}

func TestGitGetProjectSuite(t *testing.T) {
	s := new(GitGetProjectSuite)
	suite.Run(t, s)
}

func (s *GitGetProjectSuite) SetupSuite() {
	var err error
	s.jasper, err = jasper.NewSynchronizedManager(false)
	s.Require().NoError(err)

	s.comm = client.NewMock("http://localhost.com")

	s.ctx, s.cancel = context.WithCancel(context.Background())
	env := testutil.NewEnvironment(s.ctx, s.T())
	settings := env.Settings()

	testutil.ConfigureIntegrationTest(s.T(), settings, s.T().Name())
	s.settings = settings
}

func (s *GitGetProjectSuite) SetupTest() {
	s.NoError(db.ClearCollections(patch.Collection, build.Collection, task.Collection,
		model.VersionCollection, host.Collection, model.TaskLogCollection))
	var err error

	configPath1 := filepath.Join(testutil.GetDirectoryOfFile(), "testdata", "git", "plugin_clone.yml")
	configPath2 := filepath.Join(testutil.GetDirectoryOfFile(), "testdata", "git", "test_config.yml")
	configPath3 := filepath.Join(testutil.GetDirectoryOfFile(), "testdata", "git", "no_token.yml")
	configPath4 := filepath.Join(testutil.GetDirectoryOfFile(), "testdata", "git", "additional_patch.yml")
	patchPath := filepath.Join(testutil.GetDirectoryOfFile(), "testdata", "git", "test.patch")

	s.modelData1, err = modelutil.SetupAPITestData(s.settings, "testtask1", "rhel55", configPath1, modelutil.NoPatch)
	s.Require().NoError(err)
	s.taskConfig1, err = agentutil.MakeTaskConfigFromModelData(s.ctx, s.settings, s.modelData1)
	s.Require().NoError(err)
	s.taskConfig1.Expansions = util.NewExpansions(map[string]string{evergreen.GlobalGitHubTokenExpansion: fmt.Sprintf("token " + globalGitHubToken)})
	s.Require().NoError(err)

	s.modelData2, err = modelutil.SetupAPITestData(s.settings, "testtask1", "rhel55", configPath2, modelutil.NoPatch)
	s.Require().NoError(err)
	s.taskConfig2, err = agentutil.MakeTaskConfigFromModelData(s.ctx, s.settings, s.modelData2)
	s.Require().NoError(err)
	s.taskConfig2.Expansions = util.NewExpansions(s.settings.Credentials)
	s.taskConfig2.Expansions.Put("prefixpath", "hello")
	// SetupAPITestData always creates BuildVariant with no modules so this line works around that
	s.taskConfig2.BuildVariant.Modules = []string{"sample"}
	s.modelData2.Task.Requester = evergreen.PatchVersionRequester
	err = setupTestPatchData(s.modelData1, patchPath, s.T())
	s.Require().NoError(err)

	s.modelData3, err = modelutil.SetupAPITestData(s.settings, "testtask1", "rhel55", configPath2, modelutil.NoPatch)
	s.Require().NoError(err)
	s.taskConfig3, err = agentutil.MakeTaskConfigFromModelData(s.ctx, s.settings, s.modelData3)
	s.Require().NoError(err)
	s.taskConfig3.Expansions = util.NewExpansions(s.settings.Credentials)
	s.taskConfig3.GithubPatchData = thirdparty.GithubPatch{
		PRNumber:   9001,
		BaseOwner:  "evergreen-ci",
		BaseRepo:   "evergreen",
		BaseBranch: "main",
		HeadOwner:  "octocat",
		HeadRepo:   "evergreen",
		HeadHash:   "55ca6286e3e4f4fba5d0448333fa99fc5a404a73",
		Author:     "octocat",
	}
	s.taskConfig3.Task.Requester = evergreen.GithubPRRequester

	s.modelData4, err = modelutil.SetupAPITestData(s.settings, "testtask1", "rhel55", configPath2, modelutil.MergePatch)
	s.Require().NoError(err)
	s.taskConfig4, err = agentutil.MakeTaskConfigFromModelData(s.ctx, s.settings, s.modelData4)
	s.Require().NoError(err)
	s.taskConfig4.Expansions = util.NewExpansions(s.settings.Credentials)
	s.taskConfig4.GithubPatchData = thirdparty.GithubPatch{
		PRNumber:       9001,
		MergeCommitSHA: "abcdef",
	}
	s.modelData5, err = modelutil.SetupAPITestData(s.settings, "testtask1", "rhel55", configPath3, modelutil.MergePatch)
	s.Require().NoError(err)
	s.taskConfig5, err = agentutil.MakeTaskConfigFromModelData(s.ctx, s.settings, s.modelData5)
	s.Require().NoError(err)

	s.modelData6, err = modelutil.SetupAPITestData(s.settings, "testtask1", "linux-64", configPath4, modelutil.InlinePatch)
	s.Require().NoError(err)
	s.modelData6.Task.Requester = evergreen.MergeTestRequester
	s.Require().NoError(err)
	s.taskConfig6, err = agentutil.MakeTaskConfigFromModelData(s.ctx, s.settings, s.modelData6)
	s.Require().NoError(err)
	s.taskConfig6.Expansions = util.NewExpansions(map[string]string{evergreen.GlobalGitHubTokenExpansion: fmt.Sprintf("token " + globalGitHubToken)})
	s.taskConfig6.BuildVariant.Modules = []string{"evergreen"}

	s.modelData7, err = modelutil.SetupAPITestData(s.settings, "testtask1", "linux-64", configPath3, modelutil.InlinePatch)
	s.Require().NoError(err)
	s.taskConfig7, err = agentutil.MakeTaskConfigFromModelData(s.ctx, s.settings, s.modelData7)
	s.Require().NoError(err)
	s.taskConfig7.Expansions = util.NewExpansions(map[string]string{evergreen.GlobalGitHubTokenExpansion: fmt.Sprintf("token " + globalGitHubToken)})
	s.taskConfig7.BuildVariant.Modules = []string{"evergreen"}
	s.taskConfig7.GithubMergeData = thirdparty.GithubMergeGroup{
		HeadBranch: "gh-readonly-queue/main/pr-515-9cd8a2532bcddf58369aa82eb66ba88e2323c056",
		HeadSHA:    "d2a90288ad96adca4a7d0122d8d4fd1deb24db11",
	}
	s.taskConfig7.Task.Requester = evergreen.GithubMergeRequester
}

func (s *GitGetProjectSuite) TestBuildCloneCommandUsesHTTPS() {
	c := &gitFetchProject{
		Directory: "dir",
		Token:     projectGitHubToken,
	}
	conf := s.taskConfig1
	logger, err := s.comm.GetLoggerProducer(s.ctx, client.TaskData{ID: conf.Task.Id, Secret: conf.Task.Secret}, nil)
	s.NoError(err)
	opts := cloneOpts{
		method: evergreen.CloneMethodOAuth,
		owner:  conf.ProjectRef.Owner,
		repo:   conf.ProjectRef.Repo,
		branch: conf.ProjectRef.Branch,
		dir:    c.Directory,
		token:  c.Token,
	}
	s.Require().NoError(opts.setLocation())
	cmds, _ := c.buildCloneCommand(s.ctx, s.comm, logger, conf, opts)
	s.Equal("git clone https://PROJECTTOKEN:x-oauth-basic@github.com/deafgoat/mci_test.git 'dir' --branch 'master'", cmds[5])
}

func (s *GitGetProjectSuite) TestBuildCloneCommandWithHTTPSNeedsToken() {
	c := &gitFetchProject{
		Directory: "dir",
	}
	conf := s.taskConfig1
	logger, err := s.comm.GetLoggerProducer(s.ctx, client.TaskData{ID: conf.Task.Id, Secret: conf.Task.Secret}, nil)
	s.NoError(err)

	opts := cloneOpts{
		method: evergreen.CloneMethodOAuth,
		owner:  conf.ProjectRef.Owner,
		repo:   conf.ProjectRef.Repo,
		branch: conf.ProjectRef.Branch,
		dir:    c.Directory,
		token:  "",
	}
	s.Require().NoError(opts.setLocation())
	_, err = c.buildCloneCommand(context.Background(), s.comm, logger, conf, opts)
	s.Error(err)
}

func (s *GitGetProjectSuite) TestBuildCloneCommandUsesSSH() {
	c := &gitFetchProject{
		Directory: "dir",
		Token:     "",
	}
	conf := s.taskConfig2
	logger, err := s.comm.GetLoggerProducer(s.ctx, client.TaskData{ID: conf.Task.Id, Secret: conf.Task.Secret}, nil)
	s.NoError(err)

	opts := cloneOpts{
		method: evergreen.CloneMethodLegacySSH,
		owner:  "deafgoat",
		repo:   "mci_test",
		branch: "main",
		dir:    c.Directory,
	}
	s.Require().NoError(opts.setLocation())
	cmds, err := c.buildCloneCommand(s.ctx, s.comm, logger, conf, opts)
	s.Require().NoError(err)
	s.Equal("git clone 'git@github.com:deafgoat/mci_test.git' 'dir' --branch 'main'", cmds[3])
}

func (s *GitGetProjectSuite) TestBuildCloneCommandDefaultCloneMethodUsesSSH() {
	c := &gitFetchProject{
		Directory: "dir",
	}
	conf := s.taskConfig2
	logger, err := s.comm.GetLoggerProducer(s.ctx, client.TaskData{ID: conf.Task.Id, Secret: conf.Task.Secret}, nil)
	s.NoError(err)

	opts := cloneOpts{
		owner:  conf.ProjectRef.Owner,
		repo:   conf.ProjectRef.Repo,
		branch: conf.ProjectRef.Branch,
		dir:    c.Directory,
	}
	s.Require().NoError(opts.setLocation())
	cmds, err := c.buildCloneCommand(s.ctx, s.comm, logger, conf, opts)
	s.Require().NoError(err)
	s.Equal("git clone 'git@github.com:evergreen-ci/sample.git' 'dir' --branch 'main'", cmds[3])
}

func (s *GitGetProjectSuite) TestBuildCloneCommandCloneDepth() {
	c := &gitFetchProject{
		Directory: "dir",
	}
	conf := s.taskConfig2
	logger, err := s.comm.GetLoggerProducer(s.ctx, client.TaskData{ID: conf.Task.Id, Secret: conf.Task.Secret}, nil)
	s.NoError(err)

	opts := cloneOpts{
		owner:      conf.ProjectRef.Owner,
		repo:       conf.ProjectRef.Repo,
		branch:     conf.ProjectRef.Branch,
		dir:        c.Directory,
		cloneDepth: 50,
	}
	s.Require().NoError(opts.setLocation())
	cmds, err := c.buildCloneCommand(s.ctx, s.comm, logger, conf, opts)
	s.Require().NoError(err)
	combined := strings.Join(cmds, " ")
	s.Contains(combined, "--depth 50")
	s.Contains(combined, "git log HEAD..")
}

func (s *GitGetProjectSuite) TestGitPlugin() {
	conf := s.taskConfig1
	logger, err := s.comm.GetLoggerProducer(s.ctx, client.TaskData{ID: conf.Task.Id, Secret: conf.Task.Secret}, nil)
	s.NoError(err)
	token, err := s.settings.GetGithubOauthToken()
	s.Require().NoError(err)
	conf.Expansions.Put("github", token)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for _, task := range conf.Project.Tasks {
		s.NotEqual(len(task.Commands), 0)
		for _, command := range task.Commands {
			pluginCmds, err := Render(command, conf.Project, BlockInfo{})
			s.NoError(err)
			s.NotNil(pluginCmds)
			pluginCmds[0].SetJasperManager(s.jasper)
			err = pluginCmds[0].Execute(ctx, s.comm, logger, conf)
			s.NoError(err)
		}
	}
}

func (s *GitGetProjectSuite) TestGitFetchRetries() {
	c := gitFetchProject{Directory: "dir"}

	conf := s.taskConfig1
	conf.Distro.CloneMethod = "this is not real!"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logger, err := s.comm.GetLoggerProducer(ctx, client.TaskData{ID: conf.Task.Id, Secret: conf.Task.Secret}, nil)
	s.NoError(err)

	err = c.Execute(ctx, s.comm, logger, conf)
	s.Error(err)
}

func (s *GitGetProjectSuite) TestTokenScrubbedFromLogger() {
	conf := s.taskConfig1
	conf.ProjectRef.Repo = "doesntexist"
	conf.Distro = nil
	token, err := s.settings.GetGithubOauthToken()
	s.Require().NoError(err)
	conf.Expansions.Put(evergreen.GlobalGitHubTokenExpansion, token)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger, err := s.comm.GetLoggerProducer(ctx, client.TaskData{ID: conf.Task.Id, Secret: conf.Task.Secret}, nil)
	s.NoError(err)

	for _, task := range conf.Project.Tasks {
		s.NotEqual(len(task.Commands), 0)
		for _, command := range task.Commands {
			pluginCmds, err := Render(command, conf.Project, BlockInfo{})
			s.NoError(err)
			s.NotNil(pluginCmds)
			pluginCmds[0].SetJasperManager(s.jasper)
			err = pluginCmds[0].Execute(ctx, s.comm, logger, conf)
			s.Error(err)
		}
	}

	s.NoError(logger.Close())
	foundCloneCommand := false
	foundCloneErr := false
	for _, msgs := range s.comm.GetMockMessages() {
		for _, msg := range msgs {
			if strings.Contains(msg.Message, "https://[redacted oauth token]:x-oauth-basic@github.com/deafgoat/doesntexist.git") {
				foundCloneCommand = true
			}
			if strings.Contains(msg.Message, "Repository not found.") {
				foundCloneErr = true
			}
			if strings.Contains(msg.Message, token) {
				s.FailNow("token was leaked")
			}
		}
	}
	s.True(foundCloneCommand)
	s.True(foundCloneErr)
}

func (s *GitGetProjectSuite) TestStdErrLogged() {
	if os.Getenv("IS_DOCKER") == "true" {
		s.T().Skip("TestStdErrLogged will not run on docker since it requires a SSH key")
	}
	conf := s.taskConfig5
	conf.Distro.CloneMethod = evergreen.CloneMethodLegacySSH
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logger, err := s.comm.GetLoggerProducer(ctx, client.TaskData{ID: conf.Task.Id, Secret: conf.Task.Secret}, nil)
	s.NoError(err)

	for _, task := range conf.Project.Tasks {
		s.NotEqual(len(task.Commands), 0)
		for _, command := range task.Commands {
			pluginCmds, err := Render(command, conf.Project, BlockInfo{})
			s.NoError(err)
			s.NotNil(pluginCmds)
			pluginCmds[0].SetJasperManager(s.jasper)
			err = pluginCmds[0].Execute(ctx, s.comm, logger, conf)
			s.Error(err)
		}
	}

	s.NoError(logger.Close())
	foundCloneCommand := false
	foundCloneErr := false
	foundSSHErr := false
	for _, msgs := range s.comm.GetMockMessages() {
		for _, msg := range msgs {
			if strings.Contains(msg.Message, "git clone 'git@github.com:evergreen-ci/doesntexist.git' 'src' --branch 'main'") {
				foundCloneCommand = true
			}
			if strings.Contains(msg.Message, "ERROR: Repository not found.") {
				foundCloneErr = true
			}
			if strings.Contains(msg.Message, "Permission denied (publickey)") || strings.Contains(msg.Message, "Host key verification failed.") {
				foundSSHErr = true
			}
		}
	}
	s.True(foundCloneCommand)
	s.True(foundCloneErr || foundSSHErr)
}

func (s *GitGetProjectSuite) TestValidateGitCommands() {
	const refToCompare = "cf46076567e4949f9fc68e0634139d4ac495c89b" // Note: also defined in test_config.yml

	conf := s.taskConfig2
	conf.Distro.CloneMethod = evergreen.CloneMethodOAuth
	token, err := s.settings.GetGithubOauthToken()
	s.Require().NoError(err)
	conf.Expansions.Put(evergreen.GlobalGitHubTokenExpansion, token)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logger, err := s.comm.GetLoggerProducer(ctx, client.TaskData{ID: conf.Task.Id, Secret: conf.Task.Secret}, nil)
	s.NoError(err)
	var pluginCmds []Command

	for _, task := range conf.Project.Tasks {
		for _, command := range task.Commands {
			pluginCmds, err = Render(command, conf.Project, BlockInfo{})
			s.NoError(err)
			s.NotNil(pluginCmds)
			pluginCmds[0].SetJasperManager(s.jasper)
			err = pluginCmds[0].Execute(ctx, s.comm, logger, conf)
			s.NoError(err)
		}
	}
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = conf.WorkDir + "/src/hello/module/sample/"
	var out bytes.Buffer
	cmd.Stdout = &out
	err = cmd.Run()
	s.NoError(err)
	ref := strings.Trim(out.String(), "\n") // Revision that we actually checked out
	s.Equal(refToCompare, ref)
	s.Equal("hello/module", conf.ModulePaths["sample"])
}

func (s *GitGetProjectSuite) TestBuildHTTPCloneCommand() {
	projectRef := &model.ProjectRef{
		Owner:  "deafgoat",
		Repo:   "mci_test",
		Branch: "main",
	}

	// build clone command to clone by http, main branch with token into 'dir'
	opts := cloneOpts{
		method:      evergreen.CloneMethodOAuth,
		owner:       projectRef.Owner,
		repo:        projectRef.Repo,
		branch:      projectRef.Branch,
		dir:         "dir",
		token:       projectGitHubToken,
		cloneParams: "--filter=tree:0 --single-branch",
	}
	s.Require().NoError(opts.setLocation())
	cmds, err := opts.buildHTTPCloneCommand()
	s.NoError(err)
	s.Require().Len(cmds, 5)
	s.Equal("set +o xtrace", cmds[0])
	s.Equal("echo \"git clone https://[redacted oauth token]:x-oauth-basic@github.com/deafgoat/mci_test.git 'dir' --branch 'main' --filter=tree:0 --single-branch\"", cmds[1])
	s.Equal("git clone https://PROJECTTOKEN:x-oauth-basic@github.com/deafgoat/mci_test.git 'dir' --branch 'main' --filter=tree:0 --single-branch", cmds[2])
	s.Equal("set -o xtrace", cmds[3])
	s.Equal("cd dir", cmds[4])

	// build clone command to clone by http with token into 'dir' w/o specified branch
	opts.branch = ""
	cmds, err = opts.buildHTTPCloneCommand()
	s.NoError(err)
	s.Require().Len(cmds, 5)
	s.Equal("set +o xtrace", cmds[0])
	s.Equal("echo \"git clone https://[redacted oauth token]:x-oauth-basic@github.com/deafgoat/mci_test.git 'dir' --filter=tree:0 --single-branch\"", cmds[1])
	s.Equal("git clone https://PROJECTTOKEN:x-oauth-basic@github.com/deafgoat/mci_test.git 'dir' --filter=tree:0 --single-branch", cmds[2])
	s.Equal("set -o xtrace", cmds[3])
	s.Equal("cd dir", cmds[4])

	// build clone command with a URL that uses http, and ensure it's
	// been forced to use https
	opts.location = "http://github.com/deafgoat/mci_test.git"
	opts.branch = projectRef.Branch
	cmds, err = opts.buildHTTPCloneCommand()
	s.NoError(err)
	s.Require().Len(cmds, 5)
	s.Equal("echo \"git clone https://[redacted oauth token]:x-oauth-basic@github.com/deafgoat/mci_test.git 'dir' --branch 'main' --filter=tree:0 --single-branch\"", cmds[1])
	s.Equal("git clone https://PROJECTTOKEN:x-oauth-basic@github.com/deafgoat/mci_test.git 'dir' --branch 'main' --filter=tree:0 --single-branch", cmds[2])

	// ensure that we aren't sending the github oauth token to other
	// servers
	opts.location = "http://someothergithost.com/something/else.git"
	cmds, err = opts.buildHTTPCloneCommand()
	s.NoError(err)
	s.Require().Len(cmds, 5)
	s.Equal("echo \"git clone https://[redacted oauth token]:x-oauth-basic@someothergithost.com/deafgoat/mci_test.git 'dir' --branch 'main' --filter=tree:0 --single-branch\"", cmds[1])
	s.Equal("git clone https://PROJECTTOKEN:x-oauth-basic@someothergithost.com/deafgoat/mci_test.git 'dir' --branch 'main' --filter=tree:0 --single-branch", cmds[2])
}

func (s *GitGetProjectSuite) TestBuildSSHCloneCommand() {
	// ssh clone command with branch
	opts := cloneOpts{
		method:      evergreen.CloneMethodLegacySSH,
		owner:       "deafgoat",
		repo:        "mci_test",
		branch:      "main",
		dir:         "dir",
		cloneParams: "--filter=tree:0 --single-branch",
	}
	s.Require().NoError(opts.setLocation())
	cmds, err := opts.buildSSHCloneCommand()
	s.NoError(err)
	s.Len(cmds, 2)
	s.Equal("git clone 'git@github.com:deafgoat/mci_test.git' 'dir' --branch 'main' --filter=tree:0 --single-branch", cmds[0])
	s.Equal("cd dir", cmds[1])

	// ssh clone command without branch
	opts.branch = ""
	cmds, err = opts.buildSSHCloneCommand()
	s.NoError(err)
	s.Len(cmds, 2)
	s.Equal("git clone 'git@github.com:deafgoat/mci_test.git' 'dir' --filter=tree:0 --single-branch", cmds[0])
	s.Equal("cd dir", cmds[1])
}

func (s *GitGetProjectSuite) TestBuildCommand() {
	conf := s.taskConfig1
	logger, err := s.comm.GetLoggerProducer(s.ctx, client.TaskData{ID: conf.Task.Id, Secret: conf.Task.Secret}, nil)
	s.NoError(err)

	c := gitFetchProject{
		Directory: "dir",
		Token:     projectGitHubToken,
	}

	// ensure clone command with legacy SSH contains "git@github.com"
	opts := cloneOpts{
		method: evergreen.CloneMethodLegacySSH,
		branch: conf.ProjectRef.Branch,
		owner:  conf.ProjectRef.Owner,
		repo:   conf.ProjectRef.Repo,
		dir:    c.Directory,
	}
	s.Require().NoError(opts.setLocation())
	cmds, err := c.buildCloneCommand(s.ctx, s.comm, logger, conf, opts)
	s.NoError(err)
	s.Require().Len(cmds, 7)
	s.Equal("set -o xtrace", cmds[0])
	s.Equal("set -o errexit", cmds[1])
	s.Equal("rm -rf dir", cmds[2])
	s.Equal("git clone 'git@github.com:deafgoat/mci_test.git' 'dir' --branch 'master'", cmds[3])
	s.Equal("cd dir", cmds[4])
	s.Equal("git reset --hard ", cmds[5])
	s.Equal("git log --oneline -n 10", cmds[6])

	// ensure clone command with location containing "https://github.com" uses
	// HTTPS.
	opts.method = evergreen.CloneMethodOAuth
	opts.token = c.Token
	s.Require().NoError(opts.setLocation())
	s.Require().NoError(err)
	cmds, err = c.buildCloneCommand(s.ctx, s.comm, logger, conf, opts)
	s.NoError(err)
	s.Require().Len(cmds, 10)
	s.Equal("set -o xtrace", cmds[0])
	s.Equal("set -o errexit", cmds[1])
	s.Equal("rm -rf dir", cmds[2])
	s.Equal("set +o xtrace", cmds[3])
	s.Equal("echo \"git clone https://[redacted oauth token]:x-oauth-basic@github.com/deafgoat/mci_test.git 'dir' --branch 'master'\"", cmds[4])
	s.Equal("git clone https://PROJECTTOKEN:x-oauth-basic@github.com/deafgoat/mci_test.git 'dir' --branch 'master'", cmds[5])
	s.Equal("set -o xtrace", cmds[6])
	s.Equal("cd dir", cmds[7])
	s.Equal("git reset --hard ", cmds[8])
	s.Equal("git log --oneline -n 10", cmds[9])
}

func (s *GitGetProjectSuite) TestBuildCommandForPullRequests() {
	conf := s.taskConfig3
	logger, err := s.comm.GetLoggerProducer(s.ctx, client.TaskData{ID: conf.Task.Id, Secret: conf.Task.Secret}, nil)
	s.NoError(err)

	c := gitFetchProject{
		Directory: "dir",
	}

	opts := cloneOpts{
		method: evergreen.CloneMethodLegacySSH,
		branch: conf.ProjectRef.Branch,
		owner:  conf.ProjectRef.Owner,
		repo:   conf.ProjectRef.Repo,
		dir:    c.Directory,
	}
	s.Require().NoError(opts.setLocation())

	cmds, err := c.buildCloneCommand(s.ctx, s.comm, logger, conf, opts)
	s.NoError(err)
	s.Require().Len(cmds, 9)
	s.True(strings.HasPrefix(cmds[5], "git fetch origin \"pull/9001/head:evg-pr-test-"))
	s.True(strings.HasPrefix(cmds[6], "git checkout \"evg-pr-test-"))
	s.Equal("git reset --hard 55ca6286e3e4f4fba5d0448333fa99fc5a404a73", cmds[7])
	s.Equal("git log --oneline -n 10", cmds[8])
}
func (s *GitGetProjectSuite) TestBuildCommandForGitHubMergeQueue() {
	conf := s.taskConfig7
	logger, err := s.comm.GetLoggerProducer(s.ctx, client.TaskData{ID: conf.Task.Id, Secret: conf.Task.Secret}, nil)
	s.NoError(err)

	c := gitFetchProject{
		Directory: "dir",
	}

	opts := cloneOpts{
		method: evergreen.CloneMethodLegacySSH,
		branch: conf.ProjectRef.Branch,
		owner:  conf.ProjectRef.Owner,
		repo:   conf.ProjectRef.Repo,
		dir:    c.Directory,
	}
	s.Require().NoError(opts.setLocation())

	cmds, err := c.buildCloneCommand(s.ctx, s.comm, logger, conf, opts)
	s.NoError(err)
	s.Len(cmds, 9)
	s.True(strings.HasPrefix(cmds[5], "git fetch origin \"gh-readonly-queue/main/pr-515-9cd8a2532bcddf58369aa82eb66ba88e2323c056:evg-mg-test-"))
	s.True(strings.HasPrefix(cmds[6], "git checkout \"evg-mg-test-"))
	s.Equal("git reset --hard d2a90288ad96adca4a7d0122d8d4fd1deb24db11", cmds[7])
	s.Equal("git log --oneline -n 10", cmds[8])
}

func (s *GitGetProjectSuite) TestBuildCommandForCLIMergeTests() {
	conf := s.taskConfig2
	logger, err := s.comm.GetLoggerProducer(s.ctx, client.TaskData{ID: conf.Task.Id, Secret: conf.Task.Secret}, nil)
	s.NoError(err)

	c := gitFetchProject{
		Directory: "dir",
		Token:     projectGitHubToken,
	}

	opts := cloneOpts{
		method: evergreen.CloneMethodOAuth,
		branch: conf.ProjectRef.Branch,
		owner:  conf.ProjectRef.Owner,
		repo:   conf.ProjectRef.Repo,
		dir:    c.Directory,
		token:  c.Token,
	}
	s.Require().NoError(opts.setLocation())

	s.taskConfig2.Task.Requester = evergreen.MergeTestRequester
	cmds, err := c.buildCloneCommand(s.ctx, s.comm, logger, conf, opts)
	s.NoError(err)
	s.Len(cmds, 9)
	s.True(strings.HasSuffix(cmds[5], fmt.Sprintf("--branch '%s'", s.taskConfig2.ProjectRef.Branch)))
}

func (s *GitGetProjectSuite) TestBuildModuleCommand() {
	conf := s.taskConfig2
	c := gitFetchProject{
		Directory: "dir",
		Token:     projectGitHubToken,
	}

	opts := cloneOpts{
		method: evergreen.CloneMethodLegacySSH,
		owner:  "deafgoat",
		repo:   "mci_test",
		dir:    "module",
	}
	s.Require().NoError(opts.setLocation())

	// ensure module clone command with ssh URL does not inject token
	cmds, err := c.buildModuleCloneCommand(conf, opts, "main", nil)
	s.NoError(err)
	s.Require().Len(cmds, 5)
	s.Equal("set -o xtrace", cmds[0])
	s.Equal("set -o errexit", cmds[1])
	s.Equal("git clone 'git@github.com:deafgoat/mci_test.git' 'module'", cmds[2])
	s.Equal("cd module", cmds[3])
	s.Equal("git checkout 'main'", cmds[4])

	// ensure module clone command with http URL injects token
	opts.method = evergreen.CloneMethodOAuth
	opts.token = c.Token
	s.Require().NoError(opts.setLocation())
	cmds, err = c.buildModuleCloneCommand(conf, opts, "main", nil)
	s.NoError(err)
	s.Require().Len(cmds, 8)
	s.Equal("set -o xtrace", cmds[0])
	s.Equal("set -o errexit", cmds[1])
	s.Equal("set +o xtrace", cmds[2])
	s.Equal("echo \"git clone https://[redacted oauth token]:x-oauth-basic@github.com/deafgoat/mci_test.git 'module'\"", cmds[3])
	s.Equal("git clone https://PROJECTTOKEN:x-oauth-basic@github.com/deafgoat/mci_test.git 'module'", cmds[4])
	s.Equal("set -o xtrace", cmds[5])
	s.Equal("cd module", cmds[6])
	s.Equal("git checkout 'main'", cmds[7])

	// ensure insecure github url is forced to use https
	opts.location = "http://github.com/deafgoat/mci_test.git"
	cmds, err = c.buildModuleCloneCommand(conf, opts, "main", nil)
	s.NoError(err)
	s.Require().Len(cmds, 8)
	s.Equal("echo \"git clone https://[redacted oauth token]:x-oauth-basic@github.com/deafgoat/mci_test.git 'module'\"", cmds[3])
	s.Equal("git clone https://PROJECTTOKEN:x-oauth-basic@github.com/deafgoat/mci_test.git 'module'", cmds[4])

	conf = s.taskConfig4
	// with merge test-commit checkout
	module := &patch.ModulePatch{
		ModuleName: "test-module",
		Githash:    "1234abcd",
		PatchSet: patch.PatchSet{
			Patch: "1234",
		},
	}
	opts.method = evergreen.CloneMethodLegacySSH
	s.Require().NoError(opts.setLocation())
	cmds, err = c.buildModuleCloneCommand(conf, opts, "main", module)
	s.NoError(err)
	s.Require().Len(cmds, 7)
	s.Equal("set -o xtrace", cmds[0])
	s.Equal("set -o errexit", cmds[1])
	s.Equal("git clone 'git@github.com:deafgoat/mci_test.git' 'module'", cmds[2])
	s.Equal("cd module", cmds[3])
	s.Regexp("^git fetch origin \"pull/1234/merge:evg-merge-test-", cmds[4])
	s.Regexp("^git checkout 'evg-merge-test-", cmds[5])
	s.Equal("git reset --hard 1234abcd", cmds[6])
}

func (s *GitGetProjectSuite) TestGetApplyCommand() {
	c := &gitFetchProject{
		Directory:      "dir",
		Token:          projectGitHubToken,
		CommitterName:  "octocat",
		CommitterEmail: "octocat@github.com",
	}

	// regular patch
	tc := &internal.TaskConfig{
		Task: &task.Task{},
	}
	patchPath := filepath.Join(testutil.GetDirectoryOfFile(), "testdata", "git", "test.patch")
	applyCommand, err := c.getApplyCommand(patchPath, tc, false)
	s.NoError(err)
	s.Equal(fmt.Sprintf("git apply --binary --index < '%s'", patchPath), applyCommand)

	// mbox patch
	tc = &internal.TaskConfig{
		Task: &task.Task{
			DisplayName: evergreen.MergeTaskName,
		},
	}
	patchPath = filepath.Join(testutil.GetDirectoryOfFile(), "testdata", "git", "test_mbox.patch")
	applyCommand, err = c.getApplyCommand(patchPath, tc, false)
	s.NoError(err)
	s.Equal(fmt.Sprintf(`GIT_COMMITTER_NAME="%s" GIT_COMMITTER_EMAIL="%s" git am --keep-cr --keep < "%s"`, c.CommitterName, c.CommitterEmail, patchPath), applyCommand)
}

func (s *GitGetProjectSuite) TestCorrectModuleRevisionSetModule() {
	const correctHash = "b27779f856b211ffaf97cbc124b7082a20ea8bc0"
	conf := s.taskConfig2
	ctx := context.WithValue(context.Background(), "patch", &patch.Patch{
		Patches: []patch.ModulePatch{
			{
				ModuleName: "sample",
				Githash:    correctHash,
			},
		},
	})
	logger, err := s.comm.GetLoggerProducer(ctx, client.TaskData{ID: conf.Task.Id, Secret: conf.Task.Secret}, nil)
	s.NoError(err)

	for _, task := range conf.Project.Tasks {
		s.NotEqual(len(task.Commands), 0)
		for _, command := range task.Commands {
			var pluginCmds []Command
			pluginCmds, err = Render(command, conf.Project, BlockInfo{})
			s.NoError(err)
			s.NotNil(pluginCmds)
			pluginCmds[0].SetJasperManager(s.jasper)
			err = pluginCmds[0].Execute(ctx, s.comm, logger, conf)
			s.NoError(err)
		}
	}

	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = conf.WorkDir + "/src/hello/module/sample/"
	var out bytes.Buffer
	cmd.Stdout = &out
	err = cmd.Run()
	s.NoError(err)
	ref := strings.Trim(out.String(), "\n")
	s.Equal(correctHash, ref) // this revision is defined in the patch, returned by GetTaskPatch
	s.NoError(logger.Close())
	toCheck := `Using revision/ref 'b27779f856b211ffaf97cbc124b7082a20ea8bc0' for module 'sample' (reason: specified in set-module).`
	foundMsg := false
	for _, task := range s.comm.GetMockMessages() {
		for _, msg := range task {
			if msg.Message == toCheck {
				foundMsg = true
			}
		}
	}
	s.True(foundMsg)
	s.Equal("hello/module", conf.ModulePaths["sample"])
}

func (s *GitGetProjectSuite) TestCorrectModuleRevisionManifest() {
	const correctHash = "3585388b1591dfca47ac26a5b9a564ec8f138a5e"
	conf := s.taskConfig2
	conf.Expansions.Put(moduleRevExpansionName("sample"), correctHash)
	logger, err := s.comm.GetLoggerProducer(s.ctx, client.TaskData{ID: conf.Task.Id, Secret: conf.Task.Secret}, nil)
	s.NoError(err)

	for _, task := range conf.Project.Tasks {
		s.NotEqual(len(task.Commands), 0)
		for _, command := range task.Commands {
			var pluginCmds []Command
			pluginCmds, err = Render(command, conf.Project, BlockInfo{})
			s.NoError(err)
			s.NotNil(pluginCmds)
			pluginCmds[0].SetJasperManager(s.jasper)
			err = pluginCmds[0].Execute(s.ctx, s.comm, logger, conf)
			s.NoError(err)
		}
	}

	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = conf.WorkDir + "/src/hello/module/sample/"
	var out bytes.Buffer
	cmd.Stdout = &out
	err = cmd.Run()
	s.NoError(err)
	ref := strings.Trim(out.String(), "\n")
	s.Equal(correctHash, ref)
	s.NoError(logger.Close())
	toCheck := `Using revision/ref '3585388b1591dfca47ac26a5b9a564ec8f138a5e' for module 'sample' (reason: from manifest).`
	foundMsg := false
	for _, task := range s.comm.GetMockMessages() {
		for _, msg := range task {
			if msg.Message == toCheck {
				foundMsg = true
			}
		}
	}
	s.True(foundMsg)
	s.Equal("hello/module", conf.ModulePaths["sample"])
}

func (s *GitGetProjectSuite) TearDownSuite() {
	if s.taskConfig1 != nil {
		s.NoError(os.RemoveAll(s.taskConfig1.WorkDir))
	}
	if s.taskConfig2 != nil {
		s.NoError(os.RemoveAll(s.taskConfig2.WorkDir))
	}
	s.cancel()
}

func (s *GitGetProjectSuite) TestAllowsEmptyPatches() {
	dir := s.T().TempDir()

	c := gitFetchProject{
		Directory: dir,
		Token:     projectGitHubToken,
	}

	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "init", dir)
	s.NotNil(cmd)
	s.NoError(cmd.Run())

	sender := send.MakeInternalLogger()
	logger := client.NewSingleChannelLogHarness("", sender)

	conf := internal.TaskConfig{
		WorkDir: dir,
	}

	s.NoError(c.applyPatch(ctx, logger, &conf, []patch.ModulePatch{{}}, false))
	s.Equal(1, sender.Len())

	msg := sender.GetMessage()
	s.Require().NotNil(msg)
	s.Equal(level.Info, msg.Priority)
	s.Equal("Skipping empty patch file...", msg.Message.String())
}

func (s *GitGetProjectSuite) TestCloneOptsSetLocationGitHub() {
	opts := cloneOpts{
		method: "",
		owner:  "foo",
		repo:   "bar",
		token:  "",
	}
	s.Require().NoError(opts.setLocation())
	s.Equal("git@github.com:foo/bar.git", opts.location)

	opts.method = evergreen.CloneMethodLegacySSH
	s.Require().NoError(opts.setLocation())
	s.Equal("git@github.com:foo/bar.git", opts.location)

	opts.method = evergreen.CloneMethodOAuth
	s.Require().NoError(opts.setLocation())
	s.Equal("https://github.com/foo/bar.git", opts.location)

	opts.method = evergreen.CloneMethodLegacySSH
	opts.token = globalGitHubToken
	s.Require().NoError(opts.setLocation())
	s.Equal("git@github.com:foo/bar.git", opts.location)

	opts.method = "foo"
	opts.token = ""
	s.Error(opts.setLocation())
}

func (s *GitGetProjectSuite) TestGetProjectMethodAndToken() {
	var token string
	var method string
	var err error

	method, token, err = getProjectMethodAndToken(projectGitHubToken, globalGitHubToken, githubAppToken, evergreen.CloneMethodOAuth)
	s.NoError(err)
	s.Equal(projectGitHubToken, token)
	s.Equal(evergreen.CloneMethodOAuth, method)

	method, token, err = getProjectMethodAndToken(projectGitHubToken, globalGitHubToken, githubAppToken, evergreen.CloneMethodLegacySSH)
	s.NoError(err)
	s.Equal(projectGitHubToken, token)
	s.Equal(evergreen.CloneMethodOAuth, method)

	method, token, err = getProjectMethodAndToken(projectGitHubToken, "", "", evergreen.CloneMethodOAuth)
	s.NoError(err)
	s.Equal(projectGitHubToken, token)
	s.Equal(evergreen.CloneMethodOAuth, method)

	method, token, err = getProjectMethodAndToken(projectGitHubToken, "", "", evergreen.CloneMethodLegacySSH)
	s.NoError(err)
	s.Equal(projectGitHubToken, token)
	s.Equal(evergreen.CloneMethodOAuth, method)

	method, token, err = getProjectMethodAndToken("", globalGitHubToken, githubAppToken, evergreen.CloneMethodOAuth)
	s.NoError(err)
	s.Equal(githubAppToken, token)
	s.Equal(evergreen.CloneMethodOAuth, method)

	method, token, err = getProjectMethodAndToken("", "", "", evergreen.CloneMethodLegacySSH)
	s.NoError(err)
	s.Equal("", token)
	s.Equal(evergreen.CloneMethodLegacySSH, method)

	method, token, err = getProjectMethodAndToken("", "", "", evergreen.CloneMethodOAuth)
	s.Error(err)
	s.Equal("", token)
	s.Equal(evergreen.CloneMethodLegacySSH, method)

	method, token, err = getProjectMethodAndToken("", "", "", evergreen.CloneMethodLegacySSH)
	s.NoError(err)
	s.Equal("", token)
	s.Equal(evergreen.CloneMethodLegacySSH, method)

	method, token, err = getProjectMethodAndToken("", "", "", "")
	s.NoError(err)
	s.Equal("", token)
	s.Equal(evergreen.CloneMethodLegacySSH, method)

	method, token, err = getProjectMethodAndToken("", "", "", "foobar")
	s.Error(err)
	s.Equal("", token)
	s.Equal("", method)

	_, _, err = getProjectMethodAndToken("", "token this is an invalid token", "", evergreen.CloneMethodOAuth)
	s.Error(err)

	_, _, err = getProjectMethodAndToken("token this is an invalid token", "", "", evergreen.CloneMethodOAuth)
	s.Error(err)
}

func (s *GitGetProjectSuite) TestReorderPatches() {
	patches := []patch.ModulePatch{{ModuleName: ""}}
	patches = reorderPatches(patches)
	s.Equal("", patches[0].ModuleName)

	patches = []patch.ModulePatch{
		{ModuleName: ""},
		{ModuleName: "m0"},
		{ModuleName: "m1"},
	}
	patches = reorderPatches(patches)
	s.Equal("m0", patches[0].ModuleName)
	s.Equal("m1", patches[1].ModuleName)
	s.Equal("", patches[2].ModuleName)
}

func (s *GitGetProjectSuite) TestMergeMultiplePatches() {
	conf := s.taskConfig6
	token, err := s.settings.GetGithubOauthToken()
	s.Require().NoError(err)
	conf.Expansions.Put("github", token)
	ctx := context.WithValue(context.Background(), "patch", &patch.Patch{
		Id: "p",
		Patches: []patch.ModulePatch{
			{Githash: "d0d878e81b303fd2abbf09331e54af41d6cd0c7d", PatchSet: patch.PatchSet{PatchFileId: "patchfile1"}, ModuleName: "evergreen"},
		},
	})
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	s.comm.PatchFiles["patchfile1"] = `
diff --git a/README.md b/README.md
index edc0c34..8e82862 100644
--- a/README.md
+++ b/README.md
@@ -1,2 +1,3 @@
 mci_test
 ========
+another line
`

	sender := send.MakeInternalLogger()
	logger := client.NewSingleChannelLogHarness("test", sender)

	for _, task := range conf.Project.Tasks {
		s.NotEqual(len(task.Commands), 0)
		for _, command := range task.Commands {
			pluginCmds, err := Render(command, conf.Project, BlockInfo{})
			s.NoError(err)
			s.NotNil(pluginCmds)
			pluginCmds[0].SetJasperManager(s.jasper)
			err = pluginCmds[0].Execute(ctx, s.comm, logger, conf)
			// Running the git commands takes time, so it could hit the test's
			// context timeout if it's slow. Make sure that the error isn't due
			// to a timeout.
			s.False(utility.IsContextError(errors.Cause(err)))
			// This command will error because it'll apply the same patch twice.
			// We are just testing that there was an attempt to apply the patch
			// the second time.
			s.Error(err)
		}
	}

	successMessage := "Applied changes from previous commit queue patch '555555555555555555555555'"
	foundSuccessMessage := false
	for msg, ok := sender.GetMessageSafe(); ok; msg, ok = sender.GetMessageSafe() {
		if strings.Contains(msg.Message.String(), successMessage) {
			foundSuccessMessage = true
		}
	}
	s.True(foundSuccessMessage, "did not see the following in task output: %s", successMessage)
}
