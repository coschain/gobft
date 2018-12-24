package custom

import (
	"github.com/coschain/go-bft/message"
)

/*
 * A validator is a node in a distributed system that participates in the
 * bft consensus process. It proposes and votes for a certain proposal.
 * Each validator should maintain a set of all the PubValidators so that
 * it can verifies messages sent by other validators. Each validator should
 * have exactly one PrivValidator which contains its private key so that
 * it can sign a message. A validator can be a proposer, the rules of which
 * validator becomes a valid proposer at a certain time is totally decided by
 * user.
 */

// ICommittee represents a validator group which contains all validators at
// a certain height. User should typically create a new ICommittee and register
// it to the bft core before starting a new height consensus process if
// validators need to be updated
type ICommittee interface {
	GetValidator(key message.PubKey) IPubValidator
	IsValidator(key message.PubKey) bool
	TotalVotingPower() int64

	GetCurrentProposer() message.PubKey
	// DecidesProposal decides what will be proposed if this validator is the current
	// proposer. Other validators also used this function to decide what proposal they
	// will vote for, if the return value doesn't match the ProposedData of the proposal
	// they received from the current proposer, they prevote for nil
	DecidesProposal() message.ProposedData

	// Commit defines the actions the users taken when consensus is reached
	Commit(p message.ProposedData) error

	GetAppState() *message.AppState
	// BroadCast sends v to other validators
	BroadCast(v message.ConsensusMessage) error
}

// IPubValidator verifies if a message is properly signed by the right validator
type IPubValidator interface {
	VerifySig(digest, signature []byte) bool
	GetPubKey() message.PubKey
	GetVotingPower() int64
	SetVotingPower(int64)
}

// IPrivValidator signs a message
type IPrivValidator interface {
	GetPubKey() message.PubKey
	Sign(digest []byte) []byte
}
