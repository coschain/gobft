package message

import (
	"time"
	"fmt"

	"github.com/coschain/go-bft/common"
)

func NewProposal(height uint64, round uint32, data []byte) *Proposal {
	return &Proposal {
		Height: height,
		Round: round,
		Data: data,
		Timestamp: uint64(time.Now().Unix()),
	}
}

func NewVote(t Votetype, height uint64, round uint32, proposal *Proposal) *Vote {
	return &Vote {
		Type: t,
		Height: height,
		Round: round,
		Timestamp: uint64(time.Now().Unix()),
		Proposal: proposal,
	}
}

func(p *Proposal) Copy() *Proposal {
	copy := *p
	copy.Data = make([]byte, 0, len(p.Data))
	copy.Data = append(copy.Data, p.Data...)
	copy.PubKeyBytes = make([]byte, 0, len(p.PubKeyBytes))
	copy.PubKeyBytes = append(copy.PubKeyBytes, p.PubKeyBytes...)
	copy.Signature = make([]byte, 0, len(p.Signature))
	copy.Signature = append(copy.Signature, p.Signature...)
	return &copy
}

func(v *Vote) Copy() *Vote {
	copy := *v
	copy.Proposal = v.Proposal.Copy()
	copy.PubKeyBytes = make([]byte, 0, len(v.PubKeyBytes))
	copy.PubKeyBytes = append(copy.PubKeyBytes, v.PubKeyBytes...)
	copy.Signature = make([]byte, 0, len(v.Signature))
	copy.Signature = append(copy.Signature, v.Signature...)
	return &copy
}

func (v *Vote) GracefulString() string {
	if v == nil {
		return "nil-Vote"
	}
	var typeString string
	switch v.Type {
	case Votetype_prevote:
		typeString = "Prevote"
	case Votetype_precommit:
		typeString = "Precommit"
	default:
		common.PanicSanity("Unknown vote type")
	}

	return fmt.Sprintf("Vote{%v/%02d/%v(%v) %X %X @ %d}",
		v.Height,
		v.Round,
		v.Type,
		typeString,
		common.Fingerprint(v.Proposal.Data),
		common.Fingerprint(v.Signature),
		v.Timestamp,
	)
}
