package gobft

import (
	"sync"

	"github.com/coschain/gobft/custom"
	"github.com/coschain/gobft/message"
)

type Validators struct {
	sync.RWMutex
	height int64 // current height

	CustomValidators custom.ICommittee
	privVal          custom.IPrivValidator
}

func NewValidators(val custom.ICommittee, pVal custom.IPrivValidator) *Validators {
	v := &Validators{
		CustomValidators: val,
		privVal:          pVal,
	}
	return v
}

func (v *Validators) Sign(msg message.ConsensusMessage) {
	msg.SetSigner(v.privVal.GetPubKey())
	msg.SetSignature(v.privVal.Sign(msg.Digest()))
}

func (v *Validators) GetSelfPubKey() message.PubKey {
	return v.privVal.GetPubKey()
}

func (v *Validators) VerifySignature(msg message.ConsensusMessage) bool {
	val := v.CustomValidators.GetValidator(msg.GetSigner())
	if val == nil {
		return false
	}
	return val.VerifySig(msg.Digest(), msg.GetSignature())
}

func (v *Validators) GetVotingPower(address *message.PubKey) int64 {
	val := v.CustomValidators.GetValidator(*address)
	if val == nil {
		//log.Errorf("%s is not a  validator", *address)
		return 0
	}

	return val.GetVotingPower()
}

func (v *Validators) GetTotalVotingPower() int64 {
	return v.CustomValidators.TotalVotingPower()
}

func (v *Validators) GetValidatorNum() int {
	return v.CustomValidators.GetValidatorNum()
}
