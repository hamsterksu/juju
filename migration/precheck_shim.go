// Copyright 2016 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package migration

import (
	"github.com/juju/errors"
	"github.com/juju/version"

	"github.com/juju/juju/state"
)

// PrecheckShim wraps a *state.State to implement PrecheckBackend.
func PrecheckShim(st *state.State) PrecheckBackend {
	return &precheckShim{st}
}

// precheckShim is untested, but is simple enough to be verified by
// inspection.
type precheckShim struct {
	*state.State
}

// AgentVersion implements PrecheckBackend.
func (s *precheckShim) AgentVersion() (version.Number, error) {
	cfg, err := s.ModelConfig()
	if err != nil {
		return version.Zero, errors.Trace(err)
	}
	vers, ok := cfg.AgentVersion()
	if !ok {
		return version.Zero, errors.New("no model agent version")
	}
	return vers, nil
}

// AllMachines implements PrecheckBackend.
func (s *precheckShim) AllMachines() ([]PrecheckMachine, error) {
	machines, err := s.State.AllMachines()
	if err != nil {
		return nil, errors.Trace(err)
	}
	out := make([]PrecheckMachine, 0, len(machines))
	for _, machine := range machines {
		out = append(out, machine)
	}
	return out, nil
}
