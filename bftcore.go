package go_bft

import "github.com/coschain/go-bft/custom"

type Core struct {
	validators *Validators

	RoundState
	msgQueue      chan msgInfo
	timeoutTicker TimeoutTicker
}

func New(vals custom.IValidators, pVal custom.IPrivValidator) *Core {
	c := &Core{
		validators: NewValidators(vals, pVal),
	}
	return c
}
