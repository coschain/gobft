package message

import (
	"fmt"

	"github.com/coschain/go-bft/common"
	"github.com/tendermint/go-amino"
)

const maxMsgSize = 1024 * 1024

var cdc = amino.NewCodec()

func init() {
	RegisterConsensusMessages(cdc)
}

// ConsensusMessage is a message that can be sent and received on the ConsensusReactor
type ConsensusMessage interface {
	ValidateBasic() error
}

func RegisterConsensusMessages(cdc *amino.Codec) {
	cdc.RegisterInterface((*ConsensusMessage)(nil), nil)
	//cdc.RegisterConcrete(&NewRoundStepMessage{}, "tendermint/NewRoundStepMessage", nil)
	//cdc.RegisterConcrete(&NewValidBlockMessage{}, "tendermint/NewValidBlockMessage", nil)
	//cdc.RegisterConcrete(&ProposalMessage{}, "tendermint/Proposal", nil)
	//cdc.RegisterConcrete(&ProposalPOLMessage{}, "tendermint/ProposalPOL", nil)
	//cdc.RegisterConcrete(&BlockPartMessage{}, "tendermint/BlockPart", nil)
	//cdc.RegisterConcrete(&VoteMessage{}, "tendermint/Vote", nil)
	//cdc.RegisterConcrete(&HasVoteMessage{}, "tendermint/HasVote", nil)
	//cdc.RegisterConcrete(&VoteSetMaj23Message{}, "tendermint/VoteSetMaj23", nil)
	//cdc.RegisterConcrete(&VoteSetBitsMessage{}, "tendermint/VoteSetBits", nil)
	//cdc.RegisterConcrete(&ProposalHeartbeatMessage{}, "tendermint/ProposalHeartbeat", nil)
}

func decodeMsg(bz []byte) (msg ConsensusMessage, err error) {
	if len(bz) > maxMsgSize {
		return msg, fmt.Errorf("Msg exceeds max size (%d > %d)", len(bz), maxMsgSize)
	}
	err = cdc.UnmarshalBinaryBare(bz, &msg)
	return
}

// cdcEncode returns nil if the input is nil, otherwise returns
// cdc.MustMarshalBinaryBare(item)
func cdcEncode(item interface{}) []byte {
	if item != nil && !common.IsTypedNil(item) && !common.IsEmpty(item) {
		return cdc.MustMarshalBinaryBare(item)
	}
	return nil
}

// VoteMessage is sent when voting for a proposal (or lack thereof).
type VoteMessage struct {
	Vote *Vote
}

// ValidateBasic performs basic validation.
func (m *VoteMessage) ValidateBasic() error {
	return m.Vote.ValidateBasic()
}

// String returns a string representation.
func (m *VoteMessage) String() string {
	return fmt.Sprintf("[Vote %v]", m.Vote)
}
