package message

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/coschain/gobft/common"
)

// PubKey is string representation of the public key
type PubKey string

type ProposedData [32]byte

var NilData ProposedData

func (pd ProposedData) IsNil() bool {
	return pd == NilData
}

// VoteType is a type of signed message in the consensus.
type VoteType byte

const (
	// Votes
	PrevoteType   VoteType = 0x01
	PrecommitType VoteType = 0x02

	// Proposals
	ProposalType VoteType = 0x20
)

// IsVoteTypeValid returns true if t is a valid vote type.
func IsVoteTypeValid(t VoteType) bool {
	switch t {
	case PrevoteType, PrecommitType, ProposalType:
		return true
	default:
		return false
	}
}

// Vote represents a prevote, precommit from validators for
// consensus.
type Vote struct {
	Type      VoteType     `json:"type"`
	Height    int64        `json:"height"`
	Round     int          `json:"round"`
	Timestamp time.Time    `json:"timestamp"`
	Proposed  ProposedData `json:"proposed_data"` // zero if vote is nil.
	Address   PubKey       `json:"pub_key"`
	Signature []byte       `json:"signature"`
}

func NewVote(t VoteType, height int64, round int, proposed *ProposedData) *Vote {
	return &Vote{
		Type:      t,
		Height:    height,
		Round:     round,
		Timestamp: common.Now(),
		Proposed:  *proposed,
	}
}

func (v *Vote) SetSigner(key PubKey) {
	v.Address = key
}

func (v *Vote) GetSigner() PubKey {
	return v.Address
}

func (v *Vote) SetSignature(sig []byte) {
	v.Signature = sig
}

func (v *Vote) GetSignature() []byte {
	return v.Signature
}

func (v *Vote) Digest() []byte {
	buf := &bytes.Buffer{}
	binary.Write(buf, binary.BigEndian, v.Type)
	binary.Write(buf, binary.BigEndian, v.Height)
	binary.Write(buf, binary.BigEndian, v.Round)
	binary.Write(buf, binary.BigEndian, v.Timestamp)
	binary.Write(buf, binary.BigEndian, v.Proposed)
	binary.Write(buf, binary.BigEndian, v.Address)
	return buf.Bytes()
}

func (v *Vote) Copy() *Vote {
	copy := *v
	copy.Signature = make([]byte, 0, len(v.Signature))
	copy.Signature = append(copy.Signature, v.Signature...)
	return &copy
}

func (vote *Vote) String() string {
	if vote == nil {
		return "nil-Vote"
	}
	var typeString string
	switch vote.Type {
	case ProposalType:
		typeString = "Proposal"
	case PrevoteType:
		typeString = "Prevote"
	case PrecommitType:
		typeString = "Precommit"
	default:
		common.PanicSanity("Unknown vote type")
	}

	return fmt.Sprintf("Vote{%v/%02d/%v(%v) %s %X %X @ %v}",
		vote.Height,
		vote.Round,
		vote.Type,
		typeString,
		vote.Address,
		common.Fingerprint(vote.Proposed[:]),
		common.Fingerprint(vote.Signature),
		vote.Timestamp,
	)
}

// ValidateBasic performs basic validation.
func (vote *Vote) ValidateBasic() error {
	if !IsVoteTypeValid(vote.Type) {
		return errors.New("Invalid Type")
	}
	if vote.Height < 0 {
		return errors.New("Negative Height")
	}
	if vote.Round < 0 {
		return errors.New("Negative Round")
	}

	// NOTE: Timestamp validation is subtle and handled elsewhere.

	if len(vote.Signature) == 0 {
		return errors.New("Signature is missing")
	}
	//if len(vote.Signature) > MaxSignatureSize {
	//	return fmt.Errorf("Signature is too big (max: %d)", MaxSignatureSize)
	//}
	return nil
}

func (vote *Vote) Bytes() []byte {
	return cdcEncode(vote)
}

// Commit contains the evidence that a block was committed by a set of validators.
type Commit struct {
	ProposedData ProposedData `json:"proposed_data"`
	Precommits   []*Vote      `json:"precommits"`
	Address      PubKey       `json:"address"`
	Signature    []byte       `json:"signature"`
}

func (commit *Commit) SetSigner(key PubKey) {
	commit.Address = key
}

func (commit *Commit) GetSigner() PubKey {
	return commit.Address
}

func (commit *Commit) SetSignature(sig []byte) {
	commit.Signature = sig
}

func (commit *Commit) GetSignature() []byte {
	return commit.Signature
}

func (commit *Commit) Digest() []byte {
	h := sha256.New()
	h.Write(commit.ProposedData[:])
	for i := range commit.Precommits {
		h.Write(commit.Precommits[i].Bytes())
	}
	h.Write([]byte(commit.Address))
	return h.Sum(nil)
}

func (commit *Commit) Bytes() []byte {
	return cdcEncode(commit)
}

// FirstPrecommit returns the first non-nil precommit in the commit.
// If all precommits are nil, it returns an empty precommit with height 0.
func (commit *Commit) FirstPrecommit() *Vote {
	if len(commit.Precommits) == 0 {
		return nil
	}
	for _, precommit := range commit.Precommits {
		if precommit != nil {
			return precommit
		}
	}
	return &Vote{
		Type: PrecommitType,
	}
}

// Height returns the height of the commit
func (commit *Commit) Height() int64 {
	if len(commit.Precommits) == 0 {
		return 0
	}
	return commit.FirstPrecommit().Height
}

// Round returns the round of the commit
func (commit *Commit) Round() int {
	if len(commit.Precommits) == 0 {
		return 0
	}
	return commit.FirstPrecommit().Round
}

// Type returns the vote type of the commit, which is always VoteTypePrecommit
func (commit *Commit) Type() byte {
	return byte(PrecommitType)
}

// Size returns the number of votes in the commit
func (commit *Commit) Size() int {
	if commit == nil {
		return 0
	}
	return len(commit.Precommits)
}

// GetByIndex returns the vote corresponding to a given validator index
func (commit *Commit) GetByIndex(index int) *Vote {
	return commit.Precommits[index]
}

// IsCommit returns true if there is at least one vote
func (commit *Commit) IsCommit() bool {
	return len(commit.Precommits) != 0
}

// ValidateBasic performs basic validation that doesn't involve state data.
// Does not actually check the cryptographic signatures.
func (commit *Commit) ValidateBasic() error {
	if commit.ProposedData.IsNil() {
		return errors.New("Commit cannot be for nil block")
	}
	if len(commit.Precommits) == 0 {
		return errors.New("No precommits in commit")
	}
	height, round := commit.Height(), commit.Round()

	// Validate the precommits.
	for _, precommit := range commit.Precommits {
		// It's OK for precommits to be missing.
		if precommit == nil {
			continue
		}
		// Ensure that all votes are precommits.
		if precommit.Type != PrecommitType {
			return fmt.Errorf("Invalid commit vote. Expected precommit, got %v",
				precommit.Type)
		}
		// Ensure that all heights are the same.
		if precommit.Height != height {
			return fmt.Errorf("Invalid commit precommit height. Expected %v, got %v",
				height, precommit.Height)
		}
		// Ensure that all rounds are the same.
		if precommit.Round != round {
			return fmt.Errorf("Invalid commit precommit round. Expected %v, got %v",
				round, precommit.Round)
		}
	}
	return nil
}

func (commit *Commit) String() string {
	return "" // TODO:
}
