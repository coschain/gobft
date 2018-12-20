package go_bft

import (
	"sync"

	"github.com/coschain/go-bft/custom"
	"github.com/coschain/go-bft/message"
	log "github.com/sirupsen/logrus"
)

type Validators struct {
	sync.RWMutex
	height int64 // current height

	CustomValidators custom.IValidators
	privVal          custom.IPrivValidator
}

func NewValidators(val custom.IValidators, pVal custom.IPrivValidator) *Validators {
	v := &Validators{
		CustomValidators: val,
		privVal:          pVal,
	}
	return v
}

func (v *Validators) Sign(vote *message.Vote) {
	vote.Address = v.privVal.GetPubKey()
	vote.Signature = v.privVal.Sign(vote.Digest())
}

func (v *Validators) GetSelfPubKey() message.PubKey {
	return v.privVal.GetPubKey()
}

func (v *Validators) VerifySignature(vote *message.Vote) bool {
	//v.RLock()
	//defer v.RUnlock()

	val := v.CustomValidators.GetValidator(vote.Address)
	if val == nil {
		log.Errorf("vote %s signed by a invalid validator", vote.String())
		return false
	}
	return val.VerifySig(vote.Digest(), vote.Signature)
}

func (v *Validators) GetVotingPower(address *message.PubKey) int64 {
	//v.RLock()
	//defer v.RUnlock()

	val := v.CustomValidators.GetValidator(*address)
	if val == nil {
		log.Errorf("%s is not a  validator", *address)
		return 0
	}

	return val.GetVotingPower()
}

func (v *Validators) GetTotalVotingPower() int64 {
	//v.RLock()
	//defer v.RUnlock()

	return v.CustomValidators.TotalVotingPower()
}
