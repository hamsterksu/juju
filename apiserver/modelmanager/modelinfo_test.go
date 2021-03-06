// Copyright 2016 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package modelmanager_test

import (
	"strings"
	"time"

	"github.com/juju/errors"
	gitjujutesting "github.com/juju/testing"
	jc "github.com/juju/testing/checkers"
	"github.com/juju/utils/series"
	gc "gopkg.in/check.v1"
	"gopkg.in/juju/names.v2"

	"github.com/juju/juju/apiserver/common"
	"github.com/juju/juju/apiserver/metricsender"
	"github.com/juju/juju/apiserver/modelmanager"
	"github.com/juju/juju/apiserver/params"
	apiservertesting "github.com/juju/juju/apiserver/testing"
	"github.com/juju/juju/cloud"
	"github.com/juju/juju/controller"
	"github.com/juju/juju/core/description"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/environs/config"
	"github.com/juju/juju/state"
	"github.com/juju/juju/status"
	coretesting "github.com/juju/juju/testing"
)

type modelInfoSuite struct {
	coretesting.BaseSuite
	authorizer   apiservertesting.FakeAuthorizer
	st           *mockState
	modelmanager *modelmanager.ModelManagerAPI
}

var _ = gc.Suite(&modelInfoSuite{})

func (s *modelInfoSuite) SetUpTest(c *gc.C) {
	s.BaseSuite.SetUpTest(c)
	s.authorizer = apiservertesting.FakeAuthorizer{
		Tag: names.NewUserTag("admin@local"),
	}
	s.st = &mockState{
		uuid: coretesting.ModelTag.Id(),
		cloud: cloud.Cloud{
			Type:      "dummy",
			AuthTypes: []cloud.AuthType{cloud.EmptyAuthType},
		},
	}
	s.st.controllerModel = &mockModel{
		owner: names.NewUserTag("admin@local"),
		life:  state.Alive,
		cfg:   coretesting.ModelConfig(c),
		status: status.StatusInfo{
			Status: status.StatusAvailable,
			Since:  &time.Time{},
		},
		users: []*mockModelUser{{
			userName: "admin",
			access:   description.AdminAccess,
		}, {
			userName: "otheruser",
			access:   description.AdminAccess,
		}},
	}

	s.st.model = &mockModel{
		owner: names.NewUserTag("bob@local"),
		cfg:   coretesting.ModelConfig(c),
		life:  state.Dying,
		status: status.StatusInfo{
			Status: status.StatusDestroying,
			Since:  &time.Time{},
		},

		users: []*mockModelUser{{
			userName: "admin",
			access:   description.AdminAccess,
		}, {
			userName:    "bob@local",
			displayName: "Bob",
			access:      description.ReadAccess,
		}, {
			userName:    "charlotte@local",
			displayName: "Charlotte",
			access:      description.ReadAccess,
		}},
	}
	var err error
	s.modelmanager, err = modelmanager.NewModelManagerAPI(s.st, nil, &s.authorizer)
	c.Assert(err, jc.ErrorIsNil)
}

func (s *modelInfoSuite) setAPIUser(c *gc.C, user names.UserTag) {
	s.authorizer.Tag = user
	modelmanager, err := modelmanager.NewModelManagerAPI(s.st, nil, s.authorizer)
	c.Assert(err, jc.ErrorIsNil)
	s.modelmanager = modelmanager
}

func (s *modelInfoSuite) TestModelInfo(c *gc.C) {
	info := s.getModelInfo(c)
	c.Assert(info, jc.DeepEquals, params.ModelInfo{
		Name:               "testenv",
		UUID:               s.st.model.cfg.UUID(),
		ControllerUUID:     "deadbeef-0bad-400d-8000-4b1d0d06f00d",
		OwnerTag:           "user-bob@local",
		ProviderType:       "someprovider",
		Cloud:              "some-cloud",
		CloudRegion:        "some-region",
		CloudCredentialTag: "cloudcred-some-cloud_bob@local_some-credential",
		DefaultSeries:      series.LatestLts(),
		Life:               params.Dying,
		Status: params.EntityStatus{
			Status: status.StatusDestroying,
			Since:  &time.Time{},
		},
		Users: []params.ModelUserInfo{{
			UserName:       "admin",
			LastConnection: &time.Time{},
			Access:         params.ModelAdminAccess,
		}, {
			UserName:       "bob@local",
			DisplayName:    "Bob",
			LastConnection: &time.Time{},
			Access:         params.ModelReadAccess,
		}, {
			UserName:       "charlotte@local",
			DisplayName:    "Charlotte",
			LastConnection: &time.Time{},
			Access:         params.ModelReadAccess,
		}},
	})
	s.st.CheckCalls(c, []gitjujutesting.StubCall{
		{"ControllerTag", nil},
		{"ModelUUID", nil},
		{"ForModel", []interface{}{names.NewModelTag(s.st.model.cfg.UUID())}},
		{"Model", nil},
		{"ControllerConfig", nil},
		{"LastModelConnection", []interface{}{names.NewUserTag("admin")}},
		{"LastModelConnection", []interface{}{names.NewLocalUserTag("bob")}},
		{"LastModelConnection", []interface{}{names.NewLocalUserTag("charlotte")}},
		{"Close", nil},
	})
	s.st.model.CheckCalls(c, []gitjujutesting.StubCall{
		{"Config", nil},
		{"Users", nil},
		{"ModelTag", nil},
		{"ModelTag", nil},
		{"ModelTag", nil},
		{"Status", nil},
		{"Owner", nil},
		{"Life", nil},
		{"Cloud", nil},
		{"CloudRegion", nil},
		{"CloudCredential", nil},
	})
}

func (s *modelInfoSuite) TestModelInfoOwner(c *gc.C) {
	s.setAPIUser(c, names.NewUserTag("bob@local"))
	info := s.getModelInfo(c)
	c.Assert(info.Users, gc.HasLen, 3)
}

func (s *modelInfoSuite) TestModelInfoNonOwner(c *gc.C) {
	s.setAPIUser(c, names.NewUserTag("charlotte@local"))
	info := s.getModelInfo(c)
	c.Assert(info.Users, gc.HasLen, 1)
	c.Assert(info.Users[0].UserName, gc.Equals, "charlotte@local")
}

func (s *modelInfoSuite) getModelInfo(c *gc.C) params.ModelInfo {
	results, err := s.modelmanager.ModelInfo(params.Entities{
		Entities: []params.Entity{{
			names.NewModelTag(s.st.model.cfg.UUID()).String(),
		}},
	})
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(results.Results, gc.HasLen, 1)
	c.Assert(results.Results[0].Result, gc.NotNil)
	c.Assert(results.Results[0].Error, gc.IsNil)
	return *results.Results[0].Result
}

func (s *modelInfoSuite) TestModelInfoErrorInvalidTag(c *gc.C) {
	s.testModelInfoError(c, "user-bob", `"user-bob" is not a valid model tag`)
}

func (s *modelInfoSuite) TestModelInfoErrorGetModelNotFound(c *gc.C) {
	s.st.SetErrors(errors.NotFoundf("model"))
	s.testModelInfoError(c, coretesting.ModelTag.String(), `permission denied`)
}

func (s *modelInfoSuite) TestModelInfoErrorModelConfig(c *gc.C) {
	s.st.model.SetErrors(errors.Errorf("no config for you"))
	s.testModelInfoError(c, coretesting.ModelTag.String(), `no config for you`)
}

func (s *modelInfoSuite) TestModelInfoErrorModelUsers(c *gc.C) {
	s.st.model.SetErrors(errors.Errorf("no users for you"))
	s.testModelInfoError(c, coretesting.ModelTag.String(), `no users for you`)
}

func (s *modelInfoSuite) TestModelInfoErrorNoModelUsers(c *gc.C) {
	s.st.model.users = nil
	s.testModelInfoError(c, coretesting.ModelTag.String(), `permission denied`)
}

func (s *modelInfoSuite) TestModelInfoErrorNoAccess(c *gc.C) {
	s.setAPIUser(c, names.NewUserTag("nemo@local"))
	s.testModelInfoError(c, coretesting.ModelTag.String(), `permission denied`)
}

func (s *modelInfoSuite) testModelInfoError(c *gc.C, modelTag, expectedErr string) {
	results, err := s.modelmanager.ModelInfo(params.Entities{
		Entities: []params.Entity{{modelTag}},
	})
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(results.Results, gc.HasLen, 1)
	c.Assert(results.Results[0].Result, gc.IsNil)
	c.Assert(results.Results[0].Error, gc.ErrorMatches, expectedErr)
}

type mockState struct {
	gitjujutesting.Stub

	environs.EnvironConfigGetter
	common.APIHostPortsGetter
	common.ToolsStorageGetter
	common.BlockGetter
	metricsender.MetricsSenderBackend

	uuid            string
	cloud           cloud.Cloud
	clouds          map[names.CloudTag]cloud.Cloud
	model           *mockModel
	controllerModel *mockModel
	users           []description.UserAccess
	cred            cloud.Credential
}

type fakeModelDescription struct {
	description.Model `yaml:"-"`

	UUID string `yaml:"model-uuid"`
}

func (st *mockState) Export() (description.Model, error) {
	return &fakeModelDescription{UUID: st.uuid}, nil
}

func (st *mockState) ModelUUID() string {
	st.MethodCall(st, "ModelUUID")
	return st.uuid
}

func (st *mockState) ModelsForUser(user names.UserTag) ([]*state.UserModel, error) {
	st.MethodCall(st, "ModelsForUser", user)
	return nil, st.NextErr()
}

func (st *mockState) IsControllerAdmin(user names.UserTag) (bool, error) {
	st.MethodCall(st, "IsControllerAdmin", user)
	if st.controllerModel == nil {
		return user.Canonical() == "admin@local", st.NextErr()
	}
	if st.controllerModel.users == nil {
		return user.Canonical() == "admin@local", st.NextErr()
	}

	for _, u := range st.controllerModel.users {
		if user.Name() == u.userName && u.access == description.AdminAccess {
			nextErr := st.NextErr()
			if user.Name() != "admin" {
				panic(user.Name())
			}
			return true, nextErr
		}
	}
	return false, st.NextErr()
}

func (st *mockState) NewModel(args state.ModelArgs) (common.Model, common.ModelManagerBackend, error) {
	st.MethodCall(st, "NewModel", args)
	st.model.tag = names.NewModelTag(args.Config.UUID())
	return st.model, st, st.NextErr()
}

func (st *mockState) ControllerModel() (common.Model, error) {
	st.MethodCall(st, "ControllerModel")
	return st.controllerModel, st.NextErr()
}

func (st *mockState) ControllerTag() names.ControllerTag {
	st.MethodCall(st, "ControllerTag")
	return names.NewControllerTag(st.controllerModel.tag.Id())
}

func (st *mockState) ComposeNewModelConfig(modelAttr map[string]interface{}, regionSpec *environs.RegionSpec) (map[string]interface{}, error) {
	st.MethodCall(st, "ComposeNewModelConfig")
	attr := make(map[string]interface{})
	for attrName, val := range modelAttr {
		attr[attrName] = val
	}
	attr["something"] = "value"
	return attr, st.NextErr()
}

func (st *mockState) ControllerUUID() string {
	st.MethodCall(st, "ControllerUUID")
	return st.uuid
}

func (st *mockState) ControllerConfig() (controller.Config, error) {
	st.MethodCall(st, "ControllerConfig")
	return controller.Config{
		controller.ControllerUUIDKey: "deadbeef-0bad-400d-8000-4b1d0d06f00d",
	}, st.NextErr()
}

func (st *mockState) ForModel(tag names.ModelTag) (common.ModelManagerBackend, error) {
	st.MethodCall(st, "ForModel", tag)
	return st, st.NextErr()
}

func (st *mockState) GetModel(tag names.ModelTag) (common.Model, error) {
	st.MethodCall(st, "GetModel", tag)
	return st.model, st.NextErr()
}

func (st *mockState) Model() (common.Model, error) {
	st.MethodCall(st, "Model")
	return st.model, st.NextErr()
}

func (st *mockState) ModelTag() names.ModelTag {
	st.MethodCall(st, "ModelTag")
	return st.model.ModelTag()
}

func (st *mockState) AllModels() ([]common.Model, error) {
	st.MethodCall(st, "AllModels")
	return []common.Model{st.model}, st.NextErr()
}

func (st *mockState) Clouds() (map[names.CloudTag]cloud.Cloud, error) {
	st.MethodCall(st, "Clouds")
	return st.clouds, st.NextErr()
}

func (st *mockState) Cloud(name string) (cloud.Cloud, error) {
	st.MethodCall(st, "Cloud", name)
	return st.cloud, st.NextErr()
}

func (st *mockState) CloudCredential(tag names.CloudCredentialTag) (cloud.Credential, error) {
	st.MethodCall(st, "CloudCredential", tag)
	return st.cred, st.NextErr()
}

func (st *mockState) Close() error {
	st.MethodCall(st, "Close")
	return st.NextErr()
}

func (st *mockState) AddModelUser(spec state.UserAccessSpec) (description.UserAccess, error) {
	st.MethodCall(st, "AddModelUser", spec)
	return description.UserAccess{}, st.NextErr()
}

func (st *mockState) AddControllerUser(spec state.UserAccessSpec) (description.UserAccess, error) {
	st.MethodCall(st, "AddControllerUser", spec)
	return description.UserAccess{}, st.NextErr()
}

func (st *mockState) RemoveModelUser(tag names.UserTag) error {
	st.MethodCall(st, "RemoveModelUser", tag)
	return st.NextErr()
}

func (st *mockState) UserAccess(tag names.UserTag, target names.Tag) (description.UserAccess, error) {
	st.MethodCall(st, "ModelUser", tag, target)
	return description.UserAccess{}, st.NextErr()
}

func (st *mockState) LastModelConnection(user names.UserTag) (time.Time, error) {
	st.MethodCall(st, "LastModelConnection", user)
	return time.Time{}, st.NextErr()
}

func (st *mockState) RemoveUserAccess(subject names.UserTag, target names.Tag) error {
	st.MethodCall(st, "RemoveUserAccess", subject, target)
	return st.NextErr()
}

func (st *mockState) SetUserAccess(subject names.UserTag, target names.Tag, access description.Access) (description.UserAccess, error) {
	st.MethodCall(st, "SetUserAccess", subject, target, access)
	return description.UserAccess{}, st.NextErr()
}

func (st *mockState) DumpAll() (map[string]interface{}, error) {
	st.MethodCall(st, "DumpAll")
	return map[string]interface{}{
		"models": "lots of data",
	}, st.NextErr()
}

type mockModel struct {
	gitjujutesting.Stub
	owner  names.UserTag
	life   state.Life
	tag    names.ModelTag
	status status.StatusInfo
	cfg    *config.Config
	users  []*mockModelUser
}

func (m *mockModel) Config() (*config.Config, error) {
	m.MethodCall(m, "Config")
	return m.cfg, m.NextErr()
}

func (m *mockModel) Owner() names.UserTag {
	m.MethodCall(m, "Owner")
	m.PopNoErr()
	return m.owner
}

func (m *mockModel) ModelTag() names.ModelTag {
	m.MethodCall(m, "ModelTag")
	m.PopNoErr()
	return m.tag
}

func (m *mockModel) Life() state.Life {
	m.MethodCall(m, "Life")
	m.PopNoErr()
	return m.life
}

func (m *mockModel) Status() (status.StatusInfo, error) {
	m.MethodCall(m, "Status")
	return m.status, m.NextErr()
}

func (m *mockModel) Cloud() string {
	m.MethodCall(m, "Cloud")
	m.PopNoErr()
	return "some-cloud"
}

func (m *mockModel) CloudRegion() string {
	m.MethodCall(m, "CloudRegion")
	m.PopNoErr()
	return "some-region"
}

func (m *mockModel) CloudCredential() (names.CloudCredentialTag, bool) {
	m.MethodCall(m, "CloudCredential")
	m.PopNoErr()
	return names.NewCloudCredentialTag("some-cloud/bob@local/some-credential"), true
}

func (m *mockModel) Users() ([]description.UserAccess, error) {
	m.MethodCall(m, "Users")
	if err := m.NextErr(); err != nil {
		return nil, err
	}
	users := make([]description.UserAccess, len(m.users))
	for i, user := range m.users {
		users[i] = description.UserAccess{
			UserID:      strings.ToLower(user.userName),
			UserTag:     names.NewUserTag(user.userName),
			Object:      m.ModelTag(),
			Access:      user.access,
			DisplayName: user.displayName,
			UserName:    user.userName,
		}
	}
	return users, nil
}

func (m *mockModel) Destroy() error {
	m.MethodCall(m, "Destroy")
	return m.NextErr()
}

func (m *mockModel) DestroyIncludingHosted() error {
	m.MethodCall(m, "DestroyIncludingHosted")
	return m.NextErr()
}

type mockModelUser struct {
	gitjujutesting.Stub
	userName       string
	displayName    string
	lastConnection time.Time
	access         description.Access
}
