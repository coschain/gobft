package go_bft

import (
	"github.com/coschain/go-bft/custom"
	"github.com/coschain/go-bft/message"
)

type Validators struct {
	height int64
	Val    custom.Validators
}

func (v *Validators) VerifySignature(vote *message.Vote) bool {
	return v.Val.GetValidator(vote.Address).VerifySig(vote.Digest(), vote.Signature)
}

func (v *Validators) GetVotingPower(address *message.PubKey) int64 {
	return v.Val.GetValidator(*address).GetVotingPower()
}

func (v *Validators) GetTotalVotingPower() int64 {
	return v.Val.TotalVotingPower()
}
