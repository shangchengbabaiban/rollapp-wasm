package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/types"
	cryptoCodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

// RegisterLegacyAminoCodec registers the necessary interfaces and concrete types on the provided LegacyAmino codec.
// These types are used for Amino JSON serialization.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgRequestCallback{}, "callback/MsgRequestCallback", nil)
	cdc.RegisterConcrete(&MsgCancelCallback{}, "callback/MsgCancelCallback", nil)
	cdc.RegisterConcrete(&MsgUpdateParams{}, "callback/MsgUpdateParams", nil)
}

// RegisterInterfaces registers interfaces types with the interface registry.
func RegisterInterfaces(registry types.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRequestCallback{},
		&MsgCancelCallback{},
		&MsgUpdateParams{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}

var (
	ModuleCdc = codec.NewAminoCodec(amino)
	amino     = codec.NewLegacyAmino()
)

func init() {
	RegisterLegacyAminoCodec(amino)
	cryptoCodec.RegisterCrypto(amino)
	sdk.RegisterLegacyAminoCodec(amino)
	amino.Seal()
}
