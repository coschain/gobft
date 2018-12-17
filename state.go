package go_bft

import (
	"fmt"
	"time"

	"github.com/coschain/go-bft/message"
)

// RoundStepType enumerates the state of the consensus state machine
type RoundStepType uint8 // These must be numeric, ordered.

// RoundStepType
const (
	RoundStepNewHeight     = RoundStepType(0x01) // Wait til CommitTime + timeoutCommit
	RoundStepNewRound      = RoundStepType(0x02) // Setup new round and go to RoundStepPropose
	RoundStepPropose       = RoundStepType(0x03) // Did propose, gossip proposal
	RoundStepPrevote       = RoundStepType(0x04) // Did prevote, gossip prevotes
	RoundStepPrevoteWait   = RoundStepType(0x05) // Did receive any +2/3 prevotes, start timeout
	RoundStepPrecommit     = RoundStepType(0x06) // Did precommit, gossip precommits
	RoundStepPrecommitWait = RoundStepType(0x07) // Did receive any +2/3 precommits, start timeout
	RoundStepCommit        = RoundStepType(0x08) // Entered commit state machine
	// NOTE: RoundStepNewHeight acts as RoundStepCommitWait.

	// NOTE: Update IsValid method if you change this!
)

const (
	msgQueueSize = 1000
)

// msgs from the reactor which may update the state
type msgInfo struct {
	Msg message.ConsensusMessage `json:"msg"`
	//PeerID p2p.ID                  `json:"peer_key"`
}

// internally generated messages which may update the state
type timeoutInfo struct {
	Duration time.Duration `json:"duration"`
	Height   int64         `json:"height"`
	Round    int           `json:"round"`
	Step     RoundStepType `json:"step"`
}

func (ti *timeoutInfo) String() string {
	return fmt.Sprintf("%v ; %d/%d %v", ti.Duration, ti.Height, ti.Round, ti.Step)
}

// IsValid returns true if the step is valid, false if unknown/undefined.
func (rs RoundStepType) IsValid() bool {
	return uint8(rs) >= 0x01 && uint8(rs) <= 0x08
}

// String returns a string
func (rs RoundStepType) String() string {
	switch rs {
	case RoundStepNewHeight:
		return "RoundStepNewHeight"
	case RoundStepNewRound:
		return "RoundStepNewRound"
	case RoundStepPropose:
		return "RoundStepPropose"
	case RoundStepPrevote:
		return "RoundStepPrevote"
	case RoundStepPrevoteWait:
		return "RoundStepPrevoteWait"
	case RoundStepPrecommit:
		return "RoundStepPrecommit"
	case RoundStepPrecommitWait:
		return "RoundStepPrecommitWait"
	case RoundStepCommit:
		return "RoundStepCommit"
	default:
		return "RoundStepUnknown" // Cannot panic.
	}
}

// RoundState defines the internal consensus state.
// NOTE: Not thread safe. Should only be manipulated by functions downstream
// of the cs.receiveRoutine
type RoundState struct {
	Height     int64
	Round      int
	Step       RoundStepType
	StartTime  time.Time
	CommitTime time.Time // Subjective time when +2/3 precommits for Block at Round were found

	Proposal       *message.Vote
	LockedRound    int
	LockedProposal *message.Vote
	Votes          *HeightVoteSet
	CommitRound    int
	LastCommit     *VoteSet // Last precommits at Height-1
	//LastValidators []message.PubKey
}

// String returns a string
func (rs *RoundState) String() string {
	return rs.StringIndented("")
}

// StringIndented returns a string
func (rs *RoundState) StringIndented(indent string) string {
	return fmt.Sprintf(`RoundState{
%s  H:%v R:%v S:%v
%s  StartTime:     %v
%s  CommitTime:    %v
%s  Proposal:      %v
%s  LockedRound:   %v
%s  LockedProposal:   %v
%s  Votes:         %v
//%s  LastCommit:    %v
%s}`,
		indent, rs.Height, rs.Round, rs.Step,
		indent, rs.StartTime,
		indent, rs.CommitTime,
		indent, rs.Proposal,
		indent, rs.LockedRound,
		indent, rs.LockedProposal.String(),
		indent, rs.Votes.StringIndented(indent+"  "),
		//		indent, rs.LastCommit.String(),
		indent)
}

// StringShort returns a string
func (rs *RoundState) StringShort() string {
	return fmt.Sprintf(`RoundState{H:%v R:%v S:%v ST:%v}`,
		rs.Height, rs.Round, rs.Step, rs.StartTime)
}
