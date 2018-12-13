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

// IProposer decides which validator is the current proposer and illustrates
// what should be proposed if node is the current proposer
type IProposer interface {
	GetCurrentProposer() IPubValidator
	DecidesProposal() message.ProposedData
	// Each Validator will vote for the POLed proposal if there's any. Otherwise it
	// votes for the first proposal it sees in default unless user explicitly calls
	// BoundVotedData(data), in which case it votes for the bounded data.
	//BoundVotedData(data []byte)
}

// IValidators represents a validator group which contains all validators at
// a certain height. User should typically create a new IValidators and register
// it to the bft core before starting a new height consensus process if
// validators need to be updated
type IValidators interface {
	GetValidator(key message.PubKey) IPubValidator
	IsValidator(key message.PubKey) bool
	TotalVotingPower() int64

	IProposer
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

// Committer defines the actions the users taken when consensus is reached
type ICommitter interface {
	Commit(p message.ProposedData) error
}
