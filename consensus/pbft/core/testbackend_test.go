// Copyright 2017 AMIS Technologies
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package core

import (
	"crypto/ecdsa"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/pbft"
	"github.com/ethereum/go-ethereum/consensus/pbft/validator"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/event"
	elog "github.com/ethereum/go-ethereum/log"
)

var testLogger = elog.New()

type testSystemBackend struct {
	id  uint64
	sys *testSystem

	engine Engine
	peers  pbft.ValidatorSet
	events *event.TypeMux

	commitMsgs []*pbft.Proposal
	sentMsgs   [][]byte // store the message when Send is called by core

	address common.Address
}

// ==============================================
//
// define the functions that needs to be provided for PBFT.

func (self *testSystemBackend) Address() common.Address {
	return self.address
}

// Peers returns all connected peers
func (self *testSystemBackend) Validators() pbft.ValidatorSet {
	return self.peers
}

func (self *testSystemBackend) EventMux() *event.TypeMux {
	return self.events
}

func (self *testSystemBackend) Send(message []byte, target common.Address) error {
	testLogger.Info("enqueuing a message...", "address", self.Address())
	self.sentMsgs = append(self.sentMsgs, message)
	self.sys.queuedMessage <- pbft.MessageEvent{
		Payload: message,
	}
	return nil
}

func (self *testSystemBackend) Broadcast(message []byte) error {
	testLogger.Info("enqueuing a message...", "address", self.Address())
	self.sentMsgs = append(self.sentMsgs, message)
	self.sys.queuedMessage <- pbft.MessageEvent{
		Payload: message,
	}
	return nil
}

func (self *testSystemBackend) UpdateState(state *pbft.State) error {
	testLogger.Warn("nothing to happen")
	return nil
}

func (self *testSystemBackend) ViewChanged(needNewProposal bool) error {
	testLogger.Warn("nothing to happen")
	return nil
}

func (self *testSystemBackend) Commit(proposal *pbft.Proposal) error {
	testLogger.Info("commit message", "address", self.Address())
	self.commitMsgs = append(self.commitMsgs, proposal)

	// fake new head events
	go self.events.Post(pbft.FinalCommittedEvent{
		BlockNumber: proposal.RequestContext.Number(),
		BlockHash:   proposal.RequestContext.Hash(),
	})
	return nil
}

func (self *testSystemBackend) Verify(proposal *pbft.Proposal) error {
	return nil
}

func (self *testSystemBackend) Sign(data []byte) ([]byte, error) {
	testLogger.Warn("not sign any data")
	return data, nil
}

func (self *testSystemBackend) CheckSignature([]byte, common.Address, []byte) error {
	return nil
}

func (self *testSystemBackend) CheckValidatorSignature(data []byte, sig []byte) (common.Address, error) {
	return common.Address{}, nil
}

func (self *testSystemBackend) IsProposer() bool {
	testLogger.Info("use replica 0 as proposer")
	if len(self.sys.backends) == 0 {
		return false
	}
	return self.Address() == self.sys.backends[0].Address()
}

func (self *testSystemBackend) Hash(b interface{}) common.Hash {
	return common.StringToHash("Test")
}
func (self *testSystemBackend) Encode(b interface{}) ([]byte, error) {
	return []byte(""), nil

}
func (self *testSystemBackend) Decode([]byte, interface{}) error {
	return nil
}

func (self *testSystemBackend) NewRequest(request pbft.RequestContexter) {
	go self.events.Post(pbft.RequestEvent{
		BlockContext: request,
	})
}

// ==============================================
//
// define the struct that need to be provided for DB manager.

// Save an object into db
func (self *testSystemBackend) Save(key string, val interface{}) error {
	testLogger.Warn("nothing to happen")
	return nil
}

// Restore an object to val from db
func (self *testSystemBackend) Restore(key string, val interface{}) error {
	testLogger.Warn("nothing to happen")
	return nil
}

// ==============================================
//
// define the struct that need to be provided for integration tests.

type testSystem struct {
	backends []*testSystemBackend

	queuedMessage chan pbft.MessageEvent
	quit          chan struct{}
}

func newTestSystem(n uint64) *testSystem {
	testLogger.SetHandler(elog.StdoutHandler)
	return &testSystem{
		backends: make([]*testSystemBackend, n),

		queuedMessage: make(chan pbft.MessageEvent),
		quit:          make(chan struct{}),
	}
}

func newTestValidatorSet(n int) pbft.ValidatorSet {
	// generate validators
	validators := make([]pbft.Validator, n)
	b := []byte{}
	for i := 0; i < n; i++ {
		// TODO: the private key should be stored if we want to add new feature for sign data
		privateKey, _ := crypto.GenerateKey()
		validators[i] = validator.New(crypto.PubkeyToAddress(privateKey.PublicKey))
		b = append(b, validators[i].Address().Bytes()...)
	}
	vset := validator.NewSet(validator.ExtractValidators(b))

	return vset
}

// FIXME: int64 is needed for N and F
func NewTestSystemWithBackend(n, f uint64) *testSystem {
	testLogger.SetHandler(elog.StdoutHandler)

	vset := newTestValidatorSet(int(n))
	sys := newTestSystem(n)
	config := pbft.DefaultConfig

	for i := uint64(0); i < n; i++ {
		backend := sys.NewBackend(i)
		backend.peers = vset
		backend.address = vset.GetByIndex(i).Address()

		core := New(backend, config.BlockPeriod).(*core)
		core.current = newSnapshot(&pbft.Preprepare{
			View:     &pbft.View{},
			Proposal: &pbft.Proposal{},
		}, vset)
		core.logger = testLogger
		core.N = int64(n)
		core.F = int64(f)

		backend.engine = core
	}

	return sys
}

// listen will consume messages from queue and deliver a message to core
func (t *testSystem) listen() {
	for {
		select {
		case <-t.quit:
			return
		case queuedMessage := <-t.queuedMessage:
			testLogger.Info("consuming a queue message...")
			for _, backend := range t.backends {
				go backend.EventMux().Post(queuedMessage)
			}
		}
	}
}

// Run will start system components based on given flag, and returns a closer
// function that caller can control lifecycle
//
// Given a true for core if you want to initialize core engine.
func (t *testSystem) Run(core bool) func() {
	for _, b := range t.backends {
		if core {
			b.engine.Start() // start PBFT core
		}
	}

	go t.listen()
	closer := func() { t.stop(core) }
	return closer
}

func (t *testSystem) stop(core bool) {
	close(t.quit)

	for _, b := range t.backends {
		if core {
			b.engine.Stop()
		}
	}
}

func (t *testSystem) NewBackend(id uint64) *testSystemBackend {
	backend := &testSystemBackend{
		id:     id,
		sys:    t,
		events: new(event.TypeMux),
	}

	t.backends[id] = backend
	return backend
}

// ==============================================
//
// helper functions.

func getPublicKeyAddress(privateKey *ecdsa.PrivateKey) common.Address {
	return crypto.PubkeyToAddress(privateKey.PublicKey)
}
