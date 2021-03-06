// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package controller_test

import (
	"fmt"
	"net"
	"time"

	"github.com/juju/errors"
	jc "github.com/juju/testing/checkers"
	"github.com/juju/utils"
	gc "gopkg.in/check.v1"
	"gopkg.in/juju/names.v2"
	"gopkg.in/macaroon.v1"

	"github.com/juju/juju/api"
	"github.com/juju/juju/api/base"
	"github.com/juju/juju/api/controller"
	"github.com/juju/juju/apiserver"
	commontesting "github.com/juju/juju/apiserver/common/testing"
	"github.com/juju/juju/apiserver/observer"
	"github.com/juju/juju/apiserver/observer/fakeobserver"
	"github.com/juju/juju/apiserver/params"
	"github.com/juju/juju/core/description"
	jujutesting "github.com/juju/juju/juju/testing"
	"github.com/juju/juju/state"
	"github.com/juju/juju/state/multiwatcher"
	"github.com/juju/juju/testing"
	"github.com/juju/juju/testing/factory"
)

type controllerSuite struct {
	jujutesting.JujuConnSuite
	commontesting.BlockHelper
}

var _ = gc.Suite(&controllerSuite{})

func (s *controllerSuite) SetUpTest(c *gc.C) {
	s.JujuConnSuite.SetUpTest(c)
}

func (s *controllerSuite) OpenAPI(c *gc.C) *controller.Client {
	return controller.NewClient(s.OpenControllerAPI(c))
}

func (s *controllerSuite) TestAllModels(c *gc.C) {
	owner := names.NewUserTag("user@remote")
	s.Factory.MakeModel(c, &factory.ModelParams{
		Name: "first", Owner: owner}).Close()
	s.Factory.MakeModel(c, &factory.ModelParams{
		Name: "second", Owner: owner}).Close()

	sysManager := s.OpenAPI(c)
	defer sysManager.Close()
	envs, err := sysManager.AllModels()
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(envs, gc.HasLen, 3)

	var obtained []string
	for _, env := range envs {
		obtained = append(obtained, fmt.Sprintf("%s/%s", env.Owner, env.Name))
	}
	expected := []string{
		"admin@local/controller",
		"user@remote/first",
		"user@remote/second",
	}
	c.Assert(obtained, jc.SameContents, expected)
}

func (s *controllerSuite) TestModelConfig(c *gc.C) {
	sysManager := s.OpenAPI(c)
	defer sysManager.Close()
	cfg, err := sysManager.ModelConfig()
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(cfg["name"], gc.Equals, "controller")
}

func (s *controllerSuite) TestControllerConfig(c *gc.C) {
	sysManager := s.OpenAPI(c)
	defer sysManager.Close()
	cfg, err := sysManager.ControllerConfig()
	c.Assert(err, jc.ErrorIsNil)
	cfgFromDB, err := s.State.ControllerConfig()
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(cfg["controller-uuid"], gc.Equals, cfgFromDB.ControllerUUID())
	c.Assert(int(cfg["state-port"].(float64)), gc.Equals, cfgFromDB.StatePort())
	c.Assert(int(cfg["api-port"].(float64)), gc.Equals, cfgFromDB.APIPort())
}

func (s *controllerSuite) TestDestroyController(c *gc.C) {
	st := s.Factory.MakeModel(c, &factory.ModelParams{Name: "foo"})
	factory.NewFactory(st).MakeMachine(c, nil) // make it non-empty
	st.Close()

	sysManager := s.OpenAPI(c)
	defer sysManager.Close()
	err := sysManager.DestroyController(false)
	c.Assert(err, gc.ErrorMatches, `failed to destroy model: hosting 1 other models \(controller has hosted models\)`)
}

func (s *controllerSuite) TestListBlockedModels(c *gc.C) {
	err := s.State.SwitchBlockOn(state.ChangeBlock, "change block for controller")
	err = s.State.SwitchBlockOn(state.DestroyBlock, "destroy block for controller")
	c.Assert(err, jc.ErrorIsNil)

	sysManager := s.OpenAPI(c)
	defer sysManager.Close()
	results, err := sysManager.ListBlockedModels()
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(results, jc.DeepEquals, []params.ModelBlockInfo{
		{
			Name:     "controller",
			UUID:     s.State.ModelUUID(),
			OwnerTag: s.AdminUserTag(c).String(),
			Blocks: []string{
				"BlockChange",
				"BlockDestroy",
			},
		},
	})
}

func (s *controllerSuite) TestRemoveBlocks(c *gc.C) {
	s.State.SwitchBlockOn(state.DestroyBlock, "TestBlockDestroyModel")
	s.State.SwitchBlockOn(state.ChangeBlock, "TestChangeBlock")

	sysManager := s.OpenAPI(c)
	defer sysManager.Close()
	err := sysManager.RemoveBlocks()
	c.Assert(err, jc.ErrorIsNil)

	blocks, err := s.State.AllBlocksForController()
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(blocks, gc.HasLen, 0)
}

func (s *controllerSuite) TestWatchAllModels(c *gc.C) {
	// The WatchAllModels infrastructure is comprehensively tested
	// else. This test just ensure that the API calls work end-to-end.
	sysManager := s.OpenAPI(c)
	defer sysManager.Close()

	w, err := sysManager.WatchAllModels()
	c.Assert(err, jc.ErrorIsNil)
	defer func() {
		err := w.Stop()
		c.Assert(err, jc.ErrorIsNil)
	}()

	deltasC := make(chan []multiwatcher.Delta)
	go func() {
		deltas, err := w.Next()
		c.Assert(err, jc.ErrorIsNil)
		deltasC <- deltas
	}()

	select {
	case deltas := <-deltasC:
		c.Assert(deltas, gc.HasLen, 1)
		modelInfo := deltas[0].Entity.(*multiwatcher.ModelInfo)

		env, err := s.State.Model()
		c.Assert(err, jc.ErrorIsNil)

		c.Assert(modelInfo.ModelUUID, gc.Equals, env.UUID())
		c.Assert(modelInfo.Name, gc.Equals, env.Name())
		c.Assert(modelInfo.Life, gc.Equals, multiwatcher.Life("alive"))
		c.Assert(modelInfo.Owner, gc.Equals, env.Owner().Id())
		c.Assert(modelInfo.ControllerUUID, gc.Equals, env.ControllerUUID())
	case <-time.After(testing.LongWait):
		c.Fatal("timed out")
	}
}

func (s *controllerSuite) TestAPIServerCanShutdownWithOutstandingNext(c *gc.C) {

	lis, err := net.Listen("tcp", "localhost:0")
	c.Assert(err, jc.ErrorIsNil)

	srv, err := apiserver.NewServer(s.State, lis, apiserver.ServerConfig{
		Cert:        []byte(testing.ServerCert),
		Key:         []byte(testing.ServerKey),
		Tag:         names.NewMachineTag("0"),
		DataDir:     c.MkDir(),
		LogDir:      c.MkDir(),
		NewObserver: func() observer.Observer { return &fakeobserver.Instance{} },
	})
	c.Assert(err, gc.IsNil)

	// Connect to the API server we've just started.
	apiInfo := s.APIInfo(c)
	apiInfo.Addrs = []string{lis.Addr().String()}
	apiInfo.ModelTag = names.ModelTag{}
	apiState, err := api.Open(apiInfo, api.DialOpts{})
	sysManager := controller.NewClient(apiState)
	defer sysManager.Close()

	w, err := sysManager.WatchAllModels()
	c.Assert(err, jc.ErrorIsNil)
	defer w.Stop()

	deltasC := make(chan struct{}, 2)
	go func() {
		defer close(deltasC)
		for {
			_, err := w.Next()
			if err != nil {
				return
			}
			deltasC <- struct{}{}
		}
	}()
	// Read the first event.
	select {
	case <-deltasC:
	case <-time.After(testing.LongWait):
		c.Fatal("timed out")
	}
	// Wait a little while for the Next call to actually arrive.
	time.Sleep(testing.ShortWait)

	// We should be able to close the server instance
	// even when there's an outstanding Next call.
	srvStopped := make(chan struct{})
	go func() {
		srv.Stop()
		close(srvStopped)
	}()

	select {
	case <-srvStopped:
	case <-time.After(testing.LongWait):
		c.Fatal("timed out waiting for server to stop")
	}

	// Check that the Next call has returned too.
	select {
	case _, ok := <-deltasC:
		if ok {
			c.Fatalf("got unexpected event from deltasC")
		}
	case <-time.After(testing.LongWait):
		c.Fatal("timed out")
	}
}

func (s *controllerSuite) TestModelStatus(c *gc.C) {
	sysManager := s.OpenAPI(c)
	defer sysManager.Close()
	modelTag := s.State.ModelTag()
	results, err := sysManager.ModelStatus(modelTag)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(results, jc.DeepEquals, []base.ModelStatus{{
		UUID:               modelTag.Id(),
		HostedMachineCount: 0,
		ServiceCount:       0,
		Owner:              "admin@local",
		Life:               params.Alive,
	}})
}

func (s *controllerSuite) TestGetControllerAccess(c *gc.C) {
	controller := s.OpenAPI(c)
	defer controller.Close()
	err := controller.GrantController("fred@external", "addmodel")
	c.Assert(err, jc.ErrorIsNil)
	access, err := controller.GetControllerAccess("fred@external")
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(access, gc.Equals, description.Access("addmodel"))
}

func (s *controllerSuite) TestInitiateMigration(c *gc.C) {
	st := s.Factory.MakeModel(c, nil)
	defer st.Close()

	_, err := st.LatestMigration()
	c.Assert(errors.IsNotFound(err), jc.IsTrue)

	spec := controller.MigrationSpec{
		ModelUUID:            st.ModelUUID(),
		TargetControllerUUID: randomUUID(),
		TargetAddrs:          []string{"1.2.3.4:5"},
		TargetCACert:         "cert",
		TargetUser:           "someone",
		TargetPassword:       "secret",
	}

	sysManager := s.OpenAPI(c)
	defer sysManager.Close()
	id, err := sysManager.InitiateMigration(spec)
	c.Assert(err, jc.ErrorIsNil)
	expectedId := st.ModelUUID() + ":0"
	c.Check(id, gc.Equals, expectedId)

	// Check database.
	mig, err := st.LatestMigration()
	c.Assert(err, jc.ErrorIsNil)
	c.Check(mig.Id(), gc.Equals, expectedId)
}

func (s *controllerSuite) TestInitiateMigrationError(c *gc.C) {
	spec := controller.MigrationSpec{
		ModelUUID:            randomUUID(), // Model doesn't exist.
		TargetControllerUUID: randomUUID(),
		TargetAddrs:          []string{"1.2.3.4:5"},
		TargetCACert:         "cert",
		TargetUser:           "someone",
		TargetPassword:       "secret",
	}

	sysManager := s.OpenAPI(c)
	defer sysManager.Close()
	id, err := sysManager.InitiateMigration(spec)
	c.Check(id, gc.Equals, "")
	c.Check(err, gc.ErrorMatches, "unable to read model: .+")
}

func (s *controllerSuite) TestInitiateMigrationWithMacaroon(c *gc.C) {
	st := s.Factory.MakeModel(c, nil)
	defer st.Close()

	mac, err := macaroon.New([]byte("secret"), "id", "location")
	c.Assert(err, jc.ErrorIsNil)
	macJSON, err := mac.MarshalJSON()
	c.Assert(err, jc.ErrorIsNil)

	spec := controller.MigrationSpec{
		ModelUUID:            st.ModelUUID(),
		TargetControllerUUID: randomUUID(),
		TargetAddrs:          []string{"1.2.3.4:5"},
		TargetCACert:         "cert",
		TargetUser:           "someone",
		TargetMacaroon:       string(macJSON),
	}

	sysManager := s.OpenAPI(c)
	defer sysManager.Close()
	_, err = sysManager.InitiateMigration(spec)
	c.Assert(err, jc.ErrorIsNil)

	// Check database.
	mig, err := st.LatestMigration()
	c.Assert(err, jc.ErrorIsNil)
	target, err := mig.TargetInfo()
	c.Assert(err, jc.ErrorIsNil)
	c.Check(target.Macaroon, jc.DeepEquals, mac)
}

func randomUUID() string {
	return utils.MustNewUUID().String()
}
