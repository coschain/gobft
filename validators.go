package go_bft

import (
	"sync"

	"github.com/coschain/go-bft/custom"
	"github.com/coschain/go-bft/message"
)

type Validators struct {
	sync.RWMutex
	height int64 // current height
	Vals   custom.IValidators
}

func NewValidators(val custom.IValidators) *Validators {
	v := &Validators{
		Vals: val,
	}
	return v
}

func (v *Validators) UpdateValidators(h int64, vals custom.IValidators) {
	v.Lock()
	defer v.Unlock()

	v.Vals = vals
	v.height = h
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
