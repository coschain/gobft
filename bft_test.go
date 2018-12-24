package go_bft

import (
	"strconv"
	"testing"

	"github.com/coschain/go-bft/custom"
	"github.com/coschain/go-bft/message"
	"github.com/golang/mock/gomock"
)

func TestBFT(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// init IPubValidator and IPrivValidator
	var pubKeys [4]message.PubKey
	var pubVals [4]*custom.MockIPubValidator
	for i := 0; i < 4; i++ {
		pubKeys[i] = message.PubKey("val_pubkey" + strconv.Itoa(i))
		pubVals[i] = custom.NewMockIPubValidator(ctrl)
		pubVals[i].EXPECT().GetVotingPower().Return(int64(1)).AnyTimes()
		pubVals[i].EXPECT().VerifySig(gomock.Any(), gomock.Any()).Return(true).AnyTimes()
		pubVals[i].EXPECT().GetPubKey().Return(pubKeys[i]).AnyTimes()
	}

	var privVals [4]*custom.MockIPrivValidator
	for i := 0; i < 4; i++ {
		privVals[i] = custom.NewMockIPrivValidator(ctrl)
		privVals[i].EXPECT().GetPubKey().Return(pubKeys[i]).AnyTimes()
		privVals[i].EXPECT().Sign(gomock.Any()).DoAndReturn(func(digest []byte) []byte {
			return digest
		}).AnyTimes()
	}
}
