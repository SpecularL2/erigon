package prover

import (
	gethcommon "github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	gethvm "github.com/ethereum/go-ethereum/core/vm"
	gethparams "github.com/ethereum/go-ethereum/params"
	"github.com/ledgerwatch/erigon-lib/chain"
	"github.com/ledgerwatch/erigon/core/types"
	"github.com/ledgerwatch/erigon/core/vm/evmtypes"
)

func toGethReceipt(receipt *types.Receipt) *gethtypes.Receipt {
	logs := make([]*gethtypes.Log, len(receipt.Logs))
	for i, log := range receipt.Logs {
		topics := make([]gethcommon.Hash, len(log.Topics))
		for j, topic := range log.Topics {
			topics[j] = gethcommon.Hash(topic)
		}
		logs[i] = &gethtypes.Log{
			Address:     gethcommon.Address(log.Address),
			Topics:      topics,
			Data:        log.Data,
			BlockNumber: log.BlockNumber,
			TxHash:      gethcommon.Hash(log.TxHash),
			TxIndex:     log.TxIndex,
			BlockHash:   gethcommon.Hash(log.BlockHash),
			Index:       log.Index,
			Removed:     log.Removed,
		}
	}
	return &gethtypes.Receipt{
		Type:              receipt.Type,
		PostState:         receipt.PostState,
		Status:            receipt.Status,
		CumulativeGasUsed: receipt.CumulativeGasUsed,
		Bloom:             gethtypes.Bloom(receipt.Bloom),
		Logs:              logs,
		TxHash:            gethcommon.Hash(receipt.TxHash),
		ContractAddress:   gethcommon.Address(receipt.ContractAddress),
		GasUsed:           receipt.GasUsed,
		BlockHash:         gethcommon.Hash(receipt.BlockHash),
		BlockNumber:       receipt.BlockNumber,
		TransactionIndex:  receipt.TransactionIndex,
	}
}

func toGethTxContext(txctx *evmtypes.TxContext) *gethvm.TxContext {
	return &gethvm.TxContext{
		Origin:   gethcommon.Address(txctx.Origin),
		GasPrice: txctx.GasPrice.ToBig(),
	}
}

func toGethRules(rules *chain.Rules) gethparams.Rules {
	return gethparams.Rules{
		ChainID:          rules.ChainID,
		IsHomestead:      rules.IsHomestead,
		IsEIP150:         rules.IsTangerineWhistle,
		IsEIP155:         rules.IsSpuriousDragon,
		IsEIP158:         rules.IsTangerineWhistle,
		IsByzantium:      rules.IsByzantium,
		IsConstantinople: rules.IsConstantinople,
		IsPetersburg:     rules.IsPetersburg,
		IsIstanbul:       rules.IsIstanbul,
		IsBerlin:         rules.IsBerlin,
		IsLondon:         rules.IsLondon,
		IsMerge:          rules.IsShanghai,
		IsShanghai:       rules.IsShanghai,
	}
}
