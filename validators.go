package go_bft

import (
	"sync"

	"github.com/coschain/go-bft/custom"
	"github.com/coschain/go-bft/message"
)

type Validators struct {
	sync.RWMutex
	height int64 // current height

	Vals    custom.IValidators
	privVal custom.IPrivValidator
}

func NewValidators(val custom.IValidators, pVal custom.IPrivValidator) *Validators {
	v := &Validators{
		Vals:    val,
		privVal: pVal,
	}
	return v
}

func (v *Validators) Sign(vote *message.Vote) {
	vote.Address = v.privVal.GetPubKey()
	vote.Signature = v.privVal.Sign(vote.Digest())
}

func (v *Validators) VerifySignature(vote *message.Vote) bool {
	v.RLock()
	defer v.RUnlock()

	return v.Vals.GetValidator(vote.Address).VerifySig(vote.Digest(), vote.Signature)
}

func (v *Validators) GetVotingPower(address *message.PubKey) int64 {
	v.RLock()
	defer v.RUnlock()

	return v.Vals.GetValidator(*address).GetVotingPower()
}

func (v *Validators) GetTotalVotingPower() int64 {
	v.RLock()
	defer v.RUnlock()

	return v.Vals.TotalVotingPower()
}
