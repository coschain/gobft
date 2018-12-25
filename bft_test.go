package gobft

import (
	"crypto/sha256"
	"strconv"
	"testing"

	"github.com/coschain/gobft/custom"
	"github.com/coschain/gobft/message"
	"github.com/golang/mock/gomock"
	"github.com/sirupsen/logrus"
)

const nodeNum = 4

func TestBFT(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// init IPubValidator and IPrivValidator
	var pubKeys [nodeNum]message.PubKey
	var pubVals [nodeNum]*custom.MockIPubValidator
	for i := 0; i < nodeNum; i++ {
		pubKeys[i] = message.PubKey("val_pubkey" + strconv.Itoa(i))
		pubVals[i] = custom.NewMockIPubValidator(ctrl)
		pubVals[i].EXPECT().GetVotingPower().Return(int64(1)).AnyTimes()
		pubVals[i].EXPECT().VerifySig(gomock.Any(), gomock.Any()).Return(true).AnyTimes()
		pubVals[i].EXPECT().GetPubKey().Return(pubKeys[i]).AnyTimes()
	}

	var privVals [nodeNum]*custom.MockIPrivValidator
	for i := 0; i < nodeNum; i++ {
		privVals[i] = custom.NewMockIPrivValidator(ctrl)
		privVals[i].EXPECT().GetPubKey().Return(pubKeys[i]).AnyTimes()
		privVals[i].EXPECT().Sign(gomock.Any()).DoAndReturn(func(digest []byte) []byte {
			return digest
		}).AnyTimes()
	}

	// init committee
	stopCh := make(chan struct{})
	commitTimes := 0
	initState := &message.AppState{
		LastHeight:       0,
		LastProposedData: message.NilData,
	}
	committedStates := make([]*message.AppState, 0)
	committedStates = append(committedStates, initState)
	curProposers := []*custom.MockIPubValidator{pubVals[0], pubVals[1], pubVals[2], pubVals[3]}
	var indeces [nodeNum]int
	var proposedData message.ProposedData = sha256.Sum256([]byte("hello"))
	var committees [nodeNum]*custom.MockICommittee
	for j := 0; j < nodeNum; j++ {
		i := j
		indeces[i] = 0
		committees[i] = custom.NewMockICommittee(ctrl)
		committees[i].EXPECT().GetValidator(pubKeys[i]).Return(pubVals[i]).AnyTimes()
		committees[i].EXPECT().IsValidator(gomock.Any()).Return(true).AnyTimes()
		committees[i].EXPECT().TotalVotingPower().Return(int64(4)).AnyTimes()
		committees[i].EXPECT().GetCurrentProposer().DoAndReturn(func() message.PubKey {
			defer func() { indeces[i] = (indeces[i] + 1) % nodeNum }()
			return curProposers[indeces[i]].GetPubKey()
		}).AnyTimes()
		committees[i].EXPECT().DecidesProposal().Return(proposedData).AnyTimes()
		committees[i].EXPECT().Commit(gomock.Any()).DoAndReturn(func(data message.ProposedData) error {
			s := &message.AppState{
				LastHeight:       committedStates[len(committedStates)-1].LastHeight + 1,
				LastProposedData: data,
			}
			committedStates = append(committedStates, s)
			logrus.Infof("core %d committed %v at height %d", i, data, s.LastHeight)
			commitTimes++
			if commitTimes == 4 {
				close(stopCh)
			}
			return nil
		}).AnyTimes()
		committees[i].EXPECT().GetAppState().Return(committedStates[len(committedStates)-1]).AnyTimes()
	}

	// init bft core
	var cores [nodeNum]*Core
	for i := 0; i < nodeNum; i++ {
		cores[i] = NewCore(committees[i], privVals[i])
		cores[i].SetName("core" + strconv.Itoa(i))
	}
	for i := 0; i < nodeNum; i++ {
		cores[i].validators.CustomValidators.(*custom.MockICommittee).EXPECT().
			BroadCast(gomock.Any()).DoAndReturn(func(msg message.ConsensusMessage) error {
			for j := 0; j < nodeNum; j++ {
				cores[j].RecvMsg(msg)
			}
			return nil
		}).AnyTimes()
	}

	// start
	for i := 0; i < nodeNum; i++ {
		cores[i].Start()
	}

	<-stopCh
}
