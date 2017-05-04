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
	"math/big"
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/consensus/pbft"
	"github.com/ethereum/go-ethereum/consensus/pbft/validator"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestHandlePrepare(t *testing.T) {
	N := uint64(4)
	F := uint64(1)

	expectedSubject := &pbft.Subject{
		View: &pbft.View{
			ViewNumber: big.NewInt(0),
			Sequence:   big.NewInt(0)},
		Digest: []byte{1},
	}

	testCases := []struct {
		system *testSystem

		expectedErr error
	}{
		{
			// normal case
			func() *testSystem {
				sys := NewTestSystemWithBackend(N, F)

				for i, backend := range sys.backends {
					c := backend.engine.(*core)
					c.subject = expectedSubject

					if i == 0 {
						// replica 0 is primary
						c.state = StatePreprepared
					}
				}
				return sys
			}(),
			nil,
		},
		{
			// future message
			func() *testSystem {
				sys := NewTestSystemWithBackend(N, F)

				for i, backend := range sys.backends {
					c := backend.engine.(*core)

					if i == 0 {
						// replica 0 is primary
						c.subject = expectedSubject
						c.state = StatePreprepared
					} else {
						c.subject = &pbft.Subject{
							View: &pbft.View{
								ViewNumber: big.NewInt(2),
								Sequence:   big.NewInt(3)},
							Digest: []byte{1},
						}
					}
				}
				return sys
			}(),
			errFutureMessage,
		},
		{
			// subject not match
			func() *testSystem {
				sys := NewTestSystemWithBackend(N, F)

				for i, backend := range sys.backends {
					c := backend.engine.(*core)

					if i == 0 {
						// replica 0 is primary
						c.subject = expectedSubject
						c.state = StatePreprepared
					} else {
						c.subject = &pbft.Subject{
							View: &pbft.View{
								ViewNumber: big.NewInt(0),
								Sequence:   big.NewInt(0)},
							Digest: []byte{2, 3, 4},
						}
					}
				}
				return sys
			}(),
			pbft.ErrSubjectNotMatched,
		},
		{
			// less than 2F+1
			func() *testSystem {
				sys := NewTestSystemWithBackend(N, F)

				// save less than 2*F+1 replica
				sys.backends = sys.backends[2*int(F)+1:]

				for i, backend := range sys.backends {
					c := backend.engine.(*core)
					c.subject = expectedSubject

					if i == 0 {
						// replica 0 is primary
						c.state = StatePreprepared
					}
				}
				return sys
			}(),
			nil,
		},
		// TODO: double send message
	}

OUTER:
	for _, test := range testCases {
		test.system.Run(true, false)

		v0 := test.system.backends[0]
		r0 := v0.engine.(*core)

		for i, v := range test.system.backends {
			if err := r0.handlePrepare(v.engine.(*core).subject, v.Validators().GetByIndex(uint64(i))); err != nil {
				if err != test.expectedErr {
					t.Error("unexpected error: ", err)
				}
				continue OUTER
			}
		}

		// prepared is normal case
		if r0.state != StatePrepared {
			// There are not enough prepared messages in core
			if r0.state != StatePreprepared {
				t.Error("state should be preprepared")
			}
			if int64(r0.current.Prepares.Size()) > 2*r0.F {
				t.Error("prepare messages size should less than ", 2*r0.F+1)
			}

			continue
		}

		// core should have 2F+1 prepare messages
		if int64(r0.current.Prepares.Size()) <= 2*r0.F {
			t.Error("prepare messages size should greater than 2F+1, size:", r0.current.Prepares.Size())
		}

		// a message will be delivered to backend if 2F+1
		if int64(len(v0.sentMsgs)) != 1 {
			t.Error("the Send() should be called once, got:", len(test.system.backends[0].sentMsgs))
		}

		// verify commit messages
		decodedMsg, err := pbft.Decode(v0.sentMsgs[0], nil)
		if err != nil {
			t.Error("failed to parse")
		}

		if decodedMsg.Code != pbft.MsgCommit {
			t.Error("message code is not the same")
		}
		m, ok := decodedMsg.Msg.(*pbft.Subject)
		if !ok {
			t.Error("failed to decode Prepare")
		}
		if !reflect.DeepEqual(m, expectedSubject) {
			t.Error("subject should be the same")
		}
	}
}

// view number is not checked for now
func TestVerifyPrepare(t *testing.T) {
	// for log purpose
	privateKey, _ := crypto.GenerateKey()
	peer := validator.New(getPublicKeyAddress(privateKey))

	sys := NewTestSystemWithBackend(uint64(1), uint64(0))

	testCases := []struct {
		expected error

		prepare *pbft.Subject
		self    *pbft.Subject
	}{
		{
			// normal case
			expected: nil,
			prepare: &pbft.Subject{
				View:   &pbft.View{ViewNumber: big.NewInt(0), Sequence: big.NewInt(0)},
				Digest: []byte{1},
			},
			self: &pbft.Subject{
				View:   &pbft.View{ViewNumber: big.NewInt(0), Sequence: big.NewInt(0)},
				Digest: []byte{1},
			},
		},
		{
			// old message
			expected: pbft.ErrOldMessage,
			prepare: &pbft.Subject{
				View:   &pbft.View{ViewNumber: big.NewInt(0), Sequence: big.NewInt(0)},
				Digest: []byte{1},
			},
			self: &pbft.Subject{
				View:   &pbft.View{ViewNumber: big.NewInt(1), Sequence: big.NewInt(1)},
				Digest: []byte{1},
			},
		},
		{
			// malicious package(lack of sequence)
			expected: pbft.ErrSubjectNotMatched,
			prepare: &pbft.Subject{
				View:   &pbft.View{ViewNumber: big.NewInt(0), Sequence: nil},
				Digest: []byte{1},
			},
			self: &pbft.Subject{
				View:   &pbft.View{ViewNumber: big.NewInt(1), Sequence: big.NewInt(1)},
				Digest: []byte{1},
			},
		},
		{
			// wrong prepare message with same sequence but different view number
			expected: pbft.ErrSubjectNotMatched,
			prepare: &pbft.Subject{
				View:   &pbft.View{ViewNumber: big.NewInt(1), Sequence: big.NewInt(0)},
				Digest: []byte{1},
			},
			self: &pbft.Subject{
				View:   &pbft.View{ViewNumber: big.NewInt(0), Sequence: big.NewInt(0)},
				Digest: []byte{1},
			},
		},
		{
			// wrong prepare message with same view number but different sequence
			expected: pbft.ErrSubjectNotMatched,
			prepare: &pbft.Subject{
				View:   &pbft.View{ViewNumber: big.NewInt(0), Sequence: big.NewInt(1)},
				Digest: []byte{1},
			},
			self: &pbft.Subject{
				View:   &pbft.View{ViewNumber: big.NewInt(0), Sequence: big.NewInt(0)},
				Digest: []byte{1},
			},
		},
	}
	for i, test := range testCases {
		c := sys.backends[0].engine.(*core)
		c.subject = test.self

		if err := c.verifyPrepare(test.prepare, peer); err != nil {
			if err != test.expected {
				t.Errorf("expected result is not the same (%d), err:%v", i, err)
			}
		}
	}
}
