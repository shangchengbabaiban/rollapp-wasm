package params

import (
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/types"

	simappparams "github.com/cosmos/cosmos-sdk/simapp/params"
)

// EncodingConfig specifies the concrete encoding types to use for a given app.
// This is provided for compatibility between protobuf and amino implementations.
type EncodingConfig struct {
	InterfaceRegistry types.InterfaceRegistry
	Codec             codec.Codec
	TxConfig          client.TxConfig
	Amino             *codec.LegacyAmino
}

// EncodingAsSimapp takes an EncodingConfig and returns a simappparams.EncodingConfig.
// The only difference between the two is the type, which is useful for compatibility
// between amino and protobuf implementations.
func EncodingAsSimapp(encCfg EncodingConfig) simappparams.EncodingConfig {
	return simappparams.EncodingConfig{
		InterfaceRegistry: encCfg.InterfaceRegistry,
		Codec:             encCfg.Codec,
		TxConfig:          encCfg.TxConfig,
		Amino:             encCfg.Amino,
	}
}
