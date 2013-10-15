// Copyright 2012, 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package bootstrap

import (
	"fmt"

	"launchpad.net/loggo"

	"launchpad.net/juju-core/agent"
	"launchpad.net/juju-core/constraints"
	"launchpad.net/juju-core/environs"
	"launchpad.net/juju-core/instance"
	"launchpad.net/juju-core/state"
	coretools "launchpad.net/juju-core/tools"
	"launchpad.net/juju-core/version"
)

var logger = loggo.GetLogger("juju.environs.bootstrap")

// Bootstrap bootstraps the given environment. The supplied constraints are
// used to provision the instance, and are also set within the bootstrapped
// environment.
func Bootstrap(environ environs.Environ, cons constraints.Value) error {
	cfg := environ.Config()
	if secret := cfg.AdminSecret(); secret == "" {
		return fmt.Errorf("environment configuration has no admin-secret")
	}
	if authKeys := cfg.AuthorizedKeys(); authKeys == "" {
		// Apparently this can never happen, so it's not tested. But, one day,
		// Config will act differently (it's pretty crazy that, AFAICT, the
		// authorized-keys are optional config settings... but it's impossible
		// to actually *create* a config without them)... and when it does,
		// we'll be here to catch this problem early.
		return fmt.Errorf("environment configuration has no authorized-keys")
	}
	if _, hasCACert := cfg.CACert(); !hasCACert {
		return fmt.Errorf("environment configuration has no ca-cert")
	}
	if _, hasCAKey := cfg.CAPrivateKey(); !hasCAKey {
		return fmt.Errorf("environment configuration has no ca-private-key")
	}
	// Write out the bootstrap-init file, and confirm storage is writeable.
	if err := environs.VerifyStorage(environ.Storage()); err != nil {
		return err
	}
	logger.Infof("bootstrapping environment %q", environ.Name())
	return environ.Bootstrap(cons)
}

// SelectBootstrapTools returns the newest tools from the given tools list,
// and update the agent-version configuration attribute.
func SelectBootstrapTools(environ environs.Environ, toolsList coretools.List) (coretools.List, error) {
	if len(toolsList) == 0 {
		return nil, fmt.Errorf("no bootstrap tools available")
	}
	var newVersion version.Number
	newVersion, toolsList = toolsList.Newest()
	logger.Infof("picked newest version: %s", newVersion)
	cfg := environ.Config()
	if agentVersion, _ := cfg.AgentVersion(); agentVersion != newVersion {
		cfg, err := cfg.Apply(map[string]interface{}{
			"agent-version": newVersion.String(),
		})
		if err == nil {
			err = environ.SetConfig(cfg)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to update environment configuration: %v", err)
		}
	}
	return toolsList, nil
}

// ConfigureBootstrapMachine adds the initial machine into state.  As a part
// of this process the environmental constraints are saved as constraints used
// when bootstrapping are considered constraints for the entire environment.
func ConfigureBootstrapMachine(
	st *state.State,
	cons constraints.Value,
	datadir string,
	jobs []state.MachineJob,
	instId instance.Id,
	characteristics instance.HardwareCharacteristics,
) error {
	logger.Debugf("setting environment constraints")
	if err := st.SetEnvironConstraints(cons); err != nil {
		return err
	}

	logger.Debugf("create bootstrap machine in state")
	m, err := st.InjectMachine(&state.AddMachineParams{
		Series:                  version.Current.Series,
		Nonce:                   state.BootstrapNonce,
		Constraints:             cons,
		InstanceId:              instId,
		HardwareCharacteristics: characteristics,
		Jobs: jobs,
	})
	if err != nil {
		return err
	}
	// Read the machine agent's password and change it to
	// a new password (other agents will change their password
	// via the API connection).
	logger.Debugf("create new random password for machine %v", m.Id())
	mconf, err := agent.ReadConf(datadir, m.Tag())
	if err != nil {
		return err
	}
	newPassword, err := mconf.GenerateNewPassword()
	if err != nil {
		return err
	}
	if err := m.SetMongoPassword(newPassword); err != nil {
		return err
	}
	if err := m.SetPassword(newPassword); err != nil {
		return err
	}
	return nil
}
