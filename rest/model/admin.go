package model

import (
	"reflect"
	"strings"

	"github.com/evergreen-ci/evergreen"
	"github.com/evergreen-ci/utility"
	"github.com/mongodb/grip/send"
	"github.com/pkg/errors"
)

func NewConfigModel() *APIAdminSettings {
	return &APIAdminSettings{
		Amboy:             &APIAmboyConfig{},
		Api:               &APIapiConfig{},
		AuthConfig:        &APIAuthConfig{},
		Buckets:           &APIBucketConfig{},
		Cedar:             &APICedarConfig{},
		CommitQueue:       &APICommitQueueConfig{},
		ContainerPools:    &APIContainerPoolsConfig{},
		Credentials:       map[string]string{},
		DataPipes:         &APIDataPipesConfig{},
		Expansions:        map[string]string{},
		HostInit:          &APIHostInitConfig{},
		HostJasper:        &APIHostJasperConfig{},
		Jira:              &APIJiraConfig{},
		JIRANotifications: &APIJIRANotificationsConfig{},
		Keys:              map[string]string{},
		LDAPRoleMap:       &APILDAPRoleMap{},
		LoggerConfig:      &APILoggerConfig{},
		NewRelic:          &APINewRelicConfig{},
		Notify:            &APINotifyConfig{},
		Plugins:           map[string]map[string]interface{}{},
		PodLifecycle:      &APIPodLifecycleConfig{},
		ProjectCreation:   &APIProjectCreationConfig{},
		Providers:         &APICloudProviders{},
		RepoTracker:       &APIRepoTrackerConfig{},
		Scheduler:         &APISchedulerConfig{},
		ServiceFlags:      &APIServiceFlags{},
		Slack:             &APISlackConfig{},
		Splunk:            &APISplunkConfig{},
		Triggers:          &APITriggerConfig{},
		Ui:                &APIUIConfig{},
		Spawnhost:         &APISpawnHostConfig{},
		Tracer:            &APITracerSettings{},
	}
}

// APIAdminSettings is the structure of a response to the admin route
type APIAdminSettings struct {
	Amboy               *APIAmboyConfig                   `json:"amboy,omitempty"`
	Api                 *APIapiConfig                     `json:"api,omitempty"`
	ApiUrl              *string                           `json:"api_url,omitempty"`
	AWSInstanceRole     *string                           `json:"aws_instance_role,omitempty"`
	AuthConfig          *APIAuthConfig                    `json:"auth,omitempty"`
	Banner              *string                           `json:"banner,omitempty"`
	BannerTheme         *string                           `json:"banner_theme,omitempty"`
	Buckets             *APIBucketConfig                  `json:"buckets,omitempty"`
	Cedar               *APICedarConfig                   `json:"cedar,omitempty"`
	ClientBinariesDir   *string                           `json:"client_binaries_dir,omitempty"`
	CommitQueue         *APICommitQueueConfig             `json:"commit_queue,omitempty"`
	ConfigDir           *string                           `json:"configdir,omitempty"`
	ContainerPools      *APIContainerPoolsConfig          `json:"container_pools,omitempty"`
	Credentials         map[string]string                 `json:"credentials,omitempty"`
	DomainName          *string                           `json:"domain_name,omitempty"`
	DataPipes           *APIDataPipesConfig               `json:"data_pipes,omitempty"`
	Expansions          map[string]string                 `json:"expansions,omitempty"`
	GithubPRCreatorOrg  *string                           `json:"github_pr_creator_org,omitempty"`
	GithubOrgs          []string                          `json:"github_orgs,omitempty"`
	DisabledGQLQueries  []string                          `json:"disabled_gql_queries"`
	HostInit            *APIHostInitConfig                `json:"hostinit,omitempty"`
	HostJasper          *APIHostJasperConfig              `json:"host_jasper,omitempty"`
	Jira                *APIJiraConfig                    `json:"jira,omitempty"`
	JIRANotifications   *APIJIRANotificationsConfig       `json:"jira_notifications,omitempty"`
	Keys                map[string]string                 `json:"keys,omitempty"`
	LDAPRoleMap         *APILDAPRoleMap                   `json:"ldap_role_map,omitempty"`
	LoggerConfig        *APILoggerConfig                  `json:"logger_config,omitempty"`
	LogPath             *string                           `json:"log_path,omitempty"`
	NewRelic            *APINewRelicConfig                `json:"newrelic,omitempty"`
	Notify              *APINotifyConfig                  `json:"notify,omitempty"`
	Plugins             map[string]map[string]interface{} `json:"plugins,omitempty"`
	PodLifecycle        *APIPodLifecycleConfig            `json:"pod_lifecycle,omitempty"`
	PprofPort           *string                           `json:"pprof_port,omitempty"`
	ProjectCreation     *APIProjectCreationConfig         `json:"project_creation,omitempty"`
	Providers           *APICloudProviders                `json:"providers,omitempty"`
	RepoTracker         *APIRepoTrackerConfig             `json:"repotracker,omitempty"`
	Scheduler           *APISchedulerConfig               `json:"scheduler,omitempty"`
	ServiceFlags        *APIServiceFlags                  `json:"service_flags,omitempty"`
	Slack               *APISlackConfig                   `json:"slack,omitempty"`
	SSHKeyDirectory     *string                           `json:"ssh_key_directory,omitempty"`
	SSHKeyPairs         []APISSHKeyPair                   `json:"ssh_key_pairs,omitempty"`
	Splunk              *APISplunkConfig                  `json:"splunk,omitempty"`
	Triggers            *APITriggerConfig                 `json:"triggers,omitempty"`
	Ui                  *APIUIConfig                      `json:"ui,omitempty"`
	Spawnhost           *APISpawnHostConfig               `json:"spawnhost,omitempty"`
	Tracer              *APITracerSettings                `json:"tracer,omitempty"`
	ShutdownWaitSeconds *int                              `json:"shutdown_wait_seconds,omitempty"`
}

// BuildFromService builds a model from the service layer
func (as *APIAdminSettings) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case *evergreen.Settings:
		if v == nil {
			return errors.New("cannot convert nil admin settings to API model")
		}
		apiModelReflect := reflect.ValueOf(*as)
		dbModelReflect := reflect.ValueOf(*v)
		for i := 0; i < apiModelReflect.NumField(); i++ {
			propName := apiModelReflect.Type().Field(i).Name
			val := apiModelReflect.FieldByName(propName)
			if val.IsNil() {
				continue
			}

			// check to see if this property is an API model itself
			interfaceVal := val.Interface()
			model, ok := interfaceVal.(Model)
			if !ok {
				continue
			}
			// build the sub-model from the DB model. assumes that the 2 fields are named the same thing
			if err := model.BuildFromService(dbModelReflect.FieldByName(propName).Interface()); err != nil {
				return errors.Wrapf(err, "converting admin model section '%s' to API model", propName)
			}
		}
		as.ApiUrl = &v.ApiUrl
		as.AWSInstanceRole = utility.ToStringPtr(v.AWSInstanceRole)
		as.Banner = &v.Banner
		tmp := string(v.BannerTheme)
		as.BannerTheme = &tmp
		as.ClientBinariesDir = &v.ClientBinariesDir
		as.ConfigDir = &v.ConfigDir
		as.DomainName = utility.ToStringPtr(v.DomainName)
		as.GithubPRCreatorOrg = &v.GithubPRCreatorOrg
		as.LogPath = &v.LogPath
		as.Plugins = v.Plugins
		as.PprofPort = &v.PprofPort
		as.Credentials = v.Credentials
		as.Expansions = v.Expansions
		as.Keys = v.Keys
		as.GithubOrgs = v.GithubOrgs
		as.DisabledGQLQueries = v.DisabledGQLQueries
		as.SSHKeyDirectory = utility.ToStringPtr(v.SSHKeyDirectory)
		as.SSHKeyPairs = []APISSHKeyPair{}
		for _, pair := range v.SSHKeyPairs {
			as.SSHKeyPairs = append(as.SSHKeyPairs, APISSHKeyPair{
				Name:    utility.ToStringPtr(pair.Name),
				Public:  utility.ToStringPtr(pair.Public),
				Private: utility.ToStringPtr(pair.Private),
			})
		}
		uiConfig := APIUIConfig{}
		err := uiConfig.BuildFromService(v.Ui)
		if err != nil {
			return errors.Wrap(err, "converting UI config to API model")
		}
		as.Ui = &uiConfig
		jiraConfig := APIJiraConfig{}
		err = jiraConfig.BuildFromService(v.Jira)
		if err != nil {
			return errors.Wrap(err, "converting Jira config to API model")
		}
		as.Jira = &jiraConfig
		cloudProviders := APICloudProviders{}
		err = cloudProviders.BuildFromService(v.Providers)
		if err != nil {
			return errors.Wrap(err, "converting cloud provider config to API model")
		}
		as.Providers = &cloudProviders
		as.ShutdownWaitSeconds = &v.ShutdownWaitSeconds
		spawnHostConfig := APISpawnHostConfig{}
		err = spawnHostConfig.BuildFromService(v.Spawnhost)
		if err != nil {
			return errors.Wrap(err, "converting spawn host config to API model")
		}
		as.Spawnhost = &spawnHostConfig
		slackConfig := APISlackConfig{}
		err = slackConfig.BuildFromService(v.Slack)
		if err != nil {
			return errors.Wrap(err, "converting slack config to API model")
		}
		as.Slack = &slackConfig
	default:
		return errors.Errorf("programmatic error: expected admin settings but got type %T", h)
	}
	return nil
}

// ToService returns a service model from an API model
func (as *APIAdminSettings) ToService() (interface{}, error) {
	settings := evergreen.Settings{
		Credentials:        map[string]string{},
		Expansions:         map[string]string{},
		Keys:               map[string]string{},
		Plugins:            evergreen.PluginConfig{},
		GithubOrgs:         as.GithubOrgs,
		DisabledGQLQueries: as.DisabledGQLQueries,
	}
	if as.ApiUrl != nil {
		settings.ApiUrl = *as.ApiUrl
	}
	if as.AWSInstanceRole != nil {
		settings.AWSInstanceRole = *as.AWSInstanceRole
	}
	if as.Banner != nil {
		settings.Banner = *as.Banner
	}
	if as.BannerTheme != nil {
		settings.BannerTheme = evergreen.BannerTheme(*as.BannerTheme)
	}
	if as.ClientBinariesDir != nil {
		settings.ClientBinariesDir = *as.ClientBinariesDir
	}
	if as.ConfigDir != nil {
		settings.ConfigDir = *as.ConfigDir
	}
	settings.DomainName = utility.FromStringPtr(as.DomainName)

	if as.GithubPRCreatorOrg != nil {
		settings.GithubPRCreatorOrg = *as.GithubPRCreatorOrg
	}
	if as.LogPath != nil {
		settings.LogPath = *as.LogPath
	}
	if as.PprofPort != nil {
		settings.PprofPort = *as.PprofPort
	}

	apiModelReflect := reflect.ValueOf(*as)
	dbModelReflect := reflect.ValueOf(&settings).Elem()
	for i := 0; i < apiModelReflect.NumField(); i++ {
		propName := apiModelReflect.Type().Field(i).Name
		val := apiModelReflect.FieldByName(propName)
		if val.IsNil() {
			continue
		}

		// check to see if this property is an API model itself
		interfaceVal := val.Interface()
		model, ok := interfaceVal.(Model)
		if !ok {
			continue
		}
		// set the corresponding DB model field. assumes that the 2 fields are named the same thing
		i, err := model.ToService()
		if err != nil {
			return nil, errors.Wrapf(err, "converting admin model section '%s' to service model", propName)
		}
		valToSet := reflect.ValueOf(i)
		dbModelReflect.FieldByName(propName).Set(valToSet)
	}
	for k, v := range as.Credentials {
		settings.Credentials[k] = v
	}
	for k, v := range as.Expansions {
		settings.Expansions[k] = v
	}
	for k, v := range as.Keys {
		settings.Keys[k] = v
	}
	for k, v := range as.Plugins {
		settings.Plugins[k] = map[string]interface{}{}
		for k2, v2 := range v {
			settings.Plugins[k][k2] = v2
		}
	}
	settings.SSHKeyDirectory = utility.FromStringPtr(as.SSHKeyDirectory)
	settings.SSHKeyPairs = []evergreen.SSHKeyPair{}
	for _, pair := range as.SSHKeyPairs {
		settings.SSHKeyPairs = append(settings.SSHKeyPairs, evergreen.SSHKeyPair{
			Name:    utility.FromStringPtr(pair.Name),
			Public:  utility.FromStringPtr(pair.Public),
			Private: utility.FromStringPtr(pair.Private),
		})
	}

	if as.ShutdownWaitSeconds != nil {
		settings.ShutdownWaitSeconds = *as.ShutdownWaitSeconds
	}
	return settings, nil
}

type APISESConfig struct {
	SenderAddress *string `json:"sender_address"`
}

func (a *APISESConfig) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case evergreen.SESConfig:
		a.SenderAddress = utility.ToStringPtr(v.SenderAddress)
	default:
		return errors.Errorf("programmatic error: expected SESConfig but got type %T", h)
	}
	return nil
}

func (a *APISESConfig) ToService() (interface{}, error) {
	if a == nil {
		return nil, nil
	}
	config := evergreen.SESConfig{
		SenderAddress: utility.FromStringPtr(a.SenderAddress),
	}
	return config, nil
}

type APIAmboyConfig struct {
	Name                                  *string                    `json:"name"`
	SingleName                            *string                    `json:"single_name"`
	DBConnection                          APIAmboyDBConfig           `json:"db_connection"`
	PoolSizeLocal                         int                        `json:"pool_size_local"`
	PoolSizeRemote                        int                        `json:"pool_size_remote"`
	LocalStorage                          int                        `json:"local_storage_size"`
	GroupDefaultWorkers                   int                        `json:"group_default_workers"`
	GroupBackgroundCreateFrequencyMinutes int                        `json:"group_background_create_frequency"`
	GroupPruneFrequencyMinutes            int                        `json:"group_prune_frequency"`
	GroupTTLMinutes                       int                        `json:"group_ttl"`
	RequireRemotePriority                 bool                       `json:"require_remote_priority"`
	LockTimeoutMinutes                    int                        `json:"lock_timeout_minutes"`
	SampleSize                            int                        `json:"sample_size"`
	Retry                                 APIAmboyRetryConfig        `json:"retry,omitempty"`
	NamedQueues                           []APIAmboyNamedQueueConfig `json:"named_queues,omitempty"`
}

func (a *APIAmboyConfig) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case evergreen.AmboyConfig:
		a.Name = utility.ToStringPtr(v.Name)
		a.SingleName = utility.ToStringPtr(v.SingleName)
		if err := a.DBConnection.BuildFromService(v.DBConnection); err != nil {
			return errors.Wrap(err, "converting Amboy DB settings to API model")
		}
		a.PoolSizeLocal = v.PoolSizeLocal
		a.PoolSizeRemote = v.PoolSizeRemote
		a.LocalStorage = v.LocalStorage
		a.GroupDefaultWorkers = v.GroupDefaultWorkers
		a.GroupBackgroundCreateFrequencyMinutes = v.GroupBackgroundCreateFrequencyMinutes
		a.GroupPruneFrequencyMinutes = v.GroupPruneFrequencyMinutes
		a.GroupTTLMinutes = v.GroupTTLMinutes
		a.RequireRemotePriority = v.RequireRemotePriority
		a.LockTimeoutMinutes = v.LockTimeoutMinutes
		a.SampleSize = v.SampleSize
		if err := a.Retry.BuildFromService(v.Retry); err != nil {
			return errors.Wrap(err, "converting Amboy retry settings to API model")
		}
		for _, dbNamedQueue := range v.NamedQueues {
			var apiNamedQueue APIAmboyNamedQueueConfig
			apiNamedQueue.BuildFromService(dbNamedQueue)
			a.NamedQueues = append(a.NamedQueues, apiNamedQueue)
		}
	default:
		return errors.Errorf("programmatic error: expected Amboy config but got type %T", h)
	}
	return nil
}

func (a *APIAmboyConfig) ToService() (interface{}, error) {
	i, err := a.Retry.ToService()
	if err != nil {
		return nil, errors.Wrap(err, "converting Amboy retry settings to service model")
	}
	retry, ok := i.(evergreen.AmboyRetryConfig)
	if !ok {
		return nil, errors.Errorf("programmatic error: expected Amboy retry config but got type %T", i)
	}

	i, err = a.DBConnection.ToService()
	if err != nil {
		return nil, errors.Wrap(err, "converting Amboy DB settings to service model")
	}
	db, ok := i.(evergreen.AmboyDBConfig)
	if !ok {
		return nil, errors.Errorf("programmatic error: expected Amboy DB config but got type %T", i)
	}

	var dbNamedQueues []evergreen.AmboyNamedQueueConfig
	for _, apiNamedQueue := range a.NamedQueues {
		dbNamedQueues = append(dbNamedQueues, apiNamedQueue.ToService())
	}
	return evergreen.AmboyConfig{
		Name:                                  utility.FromStringPtr(a.Name),
		SingleName:                            utility.FromStringPtr(a.SingleName),
		DBConnection:                          db,
		PoolSizeLocal:                         a.PoolSizeLocal,
		PoolSizeRemote:                        a.PoolSizeRemote,
		LocalStorage:                          a.LocalStorage,
		GroupDefaultWorkers:                   a.GroupDefaultWorkers,
		GroupBackgroundCreateFrequencyMinutes: a.GroupBackgroundCreateFrequencyMinutes,
		GroupPruneFrequencyMinutes:            a.GroupPruneFrequencyMinutes,
		GroupTTLMinutes:                       a.GroupTTLMinutes,
		RequireRemotePriority:                 a.RequireRemotePriority,
		LockTimeoutMinutes:                    a.LockTimeoutMinutes,
		SampleSize:                            a.SampleSize,
		Retry:                                 retry,
		NamedQueues:                           dbNamedQueues,
	}, nil
}

type APIAmboyDBConfig struct {
	URL      *string `json:"url"`
	Database *string `json:"database"`
	Username *string `json:"username"`
	Password *string `json:"password"`
}

func (a *APIAmboyDBConfig) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case evergreen.AmboyDBConfig:
		a.URL = utility.ToStringPtr(v.URL)
		a.Database = utility.ToStringPtr(v.Database)
		a.Username = utility.ToStringPtr(v.Username)
		a.Password = utility.ToStringPtr(v.Password)
		return nil
	default:
		return errors.Errorf("programmatic error: expected Amboy DB config but got type %T", h)
	}
}

func (a *APIAmboyDBConfig) ToService() (interface{}, error) {
	return evergreen.AmboyDBConfig{
		URL:      utility.FromStringPtr(a.URL),
		Database: utility.FromStringPtr(a.Database),
		Username: utility.FromStringPtr(a.Username),
		Password: utility.FromStringPtr(a.Password),
	}, nil
}

type APIAmboyRetryConfig struct {
	NumWorkers                          int `json:"num_workers,omitempty"`
	MaxCapacity                         int `json:"max_capacity,omitempty"`
	MaxRetryAttempts                    int `json:"max_retry_attempts,omitempty"`
	MaxRetryTimeSeconds                 int `json:"max_retry_time_seconds,omitempty"`
	RetryBackoffSeconds                 int `json:"retry_backoff_seconds,omitempty"`
	StaleRetryingMonitorIntervalSeconds int `json:"stale_retrying_monitor_interval_seconds,omitempty"`
}

func (a *APIAmboyRetryConfig) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case evergreen.AmboyRetryConfig:
		a.NumWorkers = v.NumWorkers
		a.MaxCapacity = v.MaxCapacity
		a.MaxRetryAttempts = v.MaxRetryAttempts
		a.MaxRetryTimeSeconds = v.MaxRetryTimeSeconds
		a.RetryBackoffSeconds = v.RetryBackoffSeconds
		a.StaleRetryingMonitorIntervalSeconds = v.StaleRetryingMonitorIntervalSeconds
		return nil
	default:
		return errors.Errorf("programmatic error: expected Amboy retry config but got type %T", h)
	}
}

func (a *APIAmboyRetryConfig) ToService() (interface{}, error) {
	return evergreen.AmboyRetryConfig{
		NumWorkers:                          a.NumWorkers,
		MaxCapacity:                         a.MaxCapacity,
		MaxRetryAttempts:                    a.MaxRetryAttempts,
		MaxRetryTimeSeconds:                 a.MaxRetryTimeSeconds,
		RetryBackoffSeconds:                 a.RetryBackoffSeconds,
		StaleRetryingMonitorIntervalSeconds: a.StaleRetryingMonitorIntervalSeconds,
	}, nil
}

// APIAmboyNamedQueueConfig is the model for named Amboy queue settings.
type APIAmboyNamedQueueConfig struct {
	Name               *string `json:"name"`
	Regexp             *string `json:"regexp"`
	NumWorkers         int     `json:"num_workers,omitempty"`
	SampleSize         int     `json:"sample_size,omitempty"`
	LockTimeoutSeconds int     `json:"lock_timeout_seconds,omitempty"`
}

func (a *APIAmboyNamedQueueConfig) BuildFromService(h evergreen.AmboyNamedQueueConfig) {
	a.Name = utility.ToStringPtr(h.Name)
	a.Regexp = utility.ToStringPtr(h.Regexp)
	a.NumWorkers = h.NumWorkers
	a.SampleSize = h.SampleSize
	a.LockTimeoutSeconds = h.LockTimeoutSeconds
}

func (a *APIAmboyNamedQueueConfig) ToService() evergreen.AmboyNamedQueueConfig {
	return evergreen.AmboyNamedQueueConfig{
		Name:               utility.FromStringPtr(a.Name),
		Regexp:             utility.FromStringPtr(a.Regexp),
		NumWorkers:         a.NumWorkers,
		SampleSize:         a.SampleSize,
		LockTimeoutSeconds: a.LockTimeoutSeconds,
	}
}

type APIapiConfig struct {
	HttpListenAddr      *string `json:"http_listen_addr"`
	GithubWebhookSecret *string `json:"github_webhook_secret"`
}

func (a *APIapiConfig) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case evergreen.APIConfig:
		a.HttpListenAddr = utility.ToStringPtr(v.HttpListenAddr)
		a.GithubWebhookSecret = utility.ToStringPtr(v.GithubWebhookSecret)
	default:
		return errors.Errorf("programmatic error: expected REST API config but got type %T", h)
	}
	return nil
}

func (a *APIapiConfig) ToService() (interface{}, error) {
	return evergreen.APIConfig{
		HttpListenAddr:      utility.FromStringPtr(a.HttpListenAddr),
		GithubWebhookSecret: utility.FromStringPtr(a.GithubWebhookSecret),
	}, nil
}

type APIAuthConfig struct {
	LDAP                    *APILDAPConfig       `json:"ldap"`
	Okta                    *APIOktaConfig       `json:"okta"`
	Naive                   *APINaiveAuthConfig  `json:"naive"`
	Github                  *APIGithubAuthConfig `json:"github"`
	Multi                   *APIMultiAuthConfig  `json:"multi"`
	PreferredType           *string              `json:"preferred_type"`
	BackgroundReauthMinutes int                  `json:"background_reauth_minutes"`
	AllowServiceUsers       bool                 `json:"allow_service_users"`
}

func (a *APIAuthConfig) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case evergreen.AuthConfig:
		if v.LDAP != nil {
			a.LDAP = &APILDAPConfig{}
			if err := a.LDAP.BuildFromService(v.LDAP); err != nil {
				return errors.Wrap(err, "converting LDAP auth settings to API model")
			}
		}
		if v.Okta != nil {
			a.Okta = &APIOktaConfig{}
			if err := a.Okta.BuildFromService(v.Okta); err != nil {
				return errors.Wrap(err, "converting Okta auth settings to API model")
			}
		}
		if v.Github != nil {
			a.Github = &APIGithubAuthConfig{}
			if err := a.Github.BuildFromService(v.Github); err != nil {
				return errors.Wrap(err, "converting GitHub auth settings to API model")
			}
		}
		if v.Naive != nil {
			a.Naive = &APINaiveAuthConfig{}
			if err := a.Naive.BuildFromService(v.Naive); err != nil {
				return errors.Wrap(err, "converting naive auth settings to API model")
			}
		}
		if v.Multi != nil {
			a.Multi = &APIMultiAuthConfig{}
			if err := a.Multi.BuildFromService(v.Multi); err != nil {
				return errors.Wrap(err, "converting multi auth settings to API model")
			}
		}
		a.PreferredType = utility.ToStringPtr(v.PreferredType)
		a.BackgroundReauthMinutes = v.BackgroundReauthMinutes
		a.AllowServiceUsers = v.AllowServiceUsers
	default:
		return errors.Errorf("programmatic error: expected auth config but got type %T", h)
	}
	return nil
}

func (a *APIAuthConfig) ToService() (interface{}, error) {
	var ldap *evergreen.LDAPConfig
	var okta *evergreen.OktaConfig
	var naive *evergreen.NaiveAuthConfig
	var github *evergreen.GithubAuthConfig
	var multi *evergreen.MultiAuthConfig
	var ok bool
	i, err := a.LDAP.ToService()
	if err != nil {
		return nil, errors.Wrap(err, "converting LDAP auth config to service model")
	}
	if i != nil {
		ldap, ok = i.(*evergreen.LDAPConfig)
		if !ok {
			return nil, errors.Errorf("programmatic error: expected LDAP auth config but got type %T", i)
		}
	}

	i, err = a.Okta.ToService()
	if err != nil {
		return nil, errors.Wrap(err, "converting Okta auth config to service model")
	}
	if i != nil {
		okta, ok = i.(*evergreen.OktaConfig)
		if !ok {
			return nil, errors.Errorf("programmatic error: expected Okta auth config but got type %T", i)
		}
	}

	i, err = a.Naive.ToService()
	if err != nil {
		return nil, errors.Wrap(err, "converting naive auth config to service model")
	}
	if i != nil {
		naive, ok = i.(*evergreen.NaiveAuthConfig)
		if !ok {
			return nil, errors.Errorf("programmatic error: expected naive auth config but got type %T", i)
		}
	}

	i, err = a.Github.ToService()
	if err != nil {
		return nil, errors.Wrap(err, "converting GitHub auth config to service model")
	}
	if i != nil {
		github, ok = i.(*evergreen.GithubAuthConfig)
		if !ok {
			return nil, errors.Errorf("programmatic error: expected GitHub auth config but got type %T", i)
		}
	}

	i, err = a.Multi.ToService()
	if err != nil {
		return nil, errors.Wrap(err, "converting multi auth config to service model")
	}
	if i != nil {
		multi, ok = i.(*evergreen.MultiAuthConfig)
		if !ok {
			return nil, errors.Errorf("programmatic error: expected multi auth config but got type %T", i)
		}
	}

	return evergreen.AuthConfig{
		LDAP:                    ldap,
		Okta:                    okta,
		Naive:                   naive,
		Github:                  github,
		Multi:                   multi,
		PreferredType:           utility.FromStringPtr(a.PreferredType),
		BackgroundReauthMinutes: a.BackgroundReauthMinutes,
		AllowServiceUsers:       a.AllowServiceUsers,
	}, nil
}

type APIBucketConfig struct {
	LogBucket APIBucket `json:"log_bucket"`
}

type APIBucket struct {
	Name *string `json:"name"`
	Type *string `json:"type"`
}

func (a *APIBucketConfig) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case evergreen.BucketConfig:
		a.LogBucket.Name = utility.ToStringPtr(v.LogBucket.Name)
		a.LogBucket.Type = utility.ToStringPtr(v.LogBucket.Type)
	default:
		return errors.Errorf("programmatic error: expected bucket config but got type %T", h)
	}
	return nil
}

func (a *APIBucketConfig) ToService() (interface{}, error) {
	return evergreen.BucketConfig{
		LogBucket: evergreen.Bucket{
			Name: utility.FromStringPtr(a.LogBucket.Name),
			Type: utility.FromStringPtr(a.LogBucket.Type),
		},
	}, nil
}

type APICedarConfig struct {
	BaseURL *string `json:"base_url"`
	RPCPort *string `json:"rpc_port"`
	User    *string `json:"user"`
	APIKey  *string `json:"api_key"`
}

func (a *APICedarConfig) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case evergreen.CedarConfig:
		a.BaseURL = utility.ToStringPtr(v.BaseURL)
		a.RPCPort = utility.ToStringPtr(v.RPCPort)
		a.User = utility.ToStringPtr(v.User)
		a.APIKey = utility.ToStringPtr(v.APIKey)
	default:
		return errors.Errorf("programmatic error: expected Cedar config but got type %T", h)
	}
	return nil
}

func (a *APICedarConfig) ToService() (interface{}, error) {
	return evergreen.CedarConfig{
		BaseURL: utility.FromStringPtr(a.BaseURL),
		RPCPort: utility.FromStringPtr(a.RPCPort),
		User:    utility.FromStringPtr(a.User),
		APIKey:  utility.FromStringPtr(a.APIKey),
	}, nil
}

type APILDAPConfig struct {
	URL                *string `json:"url"`
	Port               *string `json:"port"`
	UserPath           *string `json:"path"`
	ServicePath        *string `json:"service_path"`
	Group              *string `json:"group"`
	ServiceGroup       *string `json:"service_group"`
	ExpireAfterMinutes *string `json:"expire_after_minutes"`
	GroupOU            *string `json:"group_ou"`
}

func (a *APILDAPConfig) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case *evergreen.LDAPConfig:
		if v == nil {
			return nil
		}
		a.URL = utility.ToStringPtr(v.URL)
		a.Port = utility.ToStringPtr(v.Port)
		a.UserPath = utility.ToStringPtr(v.UserPath)
		a.ServicePath = utility.ToStringPtr(v.ServicePath)
		a.Group = utility.ToStringPtr(v.Group)
		a.ServiceGroup = utility.ToStringPtr(v.ServiceGroup)
		a.ExpireAfterMinutes = utility.ToStringPtr(v.ExpireAfterMinutes)
		a.GroupOU = utility.ToStringPtr(v.GroupOU)
	default:
		return errors.Errorf("programmatic error: expected LDAP config but got type %T", h)
	}
	return nil
}

func (a *APILDAPConfig) ToService() (interface{}, error) {
	if a == nil {
		return nil, nil
	}
	return &evergreen.LDAPConfig{
		URL:                utility.FromStringPtr(a.URL),
		Port:               utility.FromStringPtr(a.Port),
		UserPath:           utility.FromStringPtr(a.UserPath),
		ServicePath:        utility.FromStringPtr(a.ServicePath),
		Group:              utility.FromStringPtr(a.Group),
		ServiceGroup:       utility.FromStringPtr(a.ServiceGroup),
		ExpireAfterMinutes: utility.FromStringPtr(a.ExpireAfterMinutes),
		GroupOU:            utility.FromStringPtr(a.Group),
	}, nil
}

type APIOktaConfig struct {
	ClientID           *string  `json:"client_id"`
	ClientSecret       *string  `json:"client_secret"`
	Issuer             *string  `json:"issuer"`
	Scopes             []string `json:"scopes"`
	UserGroup          *string  `json:"user_group"`
	ExpireAfterMinutes int      `json:"expire_after_minutes"`
}

func (a *APIOktaConfig) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case *evergreen.OktaConfig:
		if v == nil {
			return nil
		}
		a.ClientID = utility.ToStringPtr(v.ClientID)
		a.ClientSecret = utility.ToStringPtr(v.ClientSecret)
		a.Issuer = utility.ToStringPtr(v.Issuer)
		a.Scopes = v.Scopes
		a.UserGroup = utility.ToStringPtr(v.UserGroup)
		a.ExpireAfterMinutes = v.ExpireAfterMinutes
		return nil
	default:
		return errors.Errorf("programmatic error: expected Okta config but got type %T", h)
	}
}

func (a *APIOktaConfig) ToService() (interface{}, error) {
	if a == nil {
		return nil, nil
	}
	return &evergreen.OktaConfig{
		ClientID:           utility.FromStringPtr(a.ClientID),
		ClientSecret:       utility.FromStringPtr(a.ClientSecret),
		Issuer:             utility.FromStringPtr(a.Issuer),
		Scopes:             a.Scopes,
		UserGroup:          utility.FromStringPtr(a.UserGroup),
		ExpireAfterMinutes: a.ExpireAfterMinutes,
	}, nil
}

type APINaiveAuthConfig struct {
	Users []APIAuthUser `json:"users"`
}

func (a *APINaiveAuthConfig) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case *evergreen.NaiveAuthConfig:
		if v == nil {
			return nil
		}
		for _, u := range v.Users {
			apiUser := APIAuthUser{}
			if err := apiUser.BuildFromService(u); err != nil {
				return err
			}
			a.Users = append(a.Users, apiUser)
		}
	default:
		return errors.Errorf("programmatic error: expected naive auth config but got type %T", h)
	}
	return nil
}

func (a *APINaiveAuthConfig) ToService() (interface{}, error) {
	if a == nil {
		return nil, nil
	}
	config := evergreen.NaiveAuthConfig{}
	for _, u := range a.Users {
		i, err := u.ToService()
		if err != nil {
			return nil, err
		}
		user, ok := i.(evergreen.AuthUser)
		if !ok {
			continue
		}
		config.Users = append(config.Users, user)
	}
	return &config, nil
}

type APIAuthUser struct {
	Username    *string `json:"username"`
	DisplayName *string `json:"display_name"`
	Password    *string `json:"password"`
	Email       *string `json:"email"`
}

func (a *APIAuthUser) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case evergreen.AuthUser:
		a.Username = utility.ToStringPtr(v.Username)
		a.Password = utility.ToStringPtr(v.Password)
		a.DisplayName = utility.ToStringPtr(v.DisplayName)
		a.Email = utility.ToStringPtr(v.Email)
	default:
		return errors.Errorf("programmatic error: expected naive auth user config but got type %T", h)
	}
	return nil
}

func (a *APIAuthUser) ToService() (interface{}, error) {
	if a == nil {
		return nil, nil
	}
	return evergreen.AuthUser{
		Username:    utility.FromStringPtr(a.Username),
		Password:    utility.FromStringPtr(a.Password),
		DisplayName: utility.FromStringPtr(a.DisplayName),
		Email:       utility.FromStringPtr(a.Email),
	}, nil
}

type APIGithubAuthConfig struct {
	AppId        int64     `json:"app_id"`
	ClientId     *string   `json:"client_id"`
	ClientSecret *string   `json:"client_secret"`
	DefaultOwner *string   `json:"default_owner"`
	DefaultRepo  *string   `json:"default_repo"`
	Organization *string   `json:"organization"`
	Users        []*string `json:"users"`
}

func (a *APIGithubAuthConfig) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case *evergreen.GithubAuthConfig:
		if v == nil {
			return nil
		}
		a.ClientId = utility.ToStringPtr(v.ClientId)
		a.ClientSecret = utility.ToStringPtr(v.ClientSecret)
		a.Organization = utility.ToStringPtr(v.Organization)
		a.DefaultOwner = utility.ToStringPtr(v.DefaultOwner)
		a.DefaultRepo = utility.ToStringPtr(v.DefaultRepo)
		a.AppId = v.AppId
		for _, u := range v.Users {
			a.Users = append(a.Users, utility.ToStringPtr(u))
		}
	default:
		return errors.Errorf("programmatic error: expected GitHub auth config but got type %T", h)
	}
	return nil
}

func (a *APIGithubAuthConfig) ToService() (interface{}, error) {
	if a == nil {
		return nil, nil
	}
	config := evergreen.GithubAuthConfig{
		ClientId:     utility.FromStringPtr(a.ClientId),
		ClientSecret: utility.FromStringPtr(a.ClientSecret),
		Organization: utility.FromStringPtr(a.Organization),
		DefaultOwner: utility.FromStringPtr(a.DefaultOwner),
		DefaultRepo:  utility.FromStringPtr(a.DefaultRepo),
		AppId:        a.AppId,
	}
	for _, u := range a.Users {
		config.Users = append(config.Users, utility.FromStringPtr(u))
	}
	return &config, nil
}

type APIMultiAuthConfig struct {
	ReadWrite []string `json:"read_write"`
	ReadOnly  []string `json:"read_only"`
}

func (a *APIMultiAuthConfig) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case *evergreen.MultiAuthConfig:
		if v == nil {
			return nil
		}
		a.ReadWrite = v.ReadWrite
		a.ReadOnly = v.ReadOnly
	default:
		return errors.Errorf("programmatic error: expected multi-auth config but got type %T", h)
	}
	return nil
}

func (a *APIMultiAuthConfig) ToService() (interface{}, error) {
	if a == nil {
		return nil, nil
	}
	return &evergreen.MultiAuthConfig{
		ReadWrite: a.ReadWrite,
		ReadOnly:  a.ReadOnly,
	}, nil
}

// APIBanner is a public structure representing the banner part of the admin settings
type APIBanner struct {
	Text  *string `json:"banner"`
	Theme *string `json:"theme"`
}

// APIUiV2URL is a public structure representing the new UI url (e.g. Spruce)
type APIUiV2URL struct {
	UIv2Url *string `json:"uiv2_url"`
}

type APIHostInitConfig struct {
	HostThrottle         int     `json:"host_throttle"`
	ProvisioningThrottle int     `json:"provisioning_throttle"`
	CloudStatusBatchSize int     `json:"cloud_batch_size"`
	MaxTotalDynamicHosts int     `json:"max_total_dynamic_hosts"`
	S3BaseURL            *string `json:"s3_base_url"`
}

func (a *APIHostInitConfig) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case evergreen.HostInitConfig:
		a.HostThrottle = v.HostThrottle
		a.ProvisioningThrottle = v.ProvisioningThrottle
		a.CloudStatusBatchSize = v.CloudStatusBatchSize
		a.MaxTotalDynamicHosts = v.MaxTotalDynamicHosts
		a.S3BaseURL = utility.ToStringPtr(v.S3BaseURL)
	default:
		return errors.Errorf("programmatic error: expected host init config but got type %T", h)
	}
	return nil
}

func (a *APIHostInitConfig) ToService() (interface{}, error) {
	return evergreen.HostInitConfig{
		HostThrottle:         a.HostThrottle,
		ProvisioningThrottle: a.ProvisioningThrottle,
		CloudStatusBatchSize: a.CloudStatusBatchSize,
		MaxTotalDynamicHosts: a.MaxTotalDynamicHosts,
		S3BaseURL:            utility.FromStringPtr(a.S3BaseURL),
	}, nil
}

type APIPodLifecycleConfig struct {
	S3BaseURL                   *string `json:"s3_base_url"`
	MaxParallelPodRequests      int     `json:"max_parallel_pod_requests"`
	MaxPodDefinitionCleanupRate int     `json:"max_pod_definition_cleanup_rate"`
	MaxSecretCleanupRate        int     `json:"max_secret_cleanup_rate"`
}

func (a *APIPodLifecycleConfig) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case evergreen.PodLifecycleConfig:
		a.S3BaseURL = utility.ToStringPtr(v.S3BaseURL)
		a.MaxParallelPodRequests = v.MaxParallelPodRequests
		a.MaxPodDefinitionCleanupRate = v.MaxPodDefinitionCleanupRate
		a.MaxSecretCleanupRate = v.MaxSecretCleanupRate
	default:
		return errors.Errorf("programmatic error: expected pod lifecycle config but got type %T", h)
	}
	return nil
}

func (a *APIPodLifecycleConfig) ToService() (interface{}, error) {
	return evergreen.PodLifecycleConfig{
		S3BaseURL:                   utility.FromStringPtr(a.S3BaseURL),
		MaxParallelPodRequests:      a.MaxParallelPodRequests,
		MaxPodDefinitionCleanupRate: a.MaxPodDefinitionCleanupRate,
		MaxSecretCleanupRate:        a.MaxSecretCleanupRate,
	}, nil
}

type APIJiraConfig struct {
	Host            *string           `json:"host"`
	DefaultProject  *string           `json:"default_project"`
	Email           *string           `json:"email"`
	BasicAuthConfig *APIJiraBasicAuth `json:"basic_auth"`
	OAuth1Config    *APIJiraOAuth1    `json:"oauth1"`
}

func (a *APIJiraConfig) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case evergreen.JiraConfig:
		a.Host = utility.ToStringPtr(v.Host)
		a.DefaultProject = utility.ToStringPtr(v.DefaultProject)
		a.Email = utility.ToStringPtr(v.Email)
		a.BasicAuthConfig = &APIJiraBasicAuth{}
		a.BasicAuthConfig.BuildFromService(v.BasicAuthConfig)
		a.OAuth1Config = &APIJiraOAuth1{}
		a.OAuth1Config.BuildFromService(v.OAuth1Config)
	default:
		return errors.Errorf("programmatic error: expected Jira config but got type %T", h)
	}
	return nil
}

func (a *APIJiraConfig) ToService() (interface{}, error) {
	c := evergreen.JiraConfig{
		Host:           utility.FromStringPtr(a.Host),
		DefaultProject: utility.FromStringPtr(a.DefaultProject),
		Email:          utility.FromStringPtr(a.Email),
	}
	if a.BasicAuthConfig != nil {
		c.BasicAuthConfig = a.BasicAuthConfig.ToService()
	}
	if a.OAuth1Config != nil {
		c.OAuth1Config = a.OAuth1Config.ToService()
	}
	return c, nil
}

type APIJiraBasicAuth struct {
	Username *string `json:"username"`
	Password *string `json:"password"`
}

func (a *APIJiraBasicAuth) BuildFromService(c evergreen.JiraBasicAuthConfig) {
	a.Username = utility.ToStringPtr(c.Username)
	a.Password = utility.ToStringPtr(c.Password)
}

func (a *APIJiraBasicAuth) ToService() evergreen.JiraBasicAuthConfig {
	return evergreen.JiraBasicAuthConfig{
		Username: utility.FromStringPtr(a.Username),
		Password: utility.FromStringPtr(a.Password),
	}
}

type APIJiraOAuth1 struct {
	PrivateKey  *string `json:"private_key"`
	AccessToken *string `json:"access_token"`
	TokenSecret *string `json:"token_secret"`
	ConsumerKey *string `json:"consumer_key"`
}

func (a *APIJiraOAuth1) BuildFromService(c evergreen.JiraOAuth1Config) {
	a.PrivateKey = utility.ToStringPtr(c.PrivateKey)
	a.AccessToken = utility.ToStringPtr(c.AccessToken)
	a.TokenSecret = utility.ToStringPtr(c.TokenSecret)
	a.ConsumerKey = utility.ToStringPtr(c.ConsumerKey)
}

func (a *APIJiraOAuth1) ToService() evergreen.JiraOAuth1Config {
	return evergreen.JiraOAuth1Config{
		PrivateKey:  utility.FromStringPtr(a.PrivateKey),
		AccessToken: utility.FromStringPtr(a.AccessToken),
		TokenSecret: utility.FromStringPtr(a.TokenSecret),
		ConsumerKey: utility.FromStringPtr(a.ConsumerKey),
	}
}

type APILDAPRoleMapping struct {
	LDAPGroup *string `json:"ldap_group"`
	RoleID    *string ` json:"role_id"`
}

func (a *APILDAPRoleMapping) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case evergreen.LDAPRoleMapping:
		a.LDAPGroup = utility.ToStringPtr(v.LDAPGroup)
		a.RoleID = utility.ToStringPtr(v.RoleID)
	}

	return nil
}

func (a *APILDAPRoleMapping) ToService() (interface{}, error) {
	mapping := evergreen.LDAPRoleMapping{
		LDAPGroup: utility.FromStringPtr(a.LDAPGroup),
		RoleID:    utility.FromStringPtr(a.RoleID),
	}

	return mapping, nil
}

type APILDAPRoleMap []APILDAPRoleMapping

func (a *APILDAPRoleMap) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case evergreen.LDAPRoleMap:
		m := make(APILDAPRoleMap, len(v))
		for i := range v {
			if err := m[i].BuildFromService(v[i]); err != nil {
				return errors.Wrapf(err, "converting LDAP role at index %d to API model", i)
			}
		}
	default:
		return errors.Errorf("programmatic error: expected LDAP role map but got type %T", h)
	}

	return nil
}

func (a *APILDAPRoleMap) ToService() (interface{}, error) {
	serviceMap := make(evergreen.LDAPRoleMap, len(*a))
	for i := range *a {
		v, err := (*a)[i].ToService()
		if err != nil {
			return nil, errors.Wrapf(err, "converting LDAP role mapping at index %d to service model", i)
		}
		roleMapping, ok := v.(evergreen.LDAPRoleMapping)
		if !ok {
			return nil, errors.Errorf("programmatic error: expected LDAP role mapping at index %d but got type %T", i, v)
		}
		serviceMap[i] = roleMapping
	}

	return serviceMap, nil
}

type APILoggerConfig struct {
	Buffer         *APILogBuffering `json:"buffer"`
	DefaultLevel   *string          `json:"default_level"`
	ThresholdLevel *string          `json:"threshold_level"`
	LogkeeperURL   *string          `json:"logkeeper_url"`
	DefaultLogger  *string          `json:"default_logger"`
}

func (a *APILoggerConfig) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case evergreen.LoggerConfig:
		a.DefaultLevel = utility.ToStringPtr(v.DefaultLevel)
		a.ThresholdLevel = utility.ToStringPtr(v.ThresholdLevel)
		a.LogkeeperURL = utility.ToStringPtr(v.LogkeeperURL)
		a.DefaultLogger = utility.ToStringPtr(v.DefaultLogger)
		a.Buffer = &APILogBuffering{}
		if err := a.Buffer.BuildFromService(v.Buffer); err != nil {
			return err
		}
	default:
		return errors.Errorf("programmatic error: expected task logging config but got type %T", h)
	}
	return nil
}

func (a *APILoggerConfig) ToService() (interface{}, error) {
	config := evergreen.LoggerConfig{
		DefaultLevel:   utility.FromStringPtr(a.DefaultLevel),
		ThresholdLevel: utility.FromStringPtr(a.ThresholdLevel),
		LogkeeperURL:   utility.FromStringPtr(a.LogkeeperURL),
		DefaultLogger:  utility.FromStringPtr(a.DefaultLogger),
	}
	i, err := a.Buffer.ToService()
	if err != nil {
		return nil, err
	}
	buffer := i.(evergreen.LogBuffering)
	config.Buffer = buffer
	return config, nil
}

type APILogBuffering struct {
	UseAsync             bool `json:"use_async"`
	DurationSeconds      int  `json:"duration_seconds"`
	Count                int  `json:"count"`
	IncomingBufferFactor int  `json:"incoming_buffer_factor"`
}

func (a *APILogBuffering) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case evergreen.LogBuffering:
		a.UseAsync = v.UseAsync
		a.DurationSeconds = v.DurationSeconds
		a.Count = v.Count
		a.IncomingBufferFactor = v.IncomingBufferFactor
	default:
		return errors.Errorf("programmatic error: expected logging buffer config but got type %T", h)
	}
	return nil
}

func (a *APILogBuffering) ToService() (interface{}, error) {
	return evergreen.LogBuffering{
		UseAsync:             a.UseAsync,
		DurationSeconds:      a.DurationSeconds,
		Count:                a.Count,
		IncomingBufferFactor: a.IncomingBufferFactor,
	}, nil
}

type APINotifyConfig struct {
	BufferTargetPerInterval int          `json:"buffer_target_per_interval"`
	BufferIntervalSeconds   int          `json:"buffer_interval_seconds"`
	SES                     APISESConfig `json:"ses"`
}

func (a *APINotifyConfig) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case evergreen.NotifyConfig:
		a.SES = APISESConfig{}
		if err := a.SES.BuildFromService(v.SES); err != nil {
			return err
		}
		a.BufferTargetPerInterval = v.BufferTargetPerInterval
		a.BufferIntervalSeconds = v.BufferIntervalSeconds
	default:
		return errors.Errorf("programmatic error: expected notify config but got type %T", h)
	}
	return nil
}

func (a *APINotifyConfig) ToService() (interface{}, error) {
	ses, err := a.SES.ToService()
	if err != nil {
		return nil, err
	}

	return evergreen.NotifyConfig{
		BufferTargetPerInterval: a.BufferTargetPerInterval,
		BufferIntervalSeconds:   a.BufferIntervalSeconds,
		SES:                     ses.(evergreen.SESConfig),
	}, nil
}

type APIOwnerRepo struct {
	Owner *string `json:"owner"`
	Repo  *string `json:"repo"`
}

func (a *APIOwnerRepo) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case evergreen.OwnerRepo:
		a.Owner = utility.ToStringPtr(v.Owner)
		a.Repo = utility.ToStringPtr(v.Repo)
	default:
		return errors.Errorf("programmatic error: expected owner and repo config but got type %T", h)
	}
	return nil
}

func (a *APIOwnerRepo) ToService() (interface{}, error) {
	res := evergreen.OwnerRepo{}
	res.Owner = utility.FromStringPtr(a.Owner)
	res.Repo = utility.FromStringPtr(a.Repo)
	return res, nil
}

type APIProjectCreationConfig struct {
	TotalProjectLimit int            `json:"total_project_limit"`
	RepoProjectLimit  int            `json:"repo_project_limit"`
	RepoExceptions    []APIOwnerRepo `json:"repo_exceptions"`
	JiraProject       string         `json:"jira_project"`
}

func (a *APIProjectCreationConfig) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case evergreen.ProjectCreationConfig:
		for _, ownerRepo := range v.RepoExceptions {
			apiOwnerRepo := APIOwnerRepo{}
			if err := apiOwnerRepo.BuildFromService(ownerRepo); err != nil {
				return err
			}
			a.RepoExceptions = append(a.RepoExceptions, apiOwnerRepo)
		}
		a.TotalProjectLimit = v.TotalProjectLimit
		a.RepoProjectLimit = v.RepoProjectLimit
		a.JiraProject = v.JiraProject
	default:
		return errors.Errorf("programmatic error: expected Project Creation config but got type %T", h)
	}

	return nil
}

func (a *APIProjectCreationConfig) ToService() (interface{}, error) {
	if a == nil {
		return nil, nil
	}

	config := evergreen.ProjectCreationConfig{
		TotalProjectLimit: a.TotalProjectLimit,
		RepoProjectLimit:  a.RepoProjectLimit,
		JiraProject:       a.JiraProject,
	}

	for _, r := range a.RepoExceptions {
		i, err := r.ToService()
		if err != nil {
			return nil, err
		}
		ownerRepo, ok := i.(evergreen.OwnerRepo)
		if !ok {
			return nil, errors.Errorf("programmatic error: expected owner and repo but got type %T", i)
		}
		config.RepoExceptions = append(config.RepoExceptions, ownerRepo)
	}

	return config, nil
}

type APICloudProviders struct {
	AWS       *APIAWSConfig       `json:"aws"`
	Docker    *APIDockerConfig    `json:"docker"`
	GCE       *APIGCEConfig       `json:"gce"`
	OpenStack *APIOpenStackConfig `json:"openstack"`
	VSphere   *APIVSphereConfig   `json:"vsphere"`
}

func (a *APICloudProviders) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case evergreen.CloudProviders:
		a.AWS = &APIAWSConfig{}
		a.Docker = &APIDockerConfig{}
		a.GCE = &APIGCEConfig{}
		a.OpenStack = &APIOpenStackConfig{}
		a.VSphere = &APIVSphereConfig{}
		if err := a.AWS.BuildFromService(v.AWS); err != nil {
			return err
		}
		if err := a.Docker.BuildFromService(v.Docker); err != nil {
			return err
		}
		if err := a.GCE.BuildFromService(v.GCE); err != nil {
			return err
		}
		if err := a.OpenStack.BuildFromService(v.OpenStack); err != nil {
			return err
		}
		if err := a.VSphere.BuildFromService(v.VSphere); err != nil {
			return err
		}
	default:
		return errors.Errorf("programmatic error: expected cloud provider config but got type %T", h)
	}
	return nil
}

func (a *APICloudProviders) ToService() (interface{}, error) {
	aws, err := a.AWS.ToService()
	if err != nil {
		return nil, err
	}
	docker, err := a.Docker.ToService()
	if err != nil {
		return nil, err
	}
	gce, err := a.GCE.ToService()
	if err != nil {
		return nil, err
	}
	openstack, err := a.OpenStack.ToService()
	if err != nil {
		return nil, err
	}
	vsphere, err := a.VSphere.ToService()
	if err != nil {
		return nil, err
	}
	return evergreen.CloudProviders{
		AWS:       aws.(evergreen.AWSConfig),
		Docker:    docker.(evergreen.DockerConfig),
		GCE:       gce.(evergreen.GCEConfig),
		OpenStack: openstack.(evergreen.OpenStackConfig),
		VSphere:   vsphere.(evergreen.VSphereConfig),
	}, nil
}

type APICommitQueueConfig struct {
	MergeTaskDistro            *string `json:"merge_task_distro"`
	CommitterName              *string `json:"committer_name"`
	CommitterEmail             *string `json:"committer_email"`
	BatchSize                  int     `json:"batch_size"`
	MaxSystemFailedTaskRetries int     `json:"max_system_failed_task_retries"`
}

func (a *APICommitQueueConfig) BuildFromService(h interface{}) error {
	if v, ok := h.(evergreen.CommitQueueConfig); ok {
		a.MergeTaskDistro = utility.ToStringPtr(v.MergeTaskDistro)
		a.CommitterName = utility.ToStringPtr(v.CommitterName)
		a.CommitterEmail = utility.ToStringPtr(v.CommitterEmail)
		a.BatchSize = v.BatchSize
		a.MaxSystemFailedTaskRetries = v.MaxSystemFailedTaskRetries

		return nil
	}

	return errors.Errorf("programmatic error: expected commit queue config but got type %T", h)
}

func (a *APICommitQueueConfig) ToService() (interface{}, error) {
	return evergreen.CommitQueueConfig{
		MergeTaskDistro:            utility.FromStringPtr(a.MergeTaskDistro),
		CommitterName:              utility.FromStringPtr(a.CommitterName),
		CommitterEmail:             utility.FromStringPtr(a.CommitterEmail),
		BatchSize:                  a.BatchSize,
		MaxSystemFailedTaskRetries: a.MaxSystemFailedTaskRetries,
	}, nil
}

type APIContainerPoolsConfig struct {
	Pools []APIContainerPool `json:"pools"`
}

func (a *APIContainerPoolsConfig) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case evergreen.ContainerPoolsConfig:
		for _, pool := range v.Pools {
			apiPool := APIContainerPool{}
			if err := apiPool.BuildFromService(pool); err != nil {
				return err
			}
			a.Pools = append(a.Pools, apiPool)
		}
	default:
		return errors.Errorf("programmatic error: expected container pools config but got type %T", h)
	}
	return nil
}

func (a *APIContainerPoolsConfig) ToService() (interface{}, error) {
	if a == nil {
		return nil, nil
	}
	config := evergreen.ContainerPoolsConfig{}
	for _, p := range a.Pools {
		i, err := p.ToService()
		if err != nil {
			return nil, err
		}
		pool := i.(evergreen.ContainerPool)
		config.Pools = append(config.Pools, pool)
	}
	return config, nil
}

type APIContainerPool struct {
	Distro        *string `json:"distro"`
	Id            *string `json:"id"`
	MaxContainers int     `json:"max_containers"`
	Port          uint16  `json:"port"`
}

func (a *APIContainerPool) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case evergreen.ContainerPool:
		a.Distro = utility.ToStringPtr(v.Distro)
		a.Id = utility.ToStringPtr(v.Id)
		a.MaxContainers = v.MaxContainers
		a.Port = v.Port
	default:
		return errors.Errorf("programmatic error: expected container pool config but got type %T", h)
	}
	return nil
}

func (a *APIContainerPool) ToService() (interface{}, error) {
	return evergreen.ContainerPool{
		Distro:        utility.FromStringPtr(a.Distro),
		Id:            utility.FromStringPtr(a.Id),
		MaxContainers: a.MaxContainers,
		Port:          a.Port,
	}, nil
}

type APIEC2Key struct {
	Name   *string `json:"name"`
	Region *string `json:"region"`
	Key    *string `json:"key"`
	Secret *string `json:"secret"`
}

func (a *APIEC2Key) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case evergreen.EC2Key:
		a.Name = utility.ToStringPtr(v.Name)
		a.Region = utility.ToStringPtr(v.Region)
		a.Key = utility.ToStringPtr(v.Key)
		a.Secret = utility.ToStringPtr(v.Secret)
	default:
		return errors.Errorf("programmatic error: expected EC2 key config but got type %T", h)
	}
	return nil
}

func (a *APIEC2Key) ToService() (interface{}, error) {
	res := evergreen.EC2Key{}
	res.Name = utility.FromStringPtr(a.Name)
	res.Region = utility.FromStringPtr(a.Region)
	res.Key = utility.FromStringPtr(a.Key)
	res.Secret = utility.FromStringPtr(a.Secret)
	return res, nil
}

type APISubnet struct {
	AZ       *string `json:"az"`
	SubnetID *string `json:"subnet_id"`
}

func (a *APISubnet) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case evergreen.Subnet:
		a.AZ = utility.ToStringPtr(v.AZ)
		a.SubnetID = utility.ToStringPtr(v.SubnetID)
	default:
		return errors.Errorf("programmatic error: expected subnet config but got type %T", h)
	}
	return nil
}

func (a *APISubnet) ToService() (interface{}, error) {
	res := evergreen.Subnet{}
	res.AZ = utility.FromStringPtr(a.AZ)
	res.SubnetID = utility.FromStringPtr(a.SubnetID)
	return res, nil
}

type APIAWSConfig struct {
	EC2Keys              []APIEC2Key               `json:"ec2_keys"`
	Subnets              []APISubnet               `json:"subnets"`
	TaskSync             *APIS3Credentials         `json:"task_sync"`
	TaskSyncRead         *APIS3Credentials         `json:"task_sync_read"`
	ParserProject        *APIParserProjectS3Config `json:"parser_project"`
	DefaultSecurityGroup *string                   `json:"default_security_group"`
	AllowedInstanceTypes []*string                 `json:"allowed_instance_types"`
	AllowedRegions       []*string                 `json:"allowed_regions"`
	MaxVolumeSizePerUser *int                      `json:"max_volume_size"`
	Pod                  *APIAWSPodConfig          `json:"pod"`
}

func (a *APIAWSConfig) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case evergreen.AWSConfig:
		for _, key := range v.EC2Keys {
			apiKey := APIEC2Key{}
			if err := apiKey.BuildFromService(key); err != nil {
				return err
			}
			a.EC2Keys = append(a.EC2Keys, apiKey)
		}

		for _, subnet := range v.Subnets {
			apiSubnet := APISubnet{}
			if err := apiSubnet.BuildFromService(subnet); err != nil {
				return err
			}
			a.Subnets = append(a.Subnets, apiSubnet)
		}

		taskSync := &APIS3Credentials{}
		if err := taskSync.BuildFromService(v.TaskSync); err != nil {
			return errors.Wrap(err, "converting S3 credentials to API model")
		}
		a.TaskSync = taskSync

		taskSyncRead := &APIS3Credentials{}
		if err := taskSyncRead.BuildFromService(v.TaskSyncRead); err != nil {
			return errors.Wrap(err, "converting S3 credentials to API model")
		}
		a.TaskSyncRead = taskSyncRead

		parserProject := &APIParserProjectS3Config{}
		if err := parserProject.BuildFromService(v.ParserProject); err != nil {
			return errors.Wrap(err, "converting parser project S3 config to API model")
		}
		a.ParserProject = parserProject

		a.DefaultSecurityGroup = utility.ToStringPtr(v.DefaultSecurityGroup)
		a.MaxVolumeSizePerUser = &v.MaxVolumeSizePerUser
		a.AllowedInstanceTypes = utility.ToStringPtrSlice(v.AllowedInstanceTypes)
		a.AllowedRegions = utility.ToStringPtrSlice(v.AllowedRegions)

		var pod APIAWSPodConfig
		pod.BuildFromService(v.Pod)
		a.Pod = &pod
	default:
		return errors.Errorf("programmatic error: expected AWS config but got type %T", h)
	}
	return nil
}

func (a *APIAWSConfig) ToService() (interface{}, error) {
	if a == nil {
		return nil, nil
	}
	config := evergreen.AWSConfig{
		DefaultSecurityGroup: utility.FromStringPtr(a.DefaultSecurityGroup),
		MaxVolumeSizePerUser: evergreen.DefaultMaxVolumeSizePerUser,
	}

	var i interface{}
	var err error
	var ok bool

	i, err = a.TaskSync.ToService()
	if err != nil {
		return nil, errors.Wrap(err, "converting S3 credentials to service model")
	}
	var taskSync evergreen.S3Credentials
	if i != nil {
		taskSync, ok = i.(evergreen.S3Credentials)
		if !ok {
			return nil, errors.Errorf("expecting S3 credentials but got type %T", i)
		}
	}
	config.TaskSync = taskSync

	i, err = a.TaskSyncRead.ToService()
	if err != nil {
		return nil, errors.Wrap(err, "converting S3 credentials to service model")
	}
	var taskSyncRead evergreen.S3Credentials
	if i != nil {
		taskSyncRead, ok = i.(evergreen.S3Credentials)
		if !ok {
			return nil, errors.Errorf("programmatic error: expected S3 credentials but got type %T", i)
		}
	}
	config.TaskSyncRead = taskSyncRead

	i, err = a.ParserProject.ToService()
	if err != nil {
		return nil, errors.Wrap(err, "converting parser project S3 credentials to service model")
	}
	var parserProject evergreen.ParserProjectS3Config
	if i != nil {
		parserProject, ok = i.(evergreen.ParserProjectS3Config)
		if !ok {
			return nil, errors.Errorf("programmatic error: expected parser project S3 config but got type %T", i)
		}
	}
	config.ParserProject = parserProject

	if a.MaxVolumeSizePerUser != nil {
		config.MaxVolumeSizePerUser = *a.MaxVolumeSizePerUser
	}

	for _, k := range a.EC2Keys {
		i, err := k.ToService()
		if err != nil {
			return nil, err
		}
		key, ok := i.(evergreen.EC2Key)
		if !ok {
			return nil, errors.Errorf("programmatic error: expected EC2 key but got type %T", i)
		}
		config.EC2Keys = append(config.EC2Keys, key)
	}

	for _, s := range a.Subnets {
		i, err := s.ToService()
		if err != nil {
			return nil, err
		}
		subnet, ok := i.(evergreen.Subnet)
		if !ok {
			return nil, errors.Errorf("programmatic error: expected EC2 subnet but got type %T", i)
		}
		config.Subnets = append(config.Subnets, subnet)
	}

	config.AllowedInstanceTypes = utility.FromStringPtrSlice(a.AllowedInstanceTypes)
	config.AllowedRegions = utility.FromStringPtrSlice(a.AllowedRegions)

	pod, err := a.Pod.ToService()
	if err != nil {
		return nil, errors.Wrap(err, "converting ECS configuration to service model")
	}
	config.Pod = *pod

	return config, nil
}

type APIS3Credentials struct {
	Key    *string `json:"key"`
	Secret *string `json:"secret"`
	Bucket *string `json:"bucket"`
}

func (a *APIS3Credentials) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case evergreen.S3Credentials:
		a.Key = utility.ToStringPtr(v.Key)
		a.Secret = utility.ToStringPtr(v.Secret)
		a.Bucket = utility.ToStringPtr(v.Bucket)
		return nil
	default:
		return errors.Errorf("programmatic error: expected S3 credentials but got type %T", h)
	}
}

func (a *APIS3Credentials) ToService() (interface{}, error) {
	if a == nil {
		return nil, nil
	}
	return evergreen.S3Credentials{
		Key:    utility.FromStringPtr(a.Key),
		Secret: utility.FromStringPtr(a.Secret),
		Bucket: utility.FromStringPtr(a.Bucket),
	}, nil
}

// APIParserProjectS3Config represents configuration options for storing and
// accessing parser projects in S3.
type APIParserProjectS3Config struct {
	APIS3Credentials
	Prefix *string `json:"prefix"`
}

func (a *APIParserProjectS3Config) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case evergreen.ParserProjectS3Config:
		a.Key = utility.ToStringPtr(v.Key)
		a.Secret = utility.ToStringPtr(v.Secret)
		a.Bucket = utility.ToStringPtr(v.Bucket)
		a.Prefix = utility.ToStringPtr(v.Prefix)
		return nil
	default:
		return errors.Errorf("programmatic error: expected parser project S3 config but got type %T", h)
	}
}

func (a *APIParserProjectS3Config) ToService() (interface{}, error) {
	if a == nil {
		return nil, nil
	}
	return evergreen.ParserProjectS3Config{
		S3Credentials: evergreen.S3Credentials{
			Key:    utility.FromStringPtr(a.Key),
			Secret: utility.FromStringPtr(a.Secret),
			Bucket: utility.FromStringPtr(a.Bucket),
		},
		Prefix: utility.FromStringPtr(a.Prefix),
	}, nil
}

// APIAWSPodConfig represents configuration options for pods running in AWS.
type APIAWSPodConfig struct {
	Role           *string                  `json:"role"`
	Region         *string                  `json:"region"`
	ECS            *APIECSConfig            `json:"ecs"`
	SecretsManager *APISecretsManagerConfig `json:"secrets_manager"`
}

func (a *APIAWSPodConfig) BuildFromService(conf evergreen.AWSPodConfig) {
	a.Role = utility.ToStringPtr(conf.Role)
	a.Region = utility.ToStringPtr(conf.Region)
	var apiECS APIECSConfig
	apiECS.BuildFromService(conf.ECS)
	a.ECS = &apiECS
	var apiSecretsManager APISecretsManagerConfig
	apiSecretsManager.BuildFromService(conf.SecretsManager)
	a.SecretsManager = &apiSecretsManager
}

func (a *APIAWSPodConfig) ToService() (*evergreen.AWSPodConfig, error) {
	if a == nil {
		return nil, nil
	}

	ecs, err := a.ECS.ToService()
	if err != nil {
		return nil, errors.Wrap(err, "converting ECS config to service model")
	}

	sm := a.SecretsManager.ToService()

	return &evergreen.AWSPodConfig{
		Role:           utility.FromStringPtr(a.Role),
		Region:         utility.FromStringPtr(a.Region),
		ECS:            *ecs,
		SecretsManager: sm,
	}, nil
}

// APIECSConfig represents configuration options for AWS ECS.
type APIECSConfig struct {
	MaxCPU               *int                     `json:"max_cpu"`
	MaxMemoryMB          *int                     `json:"max_memory_mb"`
	TaskDefinitionPrefix *string                  `json:"task_definition_prefix"`
	TaskRole             *string                  `json:"task_role"`
	ExecutionRole        *string                  `json:"execution_role"`
	LogRegion            *string                  `json:"log_region"`
	LogGroup             *string                  `json:"log_group"`
	LogStreamPrefix      *string                  `json:"log_stream_prefix"`
	AWSVPC               *APIAWSVPCConfig         `json:"awsvpc"`
	Clusters             []APIECSClusterConfig    `json:"clusters"`
	CapacityProviders    []APIECSCapacityProvider `json:"capacity_providers"`
	AllowedImages        []string                 `json:"allowed_images"`
}

func (a *APIECSConfig) BuildFromService(conf evergreen.ECSConfig) {
	a.MaxCPU = utility.ToIntPtr(conf.MaxCPU)
	a.MaxMemoryMB = utility.ToIntPtr(conf.MaxMemoryMB)
	a.TaskDefinitionPrefix = utility.ToStringPtr(conf.TaskDefinitionPrefix)
	a.TaskRole = utility.ToStringPtr(conf.TaskRole)
	a.ExecutionRole = utility.ToStringPtr(conf.ExecutionRole)
	a.LogRegion = utility.ToStringPtr(conf.LogRegion)
	a.LogStreamPrefix = utility.ToStringPtr(conf.LogStreamPrefix)
	a.LogGroup = utility.ToStringPtr(conf.LogGroup)
	var apiAWSVPC APIAWSVPCConfig
	apiAWSVPC.BuildFromService(conf.AWSVPC)
	a.AWSVPC = &apiAWSVPC
	for _, cluster := range conf.Clusters {
		var apiCluster APIECSClusterConfig
		apiCluster.BuildFromService(cluster)
		a.Clusters = append(a.Clusters, apiCluster)
	}
	for _, cp := range conf.CapacityProviders {
		var apiProvider APIECSCapacityProvider
		apiProvider.BuildFromService(cp)
		a.CapacityProviders = append(a.CapacityProviders, apiProvider)
	}
	a.AllowedImages = conf.AllowedImages
}

func (a *APIECSConfig) ToService() (*evergreen.ECSConfig, error) {
	if a == nil {
		return nil, nil
	}

	var clusters []evergreen.ECSClusterConfig
	for _, apiCluster := range a.Clusters {
		cluster, err := apiCluster.ToService()
		if err != nil {
			return nil, errors.Wrap(err, "converting ECS cluster config to service model")
		}
		clusters = append(clusters, *cluster)
	}
	var providers []evergreen.ECSCapacityProvider
	for _, apiProvider := range a.CapacityProviders {
		cp, err := apiProvider.ToService()
		if err != nil {
			return nil, errors.Wrap(err, "converting capacity provider to service model")
		}
		providers = append(providers, *cp)
	}

	return &evergreen.ECSConfig{
		MaxCPU:               utility.FromIntPtr(a.MaxCPU),
		MaxMemoryMB:          utility.FromIntPtr(a.MaxMemoryMB),
		TaskDefinitionPrefix: utility.FromStringPtr(a.TaskDefinitionPrefix),
		TaskRole:             utility.FromStringPtr(a.TaskRole),
		ExecutionRole:        utility.FromStringPtr(a.ExecutionRole),
		LogRegion:            utility.FromStringPtr(a.LogRegion),
		LogStreamPrefix:      utility.FromStringPtr(a.LogStreamPrefix),
		LogGroup:             utility.FromStringPtr(a.LogGroup),
		AWSVPC:               a.AWSVPC.ToService(),
		Clusters:             clusters,
		CapacityProviders:    providers,
		AllowedImages:        a.AllowedImages,
	}, nil
}

// APIAWSVPCConfig represents configuration options for tasks in ECS using
// AWSVPC networking.
type APIAWSVPCConfig struct {
	Subnets        []string `json:"subnets,omitempty"`
	SecurityGroups []string `json:"security_groups,omitempty"`
}

func (a *APIAWSVPCConfig) BuildFromService(conf evergreen.AWSVPCConfig) {
	a.Subnets = conf.Subnets
	a.SecurityGroups = conf.SecurityGroups
}

func (a *APIAWSVPCConfig) ToService() evergreen.AWSVPCConfig {
	if a == nil {
		return evergreen.AWSVPCConfig{}
	}
	return evergreen.AWSVPCConfig{
		Subnets:        a.Subnets,
		SecurityGroups: a.SecurityGroups,
	}
}

// APIECSClusterConfig represents configuration options for a cluster in AWS
// ECS.
type APIECSClusterConfig struct {
	Name *string `json:"name"`
	OS   *string `json:"os"`
}

func (a *APIECSClusterConfig) BuildFromService(conf evergreen.ECSClusterConfig) {
	a.Name = utility.ToStringPtr(conf.Name)
	a.OS = utility.ToStringPtr(string(conf.OS))
}

func (a *APIECSClusterConfig) ToService() (*evergreen.ECSClusterConfig, error) {
	if a == nil {
		return nil, nil
	}
	os := evergreen.ECSOS(utility.FromStringPtr(a.OS))
	if err := os.Validate(); err != nil {
		return nil, errors.Wrap(err, "invalid OS")
	}
	return &evergreen.ECSClusterConfig{
		Name: utility.FromStringPtr(a.Name),
		OS:   os,
	}, nil
}

// APIECSCapacityProvider represents configuration options for a capacity
// provider within an ECS cluster.
type APIECSCapacityProvider struct {
	Name           *string `json:"name"`
	OS             *string `json:"os"`
	Arch           *string `json:"arch"`
	WindowsVersion *string `json:"windows_version"`
}

func (a *APIECSCapacityProvider) BuildFromService(cp evergreen.ECSCapacityProvider) {
	a.Name = utility.ToStringPtr(cp.Name)
	a.OS = utility.ToStringPtr(string(cp.OS))
	a.Arch = utility.ToStringPtr(string(cp.Arch))
	a.WindowsVersion = utility.ToStringPtr(string(cp.WindowsVersion))
}

func (a *APIECSCapacityProvider) ToService() (*evergreen.ECSCapacityProvider, error) {
	os := evergreen.ECSOS(utility.FromStringPtr(a.OS))
	if err := os.Validate(); err != nil {
		return nil, errors.Wrap(err, "invalid OS")
	}
	arch := evergreen.ECSArch(utility.FromStringPtr(a.Arch))
	if err := arch.Validate(); err != nil {
		return nil, errors.Wrap(err, "invalid arch")
	}
	winVer := evergreen.ECSWindowsVersion(utility.FromStringPtr(a.WindowsVersion))
	if winVer != "" {
		if err := winVer.Validate(); err != nil {
			return nil, errors.Wrap(err, "invalid Windows version")
		}
	}
	return &evergreen.ECSCapacityProvider{
		Name:           utility.FromStringPtr(a.Name),
		OS:             os,
		Arch:           arch,
		WindowsVersion: winVer,
	}, nil
}

// APISecretsManagerConfig represents configuration options for AWS Secrets
// Manager.
type APISecretsManagerConfig struct {
	SecretPrefix *string `json:"secret_prefix"`
}

func (a *APISecretsManagerConfig) BuildFromService(conf evergreen.SecretsManagerConfig) {
	a.SecretPrefix = utility.ToStringPtr(conf.SecretPrefix)
}

func (a *APISecretsManagerConfig) ToService() evergreen.SecretsManagerConfig {
	if a == nil {
		return evergreen.SecretsManagerConfig{}
	}
	return evergreen.SecretsManagerConfig{
		SecretPrefix: utility.FromStringPtr(a.SecretPrefix),
	}
}

type APIDockerConfig struct {
	APIVersion    *string `json:"api_version"`
	DefaultDistro *string `json:"default_distro"`
}

func (a *APIDockerConfig) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case evergreen.DockerConfig:
		a.APIVersion = utility.ToStringPtr(v.APIVersion)
		a.DefaultDistro = utility.ToStringPtr(v.DefaultDistro)
	default:
		return errors.Errorf("programmatic error: expected Docker config but got type %T", h)
	}
	return nil
}

func (a *APIDockerConfig) ToService() (interface{}, error) {
	return evergreen.DockerConfig{
		APIVersion:    utility.FromStringPtr(a.APIVersion),
		DefaultDistro: utility.FromStringPtr(a.DefaultDistro),
	}, nil
}

type APIGCEConfig struct {
	ClientEmail  *string `json:"client_email"`
	PrivateKey   *string `json:"private_key"`
	PrivateKeyID *string `json:"private_key_id"`
	TokenURI     *string `json:"token_uri"`
}

func (a *APIGCEConfig) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case evergreen.GCEConfig:
		a.ClientEmail = utility.ToStringPtr(v.ClientEmail)
		a.PrivateKey = utility.ToStringPtr(v.PrivateKey)
		a.PrivateKeyID = utility.ToStringPtr(v.PrivateKeyID)
		a.TokenURI = utility.ToStringPtr(v.TokenURI)
	default:
		return errors.Errorf("programmatic error: expected GCE config but got type %T", h)
	}
	return nil
}

func (a *APIGCEConfig) ToService() (interface{}, error) {
	return evergreen.GCEConfig{
		ClientEmail:  utility.FromStringPtr(a.ClientEmail),
		PrivateKey:   utility.FromStringPtr(a.PrivateKey),
		PrivateKeyID: utility.FromStringPtr(a.PrivateKeyID),
		TokenURI:     utility.FromStringPtr(a.TokenURI),
	}, nil
}

type APIOpenStackConfig struct {
	IdentityEndpoint *string `json:"identity_endpoint"`

	Username   *string `json:"username"`
	Password   *string `json:"password"`
	DomainName *string `json:"domain_name"`

	ProjectName *string `json:"project_name"`
	ProjectID   *string `json:"project_id"`

	Region *string `json:"region"`
}

func (a *APIOpenStackConfig) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case evergreen.OpenStackConfig:
		a.IdentityEndpoint = utility.ToStringPtr(v.IdentityEndpoint)
		a.Username = utility.ToStringPtr(v.Username)
		a.Password = utility.ToStringPtr(v.Password)
		a.DomainName = utility.ToStringPtr(v.DomainName)
		a.ProjectName = utility.ToStringPtr(v.ProjectName)
		a.ProjectID = utility.ToStringPtr(v.ProjectID)
		a.Region = utility.ToStringPtr(v.Region)
	default:
		return errors.Errorf("programmatic error: expected OpenStack config but got type %T", h)
	}
	return nil
}

func (a *APIOpenStackConfig) ToService() (interface{}, error) {
	return evergreen.OpenStackConfig{
		IdentityEndpoint: utility.FromStringPtr(a.IdentityEndpoint),
		Username:         utility.FromStringPtr(a.Username),
		Password:         utility.FromStringPtr(a.Password),
		DomainName:       utility.FromStringPtr(a.DomainName),
		ProjectID:        utility.FromStringPtr(a.ProjectID),
		ProjectName:      utility.FromStringPtr(a.ProjectName),
		Region:           utility.FromStringPtr(a.Region),
	}, nil
}

type APIVSphereConfig struct {
	Host     *string `json:"host"`
	Username *string `json:"username"`
	Password *string `json:"password"`
}

func (a *APIVSphereConfig) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case evergreen.VSphereConfig:
		a.Host = utility.ToStringPtr(v.Host)
		a.Username = utility.ToStringPtr(v.Username)
		a.Password = utility.ToStringPtr(v.Password)
	default:
		return errors.Errorf("programmatic error: expected VSphere config but got type %T", h)
	}
	return nil
}

func (a *APIVSphereConfig) ToService() (interface{}, error) {
	return evergreen.VSphereConfig{
		Host:     utility.FromStringPtr(a.Host),
		Username: utility.FromStringPtr(a.Username),
		Password: utility.FromStringPtr(a.Password),
	}, nil
}

type APIRepoTrackerConfig struct {
	NumNewRepoRevisionsToFetch int `json:"revs_to_fetch"`
	MaxRepoRevisionsToSearch   int `json:"max_revs_to_search"`
	MaxConcurrentRequests      int `json:"max_con_requests"`
}

func (a *APIRepoTrackerConfig) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case evergreen.RepoTrackerConfig:
		a.NumNewRepoRevisionsToFetch = v.NumNewRepoRevisionsToFetch
		a.MaxConcurrentRequests = v.MaxConcurrentRequests
		a.MaxRepoRevisionsToSearch = v.MaxRepoRevisionsToSearch
	default:
		return errors.Errorf("programmatic error: expected repotracker config but got type %T", h)
	}
	return nil
}

func (a *APIRepoTrackerConfig) ToService() (interface{}, error) {
	return evergreen.RepoTrackerConfig{
		NumNewRepoRevisionsToFetch: a.NumNewRepoRevisionsToFetch,
		MaxConcurrentRequests:      a.MaxConcurrentRequests,
		MaxRepoRevisionsToSearch:   a.MaxRepoRevisionsToSearch,
	}, nil
}

type APISchedulerConfig struct {
	TaskFinder                    *string `json:"task_finder"`
	HostAllocator                 *string `json:"host_allocator"`
	HostAllocatorRoundingRule     *string `json:"host_allocator_rounding_rule"`
	HostAllocatorFeedbackRule     *string `json:"host_allocator_feedback_rule"`
	HostsOverallocatedRule        *string `json:"hosts_overallocated_rule"`
	FutureHostFraction            float64 `json:"free_host_fraction"`
	CacheDurationSeconds          int     `json:"cache_duration_seconds"`
	Planner                       *string `json:"planner"`
	TargetTimeSeconds             int     `json:"target_time_seconds"`
	AcceptableHostIdleTimeSeconds int     `json:"acceptable_host_idle_time_seconds"`
	GroupVersions                 bool    `json:"group_versions"`
	PatchFactor                   int64   `json:"patch_factor"`
	PatchTimeInQueueFactor        int64   `json:"patch_time_in_queue_factor"`
	CommitQueueFactor             int64   `json:"commit_queue_factor"`
	MainlineTimeInQueueFactor     int64   `json:"mainline_time_in_queue_factor"`
	ExpectedRuntimeFactor         int64   `json:"expected_runtime_factor"`
	GenerateTaskFactor            int64   `json:"generate_task_factor"`
	StepbackTaskFactor            int64   `json:"stepback_task_factor"`
}

func (a *APISchedulerConfig) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case evergreen.SchedulerConfig:
		a.TaskFinder = utility.ToStringPtr(v.TaskFinder)
		a.HostAllocator = utility.ToStringPtr(v.HostAllocator)
		a.HostAllocatorFeedbackRule = utility.ToStringPtr(v.HostAllocatorFeedbackRule)
		a.HostsOverallocatedRule = utility.ToStringPtr(v.HostsOverallocatedRule)
		a.FutureHostFraction = v.FutureHostFraction
		a.CacheDurationSeconds = v.CacheDurationSeconds
		a.Planner = utility.ToStringPtr(v.Planner)
		a.TargetTimeSeconds = v.TargetTimeSeconds
		a.AcceptableHostIdleTimeSeconds = v.AcceptableHostIdleTimeSeconds
		a.GroupVersions = v.GroupVersions
		a.PatchFactor = v.PatchFactor
		a.PatchTimeInQueueFactor = v.PatchTimeInQueueFactor
		a.CommitQueueFactor = v.CommitQueueFactor
		a.MainlineTimeInQueueFactor = v.MainlineTimeInQueueFactor
		a.ExpectedRuntimeFactor = v.ExpectedRuntimeFactor
		a.GenerateTaskFactor = v.GenerateTaskFactor
		a.StepbackTaskFactor = v.StepbackTaskFactor
	default:
		return errors.Errorf("programmatic error: expected host scheduler config but got type %T", h)
	}
	return nil
}

func (a *APISchedulerConfig) ToService() (interface{}, error) {
	return evergreen.SchedulerConfig{
		TaskFinder:                    utility.FromStringPtr(a.TaskFinder),
		HostAllocator:                 utility.FromStringPtr(a.HostAllocator),
		HostAllocatorFeedbackRule:     utility.FromStringPtr(a.HostAllocatorFeedbackRule),
		HostsOverallocatedRule:        utility.FromStringPtr(a.HostsOverallocatedRule),
		FutureHostFraction:            a.FutureHostFraction,
		CacheDurationSeconds:          a.CacheDurationSeconds,
		Planner:                       utility.FromStringPtr(a.Planner),
		TargetTimeSeconds:             a.TargetTimeSeconds,
		AcceptableHostIdleTimeSeconds: a.AcceptableHostIdleTimeSeconds,
		GroupVersions:                 a.GroupVersions,
		PatchFactor:                   a.PatchFactor,
		ExpectedRuntimeFactor:         a.ExpectedRuntimeFactor,
		PatchTimeInQueueFactor:        a.PatchTimeInQueueFactor,
		CommitQueueFactor:             a.CommitQueueFactor,
		MainlineTimeInQueueFactor:     a.MainlineTimeInQueueFactor,
		GenerateTaskFactor:            a.GenerateTaskFactor,
		StepbackTaskFactor:            a.StepbackTaskFactor,
	}, nil
}

// APIServiceFlags is a public structure representing the admin service flags
type APIServiceFlags struct {
	TaskDispatchDisabled           bool `json:"task_dispatch_disabled"`
	HostInitDisabled               bool `json:"host_init_disabled"`
	PodInitDisabled                bool `json:"pod_init_disabled"`
	S3BinaryDownloadsDisabled      bool `json:"s3_binary_downloads_disabled"`
	MonitorDisabled                bool `json:"monitor_disabled"`
	AlertsDisabled                 bool `json:"alerts_disabled"`
	AgentStartDisabled             bool `json:"agent_start_disabled"`
	RepotrackerDisabled            bool `json:"repotracker_disabled"`
	SchedulerDisabled              bool `json:"scheduler_disabled"`
	CheckBlockedTasksDisabled      bool `json:"check_blocked_tasks_disabled"`
	GithubPRTestingDisabled        bool `json:"github_pr_testing_disabled"`
	CLIUpdatesDisabled             bool `json:"cli_updates_disabled"`
	BackgroundStatsDisabled        bool `json:"background_stats_disabled"`
	TaskLoggingDisabled            bool `json:"task_logging_disabled"`
	CacheStatsJobDisabled          bool `json:"cache_stats_job_disabled"`
	CacheStatsEndpointDisabled     bool `json:"cache_stats_endpoint_disabled"`
	TaskReliabilityDisabled        bool `json:"task_reliability_disabled"`
	CommitQueueDisabled            bool `json:"commit_queue_disabled"`
	HostAllocatorDisabled          bool `json:"host_allocator_disabled"`
	PodAllocatorDisabled           bool `json:"pod_allocator_disabled"`
	UnrecognizedPodCleanupDisabled bool `json:"unrecognized_pod_cleanup_disabled"`
	BackgroundReauthDisabled       bool `json:"background_reauth_disabled"`
	BackgroundCleanupDisabled      bool `json:"background_cleanup_disabled"`
	CloudCleanupDisabled           bool `json:"cloud_cleanup_disabled"`
	LegacyUIPublicAccessDisabled   bool `json:"legacy_ui_public_access_disabled"`
	GlobalGitHubTokenDisabled      bool `json:"global_github_token_disabled"`
	UnsetFunctionVarsDisabled      bool `json:"unset_function_vars_disabled"`

	// Notifications Flags
	EventProcessingDisabled      bool `json:"event_processing_disabled"`
	JIRANotificationsDisabled    bool `json:"jira_notifications_disabled"`
	SlackNotificationsDisabled   bool `json:"slack_notifications_disabled"`
	EmailNotificationsDisabled   bool `json:"email_notifications_disabled"`
	WebhookNotificationsDisabled bool `json:"webhook_notifications_disabled"`
	GithubStatusAPIDisabled      bool `json:"github_status_api_disabled"`
}

type APISSHKeyPair struct {
	Name    *string `json:"name"`
	Public  *string `json:"public"`
	Private *string `json:"private"`
}

type APISlackConfig struct {
	Options *APISlackOptions `json:"options"`
	Token   *string          `json:"token"`
	Level   *string          `json:"level"`
	Name    *string          `json:"name"`
}

func (a *APISlackConfig) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case evergreen.SlackConfig:
		a.Token = utility.ToStringPtr(v.Token)
		a.Level = utility.ToStringPtr(v.Level)
		a.Name = utility.ToStringPtr(v.Name)
		if v.Options != nil {
			a.Options = &APISlackOptions{}
			if err := a.Options.BuildFromService(*v.Options); err != nil { //nolint:govet
				return errors.Wrap(err, "converting Slack options to API model")
			}
		}
	default:
		return errors.Errorf("programmatic error: expected Slack config but got type %T", h)
	}
	return nil
}

func (a *APISlackConfig) ToService() (interface{}, error) {
	i, err := a.Options.ToService()
	if err != nil {
		return nil, err
	}
	options := i.(send.SlackOptions) //nolint:govet
	return evergreen.SlackConfig{
		Token:   utility.FromStringPtr(a.Token),
		Level:   utility.FromStringPtr(a.Level),
		Name:    utility.FromStringPtr(a.Name),
		Options: &options,
	}, nil
}

type APISlackOptions struct {
	Channel       *string         `json:"channel"`
	Hostname      *string         `json:"hostname"`
	Name          *string         `json:"name"`
	Username      *string         `json:"username"`
	BasicMetadata bool            `json:"add_basic_metadata"`
	Fields        bool            `json:"use_fields"`
	AllFields     bool            `json:"all_fields"`
	FieldsSet     map[string]bool `json:"fields"`
}

func (a *APISlackOptions) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case send.SlackOptions:
		a.Channel = utility.ToStringPtr(v.Channel)
		a.Hostname = utility.ToStringPtr(v.Hostname)
		a.Name = utility.ToStringPtr(v.Name)
		a.Username = utility.ToStringPtr(v.Username)
		a.BasicMetadata = v.BasicMetadata
		a.Fields = v.Fields
		a.AllFields = v.AllFields
		a.FieldsSet = v.FieldsSet
	default:
		return errors.Errorf("programmatic error: expected Slack options but got type %T", h)
	}
	return nil
}

func (a *APISlackOptions) ToService() (interface{}, error) {
	if a == nil {
		return send.SlackOptions{}, nil
	}
	return send.SlackOptions{
		Channel:       utility.FromStringPtr(a.Channel),
		Hostname:      utility.FromStringPtr(a.Hostname),
		Name:          utility.FromStringPtr(a.Name),
		Username:      utility.FromStringPtr(a.Username),
		BasicMetadata: a.BasicMetadata,
		Fields:        a.Fields,
		AllFields:     a.AllFields,
		FieldsSet:     a.FieldsSet,
	}, nil
}

type APISplunkConfig struct {
	SplunkConnectionInfo *APISplunkConnectionInfo `json:"splunk_connection_info"`
}

func (a *APISplunkConfig) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case evergreen.SplunkConfig:
		a.SplunkConnectionInfo = &APISplunkConnectionInfo{}
		a.SplunkConnectionInfo.BuildFromService(v.SplunkConnectionInfo)
	default:
		return errors.Errorf("programmatic error: expected Splunk config but got type '%T'", h)
	}
	return nil
}

func (a *APISplunkConfig) ToService() (interface{}, error) {
	c := evergreen.SplunkConfig{}
	if a.SplunkConnectionInfo != nil {
		c.SplunkConnectionInfo = a.SplunkConnectionInfo.ToService()
	}
	return c, nil
}

type APISplunkConnectionInfo struct {
	ServerURL *string `json:"url"`
	Token     *string `json:"token"`
	Channel   *string `json:"channel"`
}

func (a *APISplunkConnectionInfo) BuildFromService(s send.SplunkConnectionInfo) {
	a.ServerURL = utility.ToStringPtr(s.ServerURL)
	a.Token = utility.ToStringPtr(s.Token)
	a.Channel = utility.ToStringPtr(s.Channel)
}

func (a *APISplunkConnectionInfo) ToService() send.SplunkConnectionInfo {
	return send.SplunkConnectionInfo{
		ServerURL: utility.FromStringPtr(a.ServerURL),
		Token:     utility.FromStringPtr(a.Token),
		Channel:   utility.FromStringPtr(a.Channel),
	}
}

type APIUIConfig struct {
	Url            *string  `json:"url"`
	HelpUrl        *string  `json:"help_url"`
	UIv2Url        *string  `json:"uiv2_url"`
	ParsleyUrl     *string  `json:"parsley_url"`
	HttpListenAddr *string  `json:"http_listen_addr"`
	Secret         *string  `json:"secret"`
	DefaultProject *string  `json:"default_project"`
	CacheTemplates bool     `json:"cache_templates"`
	CsrfKey        *string  `json:"csrf_key"`
	CORSOrigins    []string `json:"cors_origins"`
	LoginDomain    *string  `json:"login_domain"`
	UserVoice      *string  `json:"userVoice"`
}

func (a *APIUIConfig) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case evergreen.UIConfig:
		a.Url = utility.ToStringPtr(v.Url)
		a.HelpUrl = utility.ToStringPtr(v.HelpUrl)
		a.UIv2Url = utility.ToStringPtr(v.UIv2Url)
		a.ParsleyUrl = utility.ToStringPtr(v.ParsleyUrl)
		a.HttpListenAddr = utility.ToStringPtr(v.HttpListenAddr)
		a.Secret = utility.ToStringPtr(v.Secret)
		a.DefaultProject = utility.ToStringPtr(v.DefaultProject)
		a.CacheTemplates = v.CacheTemplates
		a.CsrfKey = utility.ToStringPtr(v.CsrfKey)
		a.CORSOrigins = v.CORSOrigins
		a.LoginDomain = utility.ToStringPtr(v.LoginDomain)
		a.UserVoice = utility.ToStringPtr(v.UserVoice)
	default:
		return errors.Errorf("programmatic error: expected UI config but got type %T", h)
	}
	return nil
}

func (a *APIUIConfig) ToService() (interface{}, error) {
	return evergreen.UIConfig{
		Url:            utility.FromStringPtr(a.Url),
		HelpUrl:        utility.FromStringPtr(a.HelpUrl),
		UIv2Url:        utility.FromStringPtr(a.UIv2Url),
		ParsleyUrl:     utility.FromStringPtr(a.ParsleyUrl),
		HttpListenAddr: utility.FromStringPtr(a.HttpListenAddr),
		Secret:         utility.FromStringPtr(a.Secret),
		DefaultProject: utility.FromStringPtr(a.DefaultProject),
		CacheTemplates: a.CacheTemplates,
		CsrfKey:        utility.FromStringPtr(a.CsrfKey),
		CORSOrigins:    a.CORSOrigins,
		LoginDomain:    utility.FromStringPtr(a.LoginDomain),
		UserVoice:      utility.FromStringPtr(a.UserVoice),
	}, nil
}

type APINewRelicConfig struct {
	AccountID     *string `json:"accountId"`
	TrustKey      *string `json:"trustKey"`
	AgentID       *string `json:"agentId"`
	LicenseKey    *string `json:"licenseKey"`
	ApplicationID *string `json:"applicationId"`
}

// BuildFromService builds a model from the service layer
func (a *APINewRelicConfig) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case evergreen.NewRelicConfig:
		a.AccountID = utility.ToStringPtr(v.AccountID)
		a.TrustKey = utility.ToStringPtr(v.TrustKey)
		a.AgentID = utility.ToStringPtr(v.AgentID)
		a.LicenseKey = utility.ToStringPtr(v.LicenseKey)
		a.ApplicationID = utility.ToStringPtr(v.ApplicationID)
	default:
		return errors.Errorf("programmatic error: expected New Relic config but got type %T", h)
	}
	return nil
}

// ToService returns a service model from an API model
func (a *APINewRelicConfig) ToService() (interface{}, error) {
	return evergreen.NewRelicConfig{
		AccountID:     utility.FromStringPtr(a.AccountID),
		TrustKey:      utility.FromStringPtr(a.TrustKey),
		AgentID:       utility.FromStringPtr(a.AgentID),
		LicenseKey:    utility.FromStringPtr(a.LicenseKey),
		ApplicationID: utility.FromStringPtr(a.ApplicationID),
	}, nil
}

// RestartTasksResponse is the response model returned from the /admin/restart route
type RestartResponse struct {
	ItemsRestarted []string `json:"items_restarted"`
	ItemsErrored   []string `json:"items_errored"`
}

// BuildFromService builds a model from the service layer
func (ab *APIBanner) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case APIBanner:
		ab.Text = v.Text
		ab.Theme = v.Theme
	default:
		return errors.Errorf("programmatic error: expected banner config but got type %T", h)
	}
	return nil
}

// ToService is not yet implemented
func (ab *APIBanner) ToService() (interface{}, error) {
	return ab, nil
}

// BuildFromService builds a model from the service layer
func (as *APIServiceFlags) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case evergreen.ServiceFlags:
		as.TaskDispatchDisabled = v.TaskDispatchDisabled
		as.HostInitDisabled = v.HostInitDisabled
		as.PodInitDisabled = v.PodInitDisabled
		as.S3BinaryDownloadsDisabled = v.S3BinaryDownloadsDisabled
		as.MonitorDisabled = v.MonitorDisabled
		as.AlertsDisabled = v.AlertsDisabled
		as.AgentStartDisabled = v.AgentStartDisabled
		as.RepotrackerDisabled = v.RepotrackerDisabled
		as.SchedulerDisabled = v.SchedulerDisabled
		as.CheckBlockedTasksDisabled = v.CheckBlockedTasksDisabled
		as.GithubPRTestingDisabled = v.GithubPRTestingDisabled
		as.CLIUpdatesDisabled = v.CLIUpdatesDisabled
		as.EventProcessingDisabled = v.EventProcessingDisabled
		as.JIRANotificationsDisabled = v.JIRANotificationsDisabled
		as.SlackNotificationsDisabled = v.SlackNotificationsDisabled
		as.EmailNotificationsDisabled = v.EmailNotificationsDisabled
		as.WebhookNotificationsDisabled = v.WebhookNotificationsDisabled
		as.GithubStatusAPIDisabled = v.GithubStatusAPIDisabled
		as.BackgroundStatsDisabled = v.BackgroundStatsDisabled
		as.TaskLoggingDisabled = v.TaskLoggingDisabled
		as.CacheStatsJobDisabled = v.CacheStatsJobDisabled
		as.CacheStatsEndpointDisabled = v.CacheStatsEndpointDisabled
		as.TaskReliabilityDisabled = v.TaskReliabilityDisabled
		as.CommitQueueDisabled = v.CommitQueueDisabled
		as.HostAllocatorDisabled = v.HostAllocatorDisabled
		as.PodAllocatorDisabled = v.PodAllocatorDisabled
		as.UnrecognizedPodCleanupDisabled = v.UnrecognizedPodCleanupDisabled
		as.BackgroundCleanupDisabled = v.BackgroundCleanupDisabled
		as.BackgroundReauthDisabled = v.BackgroundReauthDisabled
		as.CloudCleanupDisabled = v.CloudCleanupDisabled
		as.LegacyUIPublicAccessDisabled = v.LegacyUIPublicAccessDisabled
		as.GlobalGitHubTokenDisabled = v.GlobalGitHubTokenDisabled
		as.UnsetFunctionVarsDisabled = v.UnsetFunctionVarsDisabled
	default:
		return errors.Errorf("programmatic error: expected service flags config but got type %T", h)
	}
	return nil
}

// ToService returns a service model from an API model
func (as *APIServiceFlags) ToService() (interface{}, error) {
	return evergreen.ServiceFlags{
		TaskDispatchDisabled:           as.TaskDispatchDisabled,
		HostInitDisabled:               as.HostInitDisabled,
		PodInitDisabled:                as.PodInitDisabled,
		S3BinaryDownloadsDisabled:      as.S3BinaryDownloadsDisabled,
		MonitorDisabled:                as.MonitorDisabled,
		AlertsDisabled:                 as.AlertsDisabled,
		AgentStartDisabled:             as.AgentStartDisabled,
		RepotrackerDisabled:            as.RepotrackerDisabled,
		SchedulerDisabled:              as.SchedulerDisabled,
		CheckBlockedTasksDisabled:      as.CheckBlockedTasksDisabled,
		GithubPRTestingDisabled:        as.GithubPRTestingDisabled,
		CLIUpdatesDisabled:             as.CLIUpdatesDisabled,
		EventProcessingDisabled:        as.EventProcessingDisabled,
		JIRANotificationsDisabled:      as.JIRANotificationsDisabled,
		SlackNotificationsDisabled:     as.SlackNotificationsDisabled,
		EmailNotificationsDisabled:     as.EmailNotificationsDisabled,
		WebhookNotificationsDisabled:   as.WebhookNotificationsDisabled,
		GithubStatusAPIDisabled:        as.GithubStatusAPIDisabled,
		BackgroundStatsDisabled:        as.BackgroundStatsDisabled,
		TaskLoggingDisabled:            as.TaskLoggingDisabled,
		CacheStatsJobDisabled:          as.CacheStatsJobDisabled,
		CacheStatsEndpointDisabled:     as.CacheStatsEndpointDisabled,
		TaskReliabilityDisabled:        as.TaskReliabilityDisabled,
		CommitQueueDisabled:            as.CommitQueueDisabled,
		HostAllocatorDisabled:          as.HostAllocatorDisabled,
		PodAllocatorDisabled:           as.PodAllocatorDisabled,
		UnrecognizedPodCleanupDisabled: as.UnrecognizedPodCleanupDisabled,
		BackgroundCleanupDisabled:      as.BackgroundCleanupDisabled,
		BackgroundReauthDisabled:       as.BackgroundReauthDisabled,
		CloudCleanupDisabled:           as.CloudCleanupDisabled,
		LegacyUIPublicAccessDisabled:   as.LegacyUIPublicAccessDisabled,
		GlobalGitHubTokenDisabled:      as.GlobalGitHubTokenDisabled,
		UnsetFunctionVarsDisabled:      as.UnsetFunctionVarsDisabled,
	}, nil
}

// BuildFromService builds a model from the service layer
func (rtr *RestartResponse) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case *RestartResponse:
		rtr.ItemsRestarted = v.ItemsRestarted
		rtr.ItemsErrored = v.ItemsErrored
	default:
		return errors.Errorf("programmatic error: expected restart response but got type %T", h)
	}
	return nil
}

// ToService is not implemented for /admin/restart
func (rtr *RestartResponse) ToService() (interface{}, error) {
	return nil, errors.New("ToService not implemented for RestartTasksResponse")
}

func AdminDbToRestModel(in evergreen.ConfigSection) (Model, error) {
	id := in.SectionId()
	var out Model
	if id == evergreen.ConfigDocID {
		out = &APIAdminSettings{}
		err := out.BuildFromService(reflect.ValueOf(in).Interface())
		if err != nil {
			return nil, err
		}
	} else {
		structVal := reflect.ValueOf(*NewConfigModel())
		for i := 0; i < structVal.NumField(); i++ {
			// this assumes that the json tag is the same as the section ID
			tag := strings.Split(structVal.Type().Field(i).Tag.Get("json"), ",")[0]
			if tag != id {
				continue
			}

			propName := structVal.Type().Field(i).Name
			propVal := structVal.FieldByName(propName)
			propInterface := propVal.Interface()
			apiModel, ok := propInterface.(Model)
			if !ok {
				return nil, errors.Errorf("could not convert section '%s' to API model interface", id)
			}
			out = apiModel
		}
		if out == nil {
			return nil, errors.Errorf("section '%s' is not defined in the API admin settings", id)
		}
		err := out.BuildFromService(reflect.Indirect(reflect.ValueOf(in)).Interface())
		if err != nil {
			return nil, err
		}
	}

	return out, nil
}

type APIJIRANotificationsConfig struct {
	CustomFields map[string]APIJIRANotificationsProject `json:"custom_fields,omitempty"`
}

type APIJIRANotificationsProject struct {
	Fields     map[string]string `json:"fields,omitempty"`
	Components []string          `json:"components,omitempty"`
	Labels     []string          `json:"labels,omitempty"`
}

func (j *APIJIRANotificationsConfig) BuildFromService(h interface{}) error {
	var config *evergreen.JIRANotificationsConfig
	switch v := h.(type) {
	case *evergreen.JIRANotificationsConfig:
		config = v
	case evergreen.JIRANotificationsConfig:
		config = &v
	default:
		return errors.Errorf("programmatic error: expected Jira notifications config but got type %T", h)
	}

	j.CustomFields = make(map[string]APIJIRANotificationsProject)
	for _, project := range config.CustomFields {
		apiProject := APIJIRANotificationsProject{}
		if err := apiProject.BuildFromService(project); err != nil {
			return errors.Wrapf(err, "converting project '%s' to API model", project.Project)
		}

		j.CustomFields[project.Project] = apiProject
	}

	return nil
}

func (j *APIJIRANotificationsConfig) ToService() (interface{}, error) {
	service := evergreen.JIRANotificationsConfig{}
	if j.CustomFields == nil || len(j.CustomFields) == 0 {
		return service, nil
	}

	for projectName, fields := range j.CustomFields {
		projectIface, err := fields.ToService()
		if err != nil {
			return nil, errors.Errorf("converting project '%s' to service model", projectName)
		}
		project := projectIface.(evergreen.JIRANotificationsProject)

		project.Project = projectName
		service.CustomFields = append(service.CustomFields, project)
	}

	return service, nil
}

func (j *APIJIRANotificationsProject) BuildFromService(h interface{}) error {
	serviceProject, ok := h.(evergreen.JIRANotificationsProject)
	if !ok {
		return errors.Errorf("programmatic error: expected Jira project notifications config but got type %T", h)
	}

	apiFields := make(map[string]string)
	for _, field := range serviceProject.Fields {
		apiFields[field.Field] = field.Template
	}
	j.Fields = apiFields
	j.Components = serviceProject.Components
	j.Labels = serviceProject.Labels

	return nil
}

func (j *APIJIRANotificationsProject) ToService() (interface{}, error) {
	service := evergreen.JIRANotificationsProject{}
	for field, template := range j.Fields {
		service.Fields = append(service.Fields, evergreen.JIRANotificationsCustomField{Field: field, Template: template})
	}
	service.Components = j.Components
	service.Labels = j.Labels

	return service, nil
}

type APITriggerConfig struct {
	GenerateTaskDistro *string `json:"generate_distro"`
}

func (c *APITriggerConfig) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case evergreen.TriggerConfig:
		c.GenerateTaskDistro = utility.ToStringPtr(v.GenerateTaskDistro)
	default:
		return errors.Errorf("programmatic error: expected downstream task trigger config but got type %T", h)
	}
	return nil
}
func (c *APITriggerConfig) ToService() (interface{}, error) {
	return evergreen.TriggerConfig{
		GenerateTaskDistro: utility.FromStringPtr(c.GenerateTaskDistro),
	}, nil
}

type APIHostJasperConfig struct {
	BinaryName       *string `json:"binary_name,omitempty"`
	DownloadFileName *string `json:"download_file_name,omitempty"`
	Port             int     `json:"port,omitempty"`
	URL              *string `json:"url,omitempty"`
	Version          *string `json:"version,omitempty"`
}

func (c *APIHostJasperConfig) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case evergreen.HostJasperConfig:
		c.BinaryName = utility.ToStringPtr(v.BinaryName)
		c.DownloadFileName = utility.ToStringPtr(v.DownloadFileName)
		c.Port = v.Port
		c.URL = utility.ToStringPtr(v.URL)
		c.Version = utility.ToStringPtr(v.Version)
	default:
		return errors.Errorf("programmatic error: expected host Jasper config but got type %T", h)
	}
	return nil
}

func (c *APIHostJasperConfig) ToService() (interface{}, error) {
	return evergreen.HostJasperConfig{
		BinaryName:       utility.FromStringPtr(c.BinaryName),
		DownloadFileName: utility.FromStringPtr(c.DownloadFileName),
		Port:             c.Port,
		URL:              utility.FromStringPtr(c.URL),
		Version:          utility.FromStringPtr(c.Version),
	}, nil
}

type APISpawnHostConfig struct {
	UnexpirableHostsPerUser   *int `json:"unexpirable_hosts_per_user"`
	UnexpirableVolumesPerUser *int `json:"unexpirable_volumes_per_user"`
	SpawnHostsPerUser         *int `json:"spawn_hosts_per_user"`
}

func (c *APISpawnHostConfig) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case evergreen.SpawnHostConfig:
		c.UnexpirableHostsPerUser = &v.UnexpirableHostsPerUser
		c.UnexpirableVolumesPerUser = &v.UnexpirableVolumesPerUser
		c.SpawnHostsPerUser = &v.SpawnHostsPerUser
	default:
		return errors.Errorf("programmatic error: expected spawn host config but got type %T", h)
	}
	return nil
}

func (c *APISpawnHostConfig) ToService() (interface{}, error) {
	config := evergreen.SpawnHostConfig{
		UnexpirableHostsPerUser:   evergreen.DefaultUnexpirableHostsPerUser,
		UnexpirableVolumesPerUser: evergreen.DefaultUnexpirableVolumesPerUser,
		SpawnHostsPerUser:         evergreen.DefaultMaxSpawnHostsPerUser,
	}
	if c.UnexpirableHostsPerUser != nil {
		config.UnexpirableHostsPerUser = *c.UnexpirableHostsPerUser
	}
	if c.UnexpirableVolumesPerUser != nil {
		config.UnexpirableVolumesPerUser = *c.UnexpirableVolumesPerUser
	}
	if c.SpawnHostsPerUser != nil {
		config.SpawnHostsPerUser = *c.SpawnHostsPerUser
	}

	return config, nil
}

type APITracerSettings struct {
	Enabled           *bool   `json:"enabled"`
	CollectorEndpoint *string `json:"collector_endpoint"`
}

func (c *APITracerSettings) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case evergreen.TracerConfig:
		c.Enabled = &v.Enabled
		c.CollectorEndpoint = &v.CollectorEndpoint
	default:
		return errors.Errorf("programmatic error: expected tracer config but got type %T", h)
	}
	return nil
}

func (c *APITracerSettings) ToService() (interface{}, error) {
	config := evergreen.TracerConfig{
		Enabled:           utility.FromBoolPtr(c.Enabled),
		CollectorEndpoint: utility.FromStringPtr(c.CollectorEndpoint),
	}

	return config, nil
}

type APIDataPipesConfig struct {
	Host         *string `json:"host"`
	Region       *string `json:"region"`
	AWSAccessKey *string `json:"aws_access_key"`
	AWSSecretKey *string `json:"aws_secret_key"`
	AWSToken     *string `json:"aws_token"`
}

func (c *APIDataPipesConfig) BuildFromService(h interface{}) error {
	switch v := h.(type) {
	case evergreen.DataPipesConfig:
		c.Host = utility.ToStringPtr(v.Host)
		c.Region = utility.ToStringPtr(v.Region)
		c.AWSAccessKey = utility.ToStringPtr(v.AWSAccessKey)
		c.AWSSecretKey = utility.ToStringPtr(v.AWSSecretKey)
		c.AWSToken = utility.ToStringPtr(v.AWSToken)
	default:
		return errors.Errorf("programmatic error: expected Data-Pipes config but got type %T", h)
	}

	return nil
}

func (c *APIDataPipesConfig) ToService() (interface{}, error) {
	return evergreen.DataPipesConfig{
		Host:         utility.FromStringPtr(c.Host),
		Region:       utility.FromStringPtr(c.Region),
		AWSAccessKey: utility.FromStringPtr(c.AWSAccessKey),
		AWSSecretKey: utility.FromStringPtr(c.AWSSecretKey),
		AWSToken:     utility.FromStringPtr(c.AWSToken),
	}, nil
}
