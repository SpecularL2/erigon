package prover

import (
	"context"
	"fmt"
	"math/big"
	"time"

	gethtypes "github.com/ethereum/go-ethereum/core/types"
	jsoniter "github.com/json-iterator/go"
	"github.com/ledgerwatch/erigon-lib/chain"
	"github.com/ledgerwatch/erigon-lib/kv"
	"github.com/ledgerwatch/erigon/core"
	"github.com/ledgerwatch/erigon/core/state"
	"github.com/ledgerwatch/erigon/core/types"
	"github.com/ledgerwatch/erigon/core/vm"
	"github.com/ledgerwatch/erigon/core/vm/evmtypes"
	"github.com/ledgerwatch/erigon/eth/tracers"
	oss "github.com/specularl2/specular/clients/geth/specular/prover/state"
	prover_types "github.com/specularl2/specular/clients/geth/specular/prover/types"
)

// func (api *ProverAPI) GenerateProofForTest(ctx context.Context, hash common.Hash, cumulativeGasUsed, blockGasUsed *big.Int, step uint64, config *ProverConfig) (json.RawMessage, error) {
// 	transaction, blockHash, blockNumber, index, err := api.backend.GetTransaction(ctx, hash)
// 	if err != nil {
// 		return nil, err
// 	}
// 	// It shouldn't happen in practice.
// 	if blockNumber == 0 {
// 		return nil, errors.New("genesis is not traceable")
// 	}
// 	reexec := defaultProveReexec
// 	if config != nil && config.Reexec != nil {
// 		reexec = *config.Reexec
// 	}
// 	block, err := api.backend.BlockByNumber(ctx, rpc.BlockNumber(blockNumber))
// 	if err != nil {
// 		return nil, err
// 	}
// 	if block == nil {
// 		return nil, fmt.Errorf("block #%d not found", blockNumber)
// 	}
// 	msg, vmctx, statedb, err := api.backend.StateAtTransaction(ctx, block, int(index), reexec)
// 	if err != nil {
// 		return nil, err
// 	}
// 	txContext := core.NewEVMTxContext(msg)
// 	receipts, err := api.backend.GetReceipts(ctx, blockHash)
// 	if err != nil {
// 		return nil, err
// 	}
// 	blockHashTree, err := oss.BlockHashTreeFromBlockContext(vmctx)
// 	if err != nil {
// 		return nil, err
// 	}
// 	its := oss.InterStateFromCaptured(
// 		blockNumber,
// 		index,
// 		statedb,
// 		cumulativeGasUsed,
// 		blockGasUsed,
// 		block.Transactions(),
// 		receipts,
// 		blockHashTree,
// 	)
// 	prover := NewTestProver(
// 		step,
// 		transaction,
// 		&txContext,
// 		receipts[index],
// 		api.backend.ChainConfig().Rules(vmctx.BlockNumber(), vmctx.Random() != nil),
// 		blockNumber,
// 		index,
// 		statedb,
// 		*its,
// 		blockHashTree,
// 	)
// 	vmenv := api.backend.NewEVM(vmctx, txContext, statedb, api.backend.ChainConfig(), prover_types.L2ELClientConfig{Debug: true, Tracer: prover})
// 	// TODO: under geth situation we don't need to pass in the block hash
// 	statedb.Prepare(hash, common.Hash{}, int(index))
// 	_, err = api.backend.ApplyMessage(vmenv, msg, new(core.GasPool).AddGas(msg.Gas()))
// 	if err != nil {
// 		return nil, fmt.Errorf("tracing failed: %w", err)
// 	}
// 	return prover.GetResult()
// }

// TraceTx configures a new tracer according to the provided configuration, and
// executes the given message in the provided environment. The return value will
// be tracer dependent.
func GenerateProofForTest(
	ctx context.Context,
	message core.Message,
	blockCtx evmtypes.BlockContext,
	txCtx evmtypes.TxContext,
	ibs *state.IntraBlockState,
	transactions types.Transactions,
	receipts types.Receipts,
	rules *chain.Rules,
	blockNumber uint64,
	transactionIdx uint64,
	stepIdx uint64,
	cumulativeGasUsed *big.Int,
	blockGasUsed *big.Int,
	dbtx kv.Tx,
	reader state.StateReader,
	writer state.StateWriter,
	config *tracers.TraceConfig,
	chainConfig *chain.Config,
	stream *jsoniter.Stream,
	callTimeout time.Duration,
) error {
	globalStateWrapper := NewErigonGlobalStateWrapper(rules, ibs, dbtx, reader, writer)
	blockHashTree, err := oss.BlockHashTreeFromBlockContext(&ErigonBlockContextWrapper{&blockCtx})
	if err != nil {
		return err
	}

	gethTransactions := make([]prover_types.Transaction, len(transactions))
	for i, transaction := range transactions {
		gethTransactions[i] = toGethTransaction(transaction)
	}
	gethReceipts := make([]*gethtypes.Receipt, len(receipts))
	for i, receipt := range receipts {
		gethReceipts[i] = toGethReceipt(receipt)
	}
	its := oss.InterStateFromCaptured(
		blockNumber,
		transactionIdx,
		globalStateWrapper,
		cumulativeGasUsed,
		blockGasUsed,
		gethTransactions,
		gethReceipts,
		blockHashTree,
	)

	tracer, err := NewTestProver(
		stepIdx,
		gethTransactions[transactionIdx],
		&txCtx,
		gethReceipts[transactionIdx],
		rules,
		blockNumber,
		transactionIdx,
		dbtx,
		reader,
		writer,
		globalStateWrapper,
		*its,
		blockHashTree,
	)
	if err != nil {
		return err
	}
	// Run the transaction with tracing enabled.
	vmenv := vm.NewEVM(blockCtx, txCtx, ibs, chainConfig, vm.Config{Debug: true, Tracer: tracer})
	var refunds = true
	if config != nil && config.NoRefunds != nil && *config.NoRefunds {
		refunds = false
	}

	if config != nil && config.BorTx != nil && *config.BorTx {
		return fmt.Errorf("bor tx not supported")
	}

	// var result *core.ExecutionResult
	_, err = core.ApplyMessage(vmenv, message, new(core.GasPool).AddGas(message.Gas()).AddDataGas(message.DataGas()), refunds, false /* gasBailout */)

	if err != nil {
		stream.WriteNil()
		return fmt.Errorf("tracing failed: %w", err)
	}
	if r, err1 := tracer.GetResult(); err1 == nil {
		stream.Write(r)
	} else {
		return err1
	}
	return nil
}

func GenerateProofForBench(
	ctx context.Context,
	message core.Message,
	blockCtx evmtypes.BlockContext,
	txCtx evmtypes.TxContext,
	ibs *state.IntraBlockState,
	transactions types.Transactions,
	receipts types.Receipts,
	rules *chain.Rules,
	blockNumber uint64,
	transactionIdx uint64,
	cumulativeGasUsed *big.Int,
	blockGasUsed *big.Int,
	dbtx kv.Tx,
	reader state.StateReader,
	writer state.StateWriter,
	config *tracers.TraceConfig,
	chainConfig *chain.Config,
	stream *jsoniter.Stream,
	callTimeout time.Duration,
) error {
	globalStateWrapper := NewErigonGlobalStateWrapper(rules, ibs, dbtx, reader, writer)
	blockHashTree, err := oss.BlockHashTreeFromBlockContext(&ErigonBlockContextWrapper{&blockCtx})
	if err != nil {
		return err
	}

	gethTransactions := make([]prover_types.Transaction, len(transactions))
	for i, transaction := range transactions {
		gethTransactions[i] = toGethTransaction(transaction)
	}
	gethReceipts := make([]*gethtypes.Receipt, len(receipts))
	for i, receipt := range receipts {
		gethReceipts[i] = toGethReceipt(receipt)
	}
	its := oss.InterStateFromCaptured(
		blockNumber,
		transactionIdx,
		globalStateWrapper,
		cumulativeGasUsed,
		blockGasUsed,
		gethTransactions,
		gethReceipts,
		blockHashTree,
	)

	tracer, err := NewBenchProver(
		gethTransactions[transactionIdx],
		&txCtx,
		gethReceipts[transactionIdx],
		rules,
		blockNumber,
		transactionIdx,
		dbtx,
		reader,
		writer,
		globalStateWrapper,
		*its,
		blockHashTree,
	)
	if err != nil {
		return err
	}
	// Run the transaction with tracing enabled.
	vmenv := vm.NewEVM(blockCtx, txCtx, ibs, chainConfig, vm.Config{Debug: true, Tracer: tracer})
	var refunds = true
	if config != nil && config.NoRefunds != nil && *config.NoRefunds {
		refunds = false
	}

	if config != nil && config.BorTx != nil && *config.BorTx {
		return fmt.Errorf("bor tx not supported")
	}

	// var result *core.ExecutionResult
	_, err = core.ApplyMessage(vmenv, message, new(core.GasPool).AddGas(message.Gas()).AddDataGas(message.DataGas()), refunds, false /* gasBailout */)

	if err != nil {
		stream.WriteNil()
		return fmt.Errorf("tracing failed: %w", err)
	}
	if r, err1 := tracer.GetResult(); err1 == nil {
		stream.Write(r)
	} else {
		return err1
	}
	return nil
}
