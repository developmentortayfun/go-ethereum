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
	"reflect"

	"github.com/ethereum/go-ethereum/consensus/pbft"
	"github.com/ethereum/go-ethereum/log"
)

func (c *core) sendPreprepare(request *pbft.Request) {
	logger := c.logger.New("state", c.state)
	nextSeqView := c.nextSequence()

	if c.isPrimary() {
		logger.Debug("sendPreprepare")
		c.broadcast(&pbft.Message{
			Code: pbft.MsgPreprepare,
			Msg: &pbft.Preprepare{
				View:     nextSeqView,
				Proposal: c.makeProposal(nextSeqView.Sequence, request),
			},
		})
	}
}

func (c *core) handlePreprepare(msg *pbft.Message, src pbft.Validator) error {
	logger := log.New("from", src.Address().Hex(), "state", c.state)
	logger.Debug("handlePreprepare")

	preprepare, ok := msg.Msg.(*pbft.Preprepare)
	if !ok {
		return errFailedDecodePreprepare
	}

	if c.isFutureMessage(pbft.MsgPreprepare, preprepare.View) {
		return errFutureMessage
	}

	if !c.backend.Validators().IsProposer(src.Address()) {
		logger.Warn("Ignore preprepare messages from non-proposer")
		return pbft.ErrNotFromProposer
	}

	if err := c.backend.Verify(preprepare.Proposal); err != nil {
		logger.Warn("Verify proposal failed")
		return err
	}

	view := c.nextSequence()
	if !reflect.DeepEqual(preprepare.View, view) {
		logger.Warn("Preprepare does not match", "expected", view, "got", preprepare.View)
		return pbft.ErrInvalidMessage
	}

	if preprepare.Proposal == nil {
		logger.Warn("Proposal is nil")
		return pbft.ErrNilProposal
	}

	if c.state == StateAcceptRequest {
		c.acceptPreprepare(preprepare)
		c.setState(StatePreprepared)
		c.sendPrepare()
	}

	return nil
}

func (c *core) acceptPreprepare(preprepare *pbft.Preprepare) {
	subject := &pbft.Subject{
		View:   preprepare.View,
		Digest: preprepare.Proposal.Header.DataHash.Bytes(),
	}

	c.subject = subject
	c.current = newSnapshot(preprepare)
	c.completed = false
}
