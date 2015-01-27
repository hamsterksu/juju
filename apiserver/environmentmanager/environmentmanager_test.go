// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package environmentmanager_test

import (
	"github.com/juju/loggo"
	"github.com/juju/names"
	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/apiserver/common"
	"github.com/juju/juju/apiserver/environmentmanager"
	"github.com/juju/juju/apiserver/params"
	apiservertesting "github.com/juju/juju/apiserver/testing"
	jujutesting "github.com/juju/juju/juju/testing"
	"github.com/juju/juju/state"
	"github.com/juju/juju/version"
)

type envManagerSuite struct {
	jujutesting.JujuConnSuite

	envmanager *environmentmanager.EnvironmentManagerAPI
	resources  *common.Resources
	authoriser apiservertesting.FakeAuthorizer
}

var _ = gc.Suite(&envManagerSuite{})

func (s *envManagerSuite) SetUpTest(c *gc.C) {
	s.JujuConnSuite.SetUpTest(c)
	s.resources = common.NewResources()
	s.AddCleanup(func(_ *gc.C) { s.resources.StopAll() })

	s.authoriser = apiservertesting.FakeAuthorizer{
		Tag: s.AdminUserTag(c),
	}

	loggo.GetLogger("juju.apiserver.environmentmanager").SetLogLevel(loggo.TRACE)
}

func (s *envManagerSuite) TestNewAPIAcceptsClient(c *gc.C) {
	anAuthoriser := s.authoriser
	anAuthoriser.Tag = names.NewUserTag("external@remote")
	endPoint, err := environmentmanager.NewEnvironmentManagerAPI(s.State, s.resources, anAuthoriser)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(endPoint, gc.NotNil)
}

func (s *envManagerSuite) TestNewAPIRefusesNonClient(c *gc.C) {
	anAuthoriser := s.authoriser
	anAuthoriser.Tag = names.NewUnitTag("mysql/0")
	endPoint, err := environmentmanager.NewEnvironmentManagerAPI(s.State, s.resources, anAuthoriser)
	c.Assert(endPoint, gc.IsNil)
	c.Assert(err, gc.ErrorMatches, "permission denied")
}

func (s *envManagerSuite) createArgs(c *gc.C, owner names.UserTag) params.EnvironmentCreateArgs {
	return params.EnvironmentCreateArgs{
		OwnerTag: owner.String(),
		Account:  make(map[string]interface{}),
		Config: map[string]interface{}{
			"name":            "test-env",
			"authorized-keys": "ssh-key",
			// And to make it a valid dummy config
			"state-server": false,
		},
	}
}

func (s *envManagerSuite) createArgsForVersion(c *gc.C, owner names.UserTag, ver interface{}) params.EnvironmentCreateArgs {
	params := s.createArgs(c, owner)
	params.Config["agent-version"] = ver
	return params
}

func (s *envManagerSuite) setAPIUser(c *gc.C, user names.UserTag) {
	s.authoriser.Tag = user
	envmanager, err := environmentmanager.NewEnvironmentManagerAPI(s.State, s.resources, s.authoriser)
	c.Assert(err, jc.ErrorIsNil)
	s.envmanager = envmanager
}

func (s *envManagerSuite) TestUserCanCreateEnvironment(c *gc.C) {
	owner := names.NewUserTag("external@remote")
	s.setAPIUser(c, owner)
	env, err := s.envmanager.CreateEnvironment(s.createArgs(c, owner))
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(env.OwnerTag, gc.Equals, owner.String())
	c.Assert(env.Name, gc.Equals, "test-env")
}

func (s *envManagerSuite) TestAdminCanCreateEnvironmentForSomeoneElse(c *gc.C) {
	s.setAPIUser(c, s.AdminUserTag(c))
	owner := names.NewUserTag("external@remote")
	env, err := s.envmanager.CreateEnvironment(s.createArgs(c, owner))
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(env.OwnerTag, gc.Equals, owner.String())
	c.Assert(env.Name, gc.Equals, "test-env")
}

func (s *envManagerSuite) TestNonAdminCannotCreateEnvironmentForSomeoneElse(c *gc.C) {
	s.setAPIUser(c, names.NewUserTag("non-admin@remote"))
	owner := names.NewUserTag("external@remote")
	_, err := s.envmanager.CreateEnvironment(s.createArgs(c, owner))
	c.Assert(err, gc.ErrorMatches, "permission denied")
}

func (s *envManagerSuite) TestCreateEnvironmentValidatesConfig(c *gc.C) {
	admin := s.AdminUserTag(c)
	s.setAPIUser(c, admin)
	args := s.createArgs(c, admin)
	delete(args.Config, "state-server")
	_, err := s.envmanager.CreateEnvironment(args)
	c.Assert(err, gc.ErrorMatches, "state-server: expected bool, got nothing")
}

func (s *envManagerSuite) TestCreateEnvironmentBadConfig(c *gc.C) {
	owner := names.NewUserTag("external@remote")
	s.setAPIUser(c, owner)
	for i, test := range []struct {
		key      string
		value    interface{}
		errMatch string
	}{
		{
			key:      "uuid",
			value:    "anything",
			errMatch: `uuid is generated, you cannot specify one`,
		}, {
			key:      "type",
			value:    "other",
			errMatch: `specified type "other" does not match apiserver "dummy"`,
		}, {
			key:      "ca-cert",
			value:    "some-cert",
			errMatch: `(?s)specified ca-cert "some-cert" does not match apiserver ".*"`,
		}, {
			key:      "state-port",
			value:    9876,
			errMatch: `specified state-port "9876" does not match apiserver "1234"`,
		}, {
			// The api-port is dynamic, but always in user-space, so > 1024.
			key:      "api-port",
			value:    123,
			errMatch: `specified api-port "123" does not match apiserver ".*"`,
		}, {
			key:      "syslog-port",
			value:    1234,
			errMatch: `specified syslog-port "1234" does not match apiserver "2345"`,
		}, {
			key:      "rsyslog-ca-cert",
			value:    "some-cert",
			errMatch: `specified rsyslog-ca-cert "some-cert" does not match apiserver ".*"`,
		},
	} {
		c.Logf("%d: %s", i, test.key)
		args := s.createArgs(c, owner)
		args.Config[test.key] = test.value
		_, err := s.envmanager.CreateEnvironment(args)
		c.Assert(err, gc.ErrorMatches, test.errMatch)

	}
}

func (s *envManagerSuite) TestCreateEnvironmentSameAgentVersion(c *gc.C) {
	admin := s.AdminUserTag(c)
	s.setAPIUser(c, admin)
	args := s.createArgsForVersion(c, admin, version.Current.Number.String())
	_, err := s.envmanager.CreateEnvironment(args)
	c.Assert(err, jc.ErrorIsNil)
}

func (s *envManagerSuite) TestCreateEnvironmentBadAgentVersion(c *gc.C) {
	admin := s.AdminUserTag(c)
	s.setAPIUser(c, admin)

	bigger := version.Current.Number
	bigger.Minor += 1

	smaller := version.Current.Number
	smaller.Minor -= 1

	for i, test := range []struct {
		value    interface{}
		errMatch string
	}{
		{
			value:    42,
			errMatch: "agent-version must be a string but has type 'int'",
		}, {
			value:    "not a number",
			errMatch: `invalid version "not a number"`,
		}, {
			value:    bigger.String(),
			errMatch: "agent-version cannot be greater than the server: .*",
		}, {
			value:    smaller.String(),
			errMatch: "no tools found for version .*",
		},
	} {
		c.Logf("test %d", i)
		args := s.createArgsForVersion(c, admin, test.value)
		_, err := s.envmanager.CreateEnvironment(args)
		c.Check(err, gc.ErrorMatches, test.errMatch)
	}
}

func (s *envManagerSuite) TestListEnvironmentsForSelf(c *gc.C) {
	user := names.NewUserTag("external@remote")
	s.setAPIUser(c, user)
	result, err := s.envmanager.ListEnvironments(params.Entity{user.String()})
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(result.Environments, gc.HasLen, 0)
}

func (s *envManagerSuite) checkEnvironmentMatches(c *gc.C, env params.Environment, expected *state.Environment) {
	c.Check(env.Name, gc.Equals, expected.Name())
	c.Check(env.UUID, gc.Equals, expected.UUID())
	c.Check(env.OwnerTag, gc.Equals, expected.Owner().String())
}

func (s *envManagerSuite) TestListEnvironmentsAdminSelf(c *gc.C) {
	user := s.AdminUserTag(c)
	s.setAPIUser(c, user)
	result, err := s.envmanager.ListEnvironments(params.Entity{user.String()})
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(result.Environments, gc.HasLen, 1)
	expected, err := s.State.Environment()
	c.Assert(err, jc.ErrorIsNil)
	s.checkEnvironmentMatches(c, result.Environments[0], expected)
}

func (s *envManagerSuite) TestListEnvironmentsAdminListsOther(c *gc.C) {
	user := s.AdminUserTag(c)
	s.setAPIUser(c, user)
	other := names.NewUserTag("external@remote")
	result, err := s.envmanager.ListEnvironments(params.Entity{other.String()})
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(result.Environments, gc.HasLen, 0)
}

func (s *envManagerSuite) TestListEnvironmentsDenied(c *gc.C) {
	user := names.NewUserTag("external@remote")
	s.setAPIUser(c, user)
	other := names.NewUserTag("other@remote")
	_, err := s.envmanager.ListEnvironments(params.Entity{other.String()})
	c.Assert(err, gc.ErrorMatches, "permission denied")
}