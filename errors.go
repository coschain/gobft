package gobft

import (
	"github.com/pkg/errors"
)

var (
	ErrVoteUnexpectedStep            = errors.New("Unexpected step")
	ErrVoteInvalidValidatorIndex     = errors.New("Invalid validator index")
	ErrVoteInvalidValidatorAddress   = errors.New("Invalid validator address")
	ErrVoteInvalidSignature          = errors.New("Invalid signature")
	ErrVoteInvalidBlockHash          = errors.New("Invalid block hash")
	ErrVoteNonDeterministicSignature = errors.New("Non-deterministic signature")
	ErrVoteNil                       = errors.New("Nil vote")
	ErrVoteMismatchedBase			 = errors.New("Invalid base")
)

var (
	ErrInvalidProposer          = errors.New("Error invalid proposer")
	ErrInvalidProposalSignature = errors.New("Error invalid proposal signature")
	ErrInvalidProposalPOLRound  = errors.New("Error invalid proposal POL round")
	ErrAddingVote               = errors.New("Error adding vote")
	ErrVoteHeightMismatch       = errors.New("Error vote height mismatch")
)

type ErrVoteConflictingVotes struct {
	//*DuplicateVoteEvidence
}

func (err *ErrVoteConflictingVotes) Error() string {
	//return fmt.Sprintf("Conflicting votes from validator %v", err.PubKey.Address())
	return ""
}

func NewConflictingVoteError( /*val *Validator, voteA, voteB *Vote*/ ) *ErrVoteConflictingVotes {
	return &ErrVoteConflictingVotes{
		//&DuplicateVoteEvidence{
		//	PubKey: val.PubKey,
		//	VoteA:  voteA,
		//	VoteB:  voteB,
		//},
	}
}
