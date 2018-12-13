package go_bft

import (
	"sync"

	"github.com/coschain/go-bft/custom"
	"github.com/coschain/go-bft/message"
)

type Validators struct {
	sync.RWMutex
	height     int64                        // current height
	heightVals map[int64]custom.IValidators // height->IValidators
}

func NewValidators(val custom.IValidators) *Validators {
	v := &Validators{
		heightVals: make(map[int64]custom.IValidators),
	}
	return v
}

func (v *Validators) AddValidators(h int64, vals custom.IValidators) {
	v.Lock()
	defer v.Unlock()

	if _, ok := v.heightVals[h]; ok {
		return
	}
	v.heightVals[h] = vals
}

func (v *Validators) RemoveValidators(h int64) {
	v.Lock()
	defer v.Unlock()

	delete(v.heightVals, h)
}

func (v *Validators) VerifySignature(vote *message.Vote) bool {
	v.RLock()
	defer v.RUnlock()

	return v.heightVals[v.height].GetValidator(vote.Address).VerifySig(vote.Digest(), vote.Signature)
}

func (v *Validators) GetVotingPower(address *message.PubKey) int64 {
	v.RLock()
	defer v.RUnlock()

	return v.heightVals[v.height].GetValidator(*address).GetVotingPower()
}

func (v *Validators) GetTotalVotingPower() int64 {
	v.RLock()
	defer v.RUnlock()

	return v.heightVals[v.height].TotalVotingPower()
}
