package gobft

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/coschain/gobft/common"
	"github.com/coschain/gobft/message"
	"github.com/pkg/errors"
)

/*
	VoteSet helps collect signatures from validators at each height+round for a
	predefined vote type.

	We need VoteSet to be able to keep track of conflicting votes when validators
	double-sign.  Yet, we can't keep track of *all* the votes seen, as that could
	be a DoS attack vector.

	NOTE: Assumes that the sum total of voting power does not exceed MaxUInt64.
*/
type VoteSet struct {
	height     int64
	round      int
	type_      message.VoteType
	validators *Validators
	base       message.ProposedData

	mtx                 sync.Mutex
	sum                 int64
	distinctVoter       int
	minorQuorum         message.ProposedData
	maj23               message.ProposedData // First 2/3 majority seen
	votesByProposedData map[message.ProposedData]*proposedDataVotes
	conflictingVotes    map[message.PubKey][]*message.Vote // TODO: track conflicting votes
}

// Constructs a new VoteSet struct used to accumulate votes for given height/round.
func NewVoteSet(height int64, round int, type_ message.VoteType, valSet *Validators, b *message.ProposedData) *VoteSet {
	if height == 0 {
		common.PanicSanity("Cannot make VoteSet for height == 0, doesn't make sense.")
	}
	return &VoteSet{
		height:              height,
		round:               round,
		type_:               type_,
		validators:          valSet,
		base:                *b,
		votesByProposedData: make(map[message.ProposedData]*proposedDataVotes),
		conflictingVotes:    make(map[message.PubKey][]*message.Vote),
	}
}

func (voteSet *VoteSet) Height() int64 {
	if voteSet == nil {
		return 0
	}
	return voteSet.height
}

func (voteSet *VoteSet) Round() int {
	if voteSet == nil {
		return -1
	}
	return voteSet.round
}

func (voteSet *VoteSet) Type() byte {
	if voteSet == nil {
		return 0x00
	}
	return byte(voteSet.type_)
}

//func (voteSet *VoteSet) Size() int {
//	if voteSet == nil {
//		return 0
//	}
//	return voteSet.valSet.Size()
//}

// Returns added=true if vote is valid and new.
// Otherwise returns err=ErrVote[
//		UnexpectedStep | InvalidIndex | InvalidAddress |
//		InvalidSignature | InvalidBlockHash | ConflictingVotes ]
// Duplicate votes return added=false, err=nil.
// Conflicting votes return added=*, err=ErrVoteConflictingVotes.
// NOTE: vote should not be mutated after adding.
// NOTE: VoteSet must not be nil
// NOTE: Vote must not be nil
func (voteSet *VoteSet) AddVote(vote *message.Vote) (added bool, err error) {
	if voteSet == nil {
		common.PanicSanity("AddVote() on nil VoteSet")
	}
	voteSet.mtx.Lock()
	defer voteSet.mtx.Unlock()

	return voteSet.addVote(vote)
}

// NOTE: Validates as much as possible before attempting to verify the signature.
func (voteSet *VoteSet) addVote(vote *message.Vote) (added bool, err error) {
	if vote == nil {
		return false, ErrVoteNil
	}
	if vote.Prev != voteSet.base {
		return false, ErrVoteMismatchedBase
	}

	// Make sure the step matches.
	if (vote.Height != voteSet.height) ||
		(vote.Round != voteSet.round) ||
		(vote.Type != voteSet.type_) {
		return false, errors.Wrapf(ErrVoteUnexpectedStep, "Expected %d/%d/%d, but got %d/%d/%d",
			voteSet.height, voteSet.round, voteSet.type_,
			vote.Height, vote.Round, vote.Type)
	}

	// If we already know of this vote, return false.
	if existing, ok := voteSet.getVote(&vote.Proposed, vote.Address); ok {
		if bytes.Equal(existing.Signature, vote.Signature) {
			return false, errors.New("duplicate vote") // duplicate
		}
		return false, errors.Wrapf(ErrVoteNonDeterministicSignature, "Existing vote: %v; New vote: %v", existing, vote)
	}

	// Check signature.

	if !voteSet.validators.VerifySignature(vote) {
		return false, errors.Wrapf(ErrVoteInvalidSignature, "Failed to verify vote with PubKey %s", vote.Address)
	}

	// Add vote and get conflicting vote if any.
	added, conflicting := voteSet.addVerifiedVote(vote, voteSet.validators.GetVotingPower(&vote.Address))
	if conflicting != nil {
		//return added, NewConflictingVoteError()
		return added, errors.New("conflicting vote")
	}
	if !added {
		common.PanicSanity("Expected to add non-conflicting vote")
	}
	return added, nil
}

// Returns (vote, true) if vote exists for valIndex and blockKey.
func (voteSet *VoteSet) getVote(pd *message.ProposedData, address message.PubKey) (vote *message.Vote, ok bool) {
	if pdvotes, ok := voteSet.votesByProposedData[*pd]; ok {
		if vote := pdvotes.getVote(address); vote != nil {
			return vote, true
		}
	}
	return nil, false
}

// Assumes signature is valid.
// If conflicting vote exists, returns it.
func (voteSet *VoteSet) addVerifiedVote(vote *message.Vote, votingPower int64) (added bool, conflicting *message.Vote) {
	byProposed, ok := voteSet.votesByProposedData[vote.Proposed]
	if ok {
		// TODO: check conflicting vote
	} else {
		byProposed = newProposedDataVotes()
		voteSet.votesByProposedData[vote.Proposed] = byProposed
	}

	// no conflict, add the vote
	// Before adding to votesByBlock, see if we'll exceed quorum
	origSum := byProposed.sum
	quorum := voteSet.validators.GetTotalVotingPower()*2/3 + 1

	// Add vote to votesByBlock
	byProposed.addVote(vote, votingPower)

	// If we just crossed the quorum threshold and have 2/3 majority...
	if origSum < quorum && quorum <= byProposed.sum {
		// Only consider the first quorum reached
		if voteSet.maj23 == message.NilData {
			voteSet.maj23 = vote.Proposed
		}
	}
	voteSet.sum += votingPower

	if voteSet.distinctVoter < len(byProposed.votes) {
		voteSet.distinctVoter = len(byProposed.votes)
		if voteSet.minorQuorum == message.NilData {
			voteSet.minorQuorum = vote.Proposed
		}
	}

	return true, conflicting
}

func (voteSet *VoteSet) HasTwoThirdsMajority() bool {
	if voteSet == nil {
		return false
	}
	voteSet.mtx.Lock()
	defer voteSet.mtx.Unlock()
	return voteSet.maj23 != message.NilData
}

func (voteSet *VoteSet) IsCommit() bool {
	if voteSet == nil {
		return false
	}
	if voteSet.type_ != message.PrecommitType {
		return false
	}
	voteSet.mtx.Lock()
	defer voteSet.mtx.Unlock()
	return voteSet.maj23 != message.NilData
}

func (voteSet *VoteSet) MinorQuorum() (message.ProposedData, bool) {
	if voteSet == nil {
		return message.NilData, false
	}
	voteSet.mtx.Lock()
	defer voteSet.mtx.Unlock()

	valNum := voteSet.validators.GetValidatorNum()
	if voteSet.distinctVoter > valNum*2/3 ||
		valNum < 4 && voteSet.distinctVoter >= (valNum+1)/2 {
		return voteSet.minorQuorum, true
	}

	return message.NilData, false
}

func (voteSet *VoteSet) HasTwoThirdsAny() bool {
	if voteSet == nil {
		return false
	}
	voteSet.mtx.Lock()
	defer voteSet.mtx.Unlock()
	return voteSet.sum > voteSet.validators.GetTotalVotingPower()*2/3
}

func (voteSet *VoteSet) HasAll() bool {
	voteSet.mtx.Lock()
	defer voteSet.mtx.Unlock()
	return voteSet.sum == voteSet.validators.GetTotalVotingPower()
}

// If there was a +2/3 majority for blockID, return blockID and true.
// Else, return the empty BlockID{} and false.
func (voteSet *VoteSet) TwoThirdsMajority() (proposed message.ProposedData, ok bool) {
	if voteSet == nil {
		return message.NilData, false
	}
	voteSet.mtx.Lock()
	defer voteSet.mtx.Unlock()
	if voteSet.maj23 != message.NilData {
		return voteSet.maj23, true
	}
	return message.NilData, false
}

func (voteSet *VoteSet) String() string {
	if voteSet == nil {
		return "nil-VoteSet"
	}
	return voteSet.StringIndented("")
}

func (voteSet *VoteSet) StringIndented(indent string) string {
	voteSet.mtx.Lock()
	defer voteSet.mtx.Unlock()

	voteStrings := make([]string, 0, common.ValNum)
	for _, proposedVotes := range voteSet.votesByProposedData {
		for _, vote := range proposedVotes.votes {
			if vote == nil {
				voteStrings = append(voteStrings, "nil-Vote")
			} else {
				voteStrings = append(voteStrings, vote.String())
			}
		}
	}

	return fmt.Sprintf(`VoteSet{
%s  H:%v R:%v T:%v
%s  %v
%s}`,
		indent, voteSet.height, voteSet.round, voteSet.type_,
		indent, strings.Join(voteStrings, "\n"+indent+"  "),
		indent)
}

// return the power voted, the total, and the fraction
func (voteSet *VoteSet) sumTotalFrac() (int64, int64, float64) {
	voted, total := voteSet.sum, voteSet.validators.GetTotalVotingPower()
	fracVoted := float64(voted) / float64(total)
	return voted, total, fracVoted
}

//--------------------------------------------------------------------------------
// Commit

func (voteSet *VoteSet) MakeCommit() *message.Commit {
	if voteSet.maj23 == message.NilData {
		common.PanicSanity("[MakeCommit] precommit doen't reach +2/3")
	}
	precommits := voteSet.votesByProposedData[voteSet.maj23].getAllVotes()
	return &message.Commit{
		ProposedData: voteSet.maj23,
		Precommits:   precommits,
		Prev:         precommits[0].Prev,
	}
}

func (voteSet *VoteSet) MakeFetchVotesReq() *message.FetchVotesReq {
	voters := make([]message.PubKey, 0, common.ValNum)
	for _, pd := range voteSet.votesByProposedData {
		for pk := range pd.votes {
			voters = append(voters, pk)
		}
	}
	return &message.FetchVotesReq{
		Type:   voteSet.type_,
		Height: voteSet.height,
		Round:  voteSet.round,
		Voters: voters,
		Time: time.Now(),
	}
}

func (voteSet *VoteSet) MakeFetchVotesRsp(req *message.FetchVotesReq) *message.FetchVotesRsp {
	votes := make([]*message.Vote, 0, common.ValNum)
	cache := make(map[message.PubKey]bool)
	for i := range req.Voters {
		cache[req.Voters[i]] = true
	}

	for _, pd := range voteSet.votesByProposedData {
		for pk := range pd.votes {
			if _, exist := cache[pk]; !exist {
				votes = append(votes, pd.votes[pk])
			}
		}
	}

	return &message.FetchVotesRsp{
		Type: voteSet.type_,
		Height: voteSet.height,
		Round: voteSet.round,
		MissingVotes: votes,
		Time: time.Now(),
	}
}

type proposedDataVotes struct {
	votes map[message.PubKey]*message.Vote
	sum   int64
}

func newProposedDataVotes() *proposedDataVotes {
	return &proposedDataVotes{
		votes: make(map[message.PubKey]*message.Vote),
	}
}

func (pd *proposedDataVotes) addVote(vote *message.Vote, power int64) {
	if _, ok := pd.votes[vote.Address]; ok {
		return
	}
	pd.votes[vote.Address] = vote
	pd.sum += power
	// TODO: check maj23
}

func (pd *proposedDataVotes) getVote(address message.PubKey) *message.Vote {
	if vote, ok := pd.votes[address]; ok {
		return vote
	}
	return nil
}

func (pd *proposedDataVotes) getAllVotes() []*message.Vote {
	ret := make([]*message.Vote, 0, len(pd.votes))
	for _, v := range pd.votes {
		ret = append(ret, v)
	}
	return ret
}
