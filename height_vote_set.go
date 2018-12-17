package go_bft

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/coschain/go-bft/common"
	"github.com/coschain/go-bft/message"
)

type RoundVoteSet struct {
	Prevotes   *VoteSet
	Precommits *VoteSet
}

var (
	GotVoteFromUnwantedRoundError = errors.New("Peer has sent a vote that does not match our round for more than one round")
)

/*
Keeps track of all VoteSets from round 0 to round 'round'.

Also keeps track of up to one RoundVoteSet greater than
'round' from each peer, to facilitate catchup syncing of commits.

A commit is +2/3 precommits for a block at a round,
but which round is not known in advance, so when a peer
provides a precommit for a round greater than mtx.round,
we create a new entry in roundVoteSets but also remember the
peer to prevent abuse.
*/
type HeightVoteSet struct {
	height int64
	valSet *Validators

	mtx           sync.Mutex
	round         int                  // max tracked round
	roundVoteSets map[int]RoundVoteSet // keys: [0...round]
}

func NewHeightVoteSet(height int64, valSet *Validators) *HeightVoteSet {
	hvs := &HeightVoteSet{}
	hvs.Reset(height, valSet)
	return hvs
}

func (hvs *HeightVoteSet) Reset(height int64, valSet *Validators) {
	hvs.mtx.Lock()
	defer hvs.mtx.Unlock()

	hvs.height = height
	hvs.valSet = valSet
	hvs.roundVoteSets = make(map[int]RoundVoteSet)

	hvs.addRound(0)
	hvs.round = 0
}

func (hvs *HeightVoteSet) Height() int64 {
	hvs.mtx.Lock()
	defer hvs.mtx.Unlock()
	return hvs.height
}

func (hvs *HeightVoteSet) Round() int {
	hvs.mtx.Lock()
	defer hvs.mtx.Unlock()
	return hvs.round
}

// Create more RoundVoteSets up to round.
func (hvs *HeightVoteSet) SetRound(round int) {
	hvs.mtx.Lock()
	defer hvs.mtx.Unlock()
	if hvs.round != 0 && (round < hvs.round+1) {
		common.PanicSanity("SetRound() must increment hvs.round")
	}
	for r := hvs.round + 1; r <= round; r++ {
		if _, ok := hvs.roundVoteSets[r]; ok {
			continue // Already exists because peerCatchupRounds.
		}
		hvs.addRound(r)
	}
	hvs.round = round
}

func (hvs *HeightVoteSet) addRound(round int) {
	if _, ok := hvs.roundVoteSets[round]; ok {
		common.PanicSanity("addRound() for an existing round")
	}
	// log.Debug("addRound(round)", "round", round)
	prevotes := NewVoteSet(hvs.height, round, message.PrevoteType, hvs.valSet)
	precommits := NewVoteSet(hvs.height, round, message.PrecommitType, hvs.valSet)
	hvs.roundVoteSets[round] = RoundVoteSet{
		Prevotes:   prevotes,
		Precommits: precommits,
	}
}

// Duplicate votes return added=false, err=nil.
// By convention, peerID is "" if origin is self.
func (hvs *HeightVoteSet) AddVote(vote *message.Vote) (added bool, err error) {
	hvs.mtx.Lock()
	defer hvs.mtx.Unlock()
	if !message.IsVoteTypeValid(vote.Type) {
		return
	}
	voteSet := hvs.getVoteSet(vote.Round, vote.Type)
	if voteSet == nil {
		hvs.addRound(vote.Round)
		voteSet = hvs.getVoteSet(vote.Round, vote.Type)
		// TODO: valid catchup rounds?
	}
	added, err = voteSet.AddVote(vote)
	return
}

func (hvs *HeightVoteSet) Prevotes(round int) *VoteSet {
	hvs.mtx.Lock()
	defer hvs.mtx.Unlock()
	return hvs.getVoteSet(round, message.PrevoteType)
}

func (hvs *HeightVoteSet) Precommits(round int) *VoteSet {
	hvs.mtx.Lock()
	defer hvs.mtx.Unlock()
	return hvs.getVoteSet(round, message.PrecommitType)
}

// Last round and blockID that has +2/3 prevotes for a particular block or nil.
// Returns -1 if no such round exists.
func (hvs *HeightVoteSet) POLInfo() (polRound int, polData message.ProposedData) {
	hvs.mtx.Lock()
	defer hvs.mtx.Unlock()
	for r := hvs.round; r >= 0; r-- {
		rvs := hvs.getVoteSet(r, message.PrevoteType)
		polData, ok := rvs.TwoThirdsMajority()
		if ok {
			return r, polData
		}
	}
	return -1, message.NilData
}

func (hvs *HeightVoteSet) getVoteSet(round int, type_ message.VoteType) *VoteSet {
	rvs, ok := hvs.roundVoteSets[round]
	if !ok {
		return nil
	}
	switch type_ {
	case message.PrevoteType:
		return rvs.Prevotes
	case message.PrecommitType:
		return rvs.Precommits
	default:
		common.PanicSanity(fmt.Sprintf("Unexpected vote type %X", type_))
		return nil
	}
}

func (hvs *HeightVoteSet) String() string {
	return hvs.StringIndented("")
}

func (hvs *HeightVoteSet) StringIndented(indent string) string {
	hvs.mtx.Lock()
	defer hvs.mtx.Unlock()
	vsStrings := make([]string, 0, (len(hvs.roundVoteSets)+1)*2)
	// rounds 0 ~ hvs.round inclusive
	for round := 0; round <= hvs.round; round++ {
		voteSetString := hvs.roundVoteSets[round].Prevotes.String()
		vsStrings = append(vsStrings, voteSetString)
		voteSetString = hvs.roundVoteSets[round].Precommits.String()
		vsStrings = append(vsStrings, voteSetString)
	}
	// all other peer catchup rounds
	for round, roundVoteSet := range hvs.roundVoteSets {
		if round <= hvs.round {
			continue
		}
		voteSetString := roundVoteSet.Prevotes.String()
		vsStrings = append(vsStrings, voteSetString)
		voteSetString = roundVoteSet.Precommits.String()
		vsStrings = append(vsStrings, voteSetString)
	}
	return fmt.Sprintf(`HeightVoteSet{H:%v R:0~%v
%s  %v
%s}`,
		hvs.height, hvs.round,
		indent, strings.Join(vsStrings, "\n"+indent+"  "),
		indent)
}
