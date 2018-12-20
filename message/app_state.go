package message

import (
	"time"
)

type AppState struct {
	// The first height that reaches bft consensus is 1, so LastHeight
	// should be 0 at genesis
	LastHeight       int64
	LastProposedData ProposedData
	LastCommitTime   time.Time

	// the latest AppHash we've received from calling abci.Commit()
	AppHash []byte
}
