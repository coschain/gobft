package message

import (
	"time"
)

type AppState struct {
	// SYSID is immutable, each application should specify a unique SYSID
	SYSID string

	// LastBlockHeight=0 at genesis (ie. block(H=0) does not exist)
	LastHeight       int64
	LastProposedData ProposedData
	LastCommitTime   time.Time

	// the latest AppHash we've received from calling abci.Commit()
	AppHash []byte
}
