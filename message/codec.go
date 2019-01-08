package message

import (
	"fmt"

	"github.com/coschain/gobft/common"
	"github.com/tendermint/go-amino"
)

const maxMsgSize = 1024 * 1024

var cdc = amino.NewCodec()

func init() {
	RegisterConsensusMessages(cdc)
}

// ConsensusMessage is a message that can be sent and received on the ConsensusReactor
type ConsensusMessage interface {
	ValidateBasic() error
	Digest() []byte
	SetSigner(key PubKey)
	SetSignature(sig []byte)
	GetSigner() PubKey
	GetSignature() []byte
	Bytes() []byte
	String() string
}

func RegisterConsensusMessages(cdc *amino.Codec) {
	cdc.RegisterInterface((*ConsensusMessage)(nil), nil)
	cdc.RegisterConcrete(&Vote{}, "gobft/Vote", nil)
	cdc.RegisterConcrete(&Commit{}, "gobft/Commit", nil)
}

func DecodeConsensusMsg(bz []byte) (msg ConsensusMessage, err error) {
	if len(bz) > maxMsgSize {
		return msg, fmt.Errorf("Msg exceeds max size (%d > %d)", len(bz), maxMsgSize)
	}
	err = cdc.UnmarshalBinaryBare(bz, &msg)
	return
}

// cdcEncode returns nil if the input is nil, otherwise returns
// cdc.MustMarshalBinaryBare(item)
func cdcEncode(item interface{}) []byte {
	if item != nil && !common.IsTypedNil(item) && !common.IsEmpty(item) {
		return cdc.MustMarshalBinaryBare(item)
	}
	return nil
}
