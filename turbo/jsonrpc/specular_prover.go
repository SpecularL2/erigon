package jsonrpc

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ledgerwatch/erigon-lib/chain"
	libcommon "github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon-lib/kv/memdb"
	"github.com/ledgerwatch/erigon/consensus"
	"github.com/ledgerwatch/erigon/core"
	"github.com/ledgerwatch/erigon/core/state"
	"github.com/ledgerwatch/erigon/core/vm"
	"github.com/ledgerwatch/erigon/core/vm/evmtypes"
	"github.com/ledgerwatch/erigon/eth/stagedsync"
	"github.com/ledgerwatch/erigon/prover"
	"github.com/ledgerwatch/erigon/turbo/rpchelper"
	"github.com/ledgerwatch/erigon/turbo/services"

	jsoniter "github.com/json-iterator/go"
	"github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon/core/rawdb"
	"github.com/ledgerwatch/erigon/core/types"
	"github.com/ledgerwatch/erigon/eth/tracers"
)

// ComputeTxEnv returns the execution environment of a certain transaction.
// TODO: use PlainStateWriter instead of PlainState
func (api *PrivateDebugAPIImpl) computeTxEnvForProof(
	ctx context.Context,
	engine consensus.EngineReader,
	block *types.Block,
	cfg *chain.Config,
	headerReader services.HeaderReader,
	tx *memdb.MemoryMutation,
	txIndex int,
	historyV3 bool,
) (
	core.Message,
	evmtypes.BlockContext,
	evmtypes.TxContext,
	*chain.Rules,
	*state.IntraBlockState,
	state.StateReader,
	state.StateWriter,
	error,
) {
	latestBlock, err := rpchelper.GetLatestBlockNumber(tx)
	if err != nil {
		return nil, evmtypes.BlockContext{}, evmtypes.TxContext{}, nil, nil, nil, nil, err
	}

	unwindState := &stagedsync.UnwindState{UnwindPoint: block.Header().Number.Uint64() - 1}
	stageState := &stagedsync.StageState{BlockNumber: latestBlock}

	hashStageCfg := stagedsync.StageHashStateCfg(nil, api.dirs, api.historyV3(tx))
	if err = stagedsync.UnwindHashStateStage(unwindState, stageState, tx, hashStageCfg, ctx, api.logger); err != nil {
		return nil, evmtypes.BlockContext{}, evmtypes.TxContext{}, nil, nil, nil, nil, err
	}

	interHashStageCfg := stagedsync.StageTrieCfg(nil, false, false, false, api.dirs.Tmp, api._blockReader, nil, api.historyV3(tx), api._agg)
	err = stagedsync.UnwindIntermediateHashesStage(unwindState, stageState, tx, interHashStageCfg, ctx, api.logger)
	if err != nil {
		return nil, evmtypes.BlockContext{}, evmtypes.TxContext{}, nil, nil, nil, nil, err
	}

	execCfg := stagedsync.ExecuteBlockCfg{}
	err = stagedsync.UnwindExecutionStage(unwindState, stageState, tx, ctx, execCfg, false, api.logger)
	if err != nil {
		return nil, evmtypes.BlockContext{}, evmtypes.TxContext{}, nil, nil, nil, nil, err
	}

	reader := state.NewPlainStateReader(tx)
	writer := state.NewPlainStateWriterNoHistory(tx)

	// Create the parent state database
	statedb := state.New(reader)

	if txIndex == 0 && len(block.Transactions()) == 0 {
		return nil, evmtypes.BlockContext{}, evmtypes.TxContext{}, nil, nil, nil, nil, err
	}
	getHeader := func(hash libcommon.Hash, n uint64) *types.Header {
		h, _ := headerReader.HeaderByNumber(ctx, tx, n)
		return h
	}
	header := block.HeaderNoCopy()

	blockContext := core.NewEVMBlockContext(header, core.GetHashFn(header, getHeader), engine, nil)

	// Recompute transactions up to the target index.
	signer := types.MakeSigner(cfg, block.NumberU64(), block.Time())
	if historyV3 {
		rules := cfg.Rules(blockContext.BlockNumber, blockContext.Time)
		txn := block.Transactions()[txIndex]
		statedb.SetTxContext(txn.Hash(), block.Hash(), txIndex)
		msg, _ := txn.AsMessage(*signer, block.BaseFee(), rules)
		if msg.FeeCap().IsZero() && engine != nil {
			syscall := func(contract libcommon.Address, data []byte) ([]byte, error) {
				return core.SysCallContract(contract, data, cfg, statedb, header, engine, true /* constCall */)
			}
			msg.SetIsFree(engine.IsServiceTransaction(msg.From(), syscall))
		}

		TxContext := core.NewEVMTxContext(msg)
		return msg, blockContext, TxContext, rules, statedb, reader, writer, nil
	}
	vmenv := vm.NewEVM(blockContext, evmtypes.TxContext{}, statedb, cfg, vm.Config{})
	rules := vmenv.ChainRules()

	consensusHeaderReader := stagedsync.NewChainReaderImpl(cfg, tx, nil)

	core.InitializeBlockExecution(engine.(consensus.Engine), consensusHeaderReader, header, block.Transactions(), block.Uncles(), cfg, statedb)

	for idx, txn := range block.Transactions() {
		select {
		default:
		case <-ctx.Done():
			return nil, evmtypes.BlockContext{}, evmtypes.TxContext{}, nil, nil, nil, nil, ctx.Err()
		}
		statedb.SetTxContext(txn.Hash(), block.Hash(), idx)

		// Assemble the transaction call message and return if the requested offset
		msg, _ := txn.AsMessage(*signer, block.BaseFee(), rules)
		if msg.FeeCap().IsZero() && engine != nil {
			syscall := func(contract libcommon.Address, data []byte) ([]byte, error) {
				return core.SysCallContract(contract, data, cfg, statedb, header, engine, true /* constCall */)
			}
			msg.SetIsFree(engine.IsServiceTransaction(msg.From(), syscall))
		}

		TxContext := core.NewEVMTxContext(msg)
		if idx == txIndex {
			return msg, blockContext, TxContext, rules, statedb, reader, writer, nil
		}
		vmenv.Reset(TxContext, statedb)
		// Not yet the searched for transaction, execute on top of the current state
		if _, err := core.ApplyMessage(vmenv, msg, new(core.GasPool).AddGas(txn.GetGas()).AddDataGas(txn.GetDataGas()), true /* refunds */, false /* gasBailout */); err != nil {
			return nil, evmtypes.BlockContext{}, evmtypes.TxContext{}, nil, nil, nil, nil, fmt.Errorf("transaction %x failed: %w", txn.Hash(), err)
		}
		// Ensure any modifications are committed to the state
		// Only delete empty objects if EIP161 (part of Spurious Dragon) is in effect
		_ = statedb.FinalizeTx(rules, writer)

		if idx+1 == len(block.Transactions()) {
			// Return the state from evaluating all txs in the block, note no msg or TxContext in this case
			return nil, blockContext, evmtypes.TxContext{}, rules, statedb, reader, writer, nil
		}
	}
	return nil, evmtypes.BlockContext{}, evmtypes.TxContext{}, nil, nil, nil, nil, fmt.Errorf("transaction index %d out of range for block %x", txIndex, block.Hash())
}

// TODO: implement https://github.com/SpecularL2/specular/blob/08d42a39a0cdbae89fe9064f2916888b927705aa/clients/geth/specular/prover/test_api.go#L31
func (api *PrivateDebugAPIImpl) GenerateProofForTest(ctx context.Context, hash common.Hash, stepIdx uint64, config *tracers.TraceConfig, stream *jsoniter.Stream) error {
	tx, err := api.db.BeginRo(ctx)
	if err != nil {
		stream.WriteNil()
		return err
	}
	defer tx.Rollback()
	chainConfig, err := api.chainConfig(tx)
	if err != nil {
		stream.WriteNil()
		return err
	}
	// Retrieve the transaction and assemble its EVM context
	blockNum, ok, err := api.txnLookup(ctx, tx, hash)
	if err != nil {
		stream.WriteNil()
		return err
	}
	if !ok {
		stream.WriteNil()
		return nil
	}

	// check pruning to ensure we have history at this block level
	err = api.BaseAPI.checkPruneHistory(tx, blockNum)
	if err != nil {
		stream.WriteNil()
		return err
	}

	// Private API returns 0 if transaction is not found.
	if blockNum == 0 && chainConfig.Bor != nil {
		blockNumPtr, err := rawdb.ReadBorTxLookupEntry(tx, hash)
		if err != nil {
			stream.WriteNil()
			return err
		}
		if blockNumPtr == nil {
			stream.WriteNil()
			return nil
		}
		blockNum = *blockNumPtr
	}
	block, err := api.blockByNumberWithSenders(ctx, tx, blockNum)
	if err != nil {
		stream.WriteNil()
		return err
	}
	if block == nil {
		stream.WriteNil()
		return nil
	}
	var txnIndex uint64
	var txn types.Transaction
	for i, transaction := range block.Transactions() {
		if transaction.Hash() == hash {
			txnIndex = uint64(i)
			txn = transaction
			break
		}
	}
	if txn == nil {
		var borTx types.Transaction
		borTx, err = rawdb.ReadBorTransaction(tx, hash)
		if err != nil {
			return err
		}

		if borTx != nil {
			stream.WriteNil()
			return nil
		}
		stream.WriteNil()
		return fmt.Errorf("transaction %#x not found", hash)
	}
	engine := api.engine()

	receipts, err := api.getReceipts(ctx, tx, chainConfig, block, nil)
	if err != nil {
		stream.WriteNil()
		return err
	}

	batch := memdb.NewMemoryBatch(tx, api.dirs.Tmp)
	defer batch.Rollback()

	fmt.Println("before")
	fmt.Println("historyV3", api.historyV3(batch))

	//msg, blockCtx, txCtx, ibs, _, err := transactions.ComputeTxEnv(ctx, engine, block, chainConfig, api._blockReader, tx, int(txnIndex), false)
	msg, blockCtx, txCtx, rules, ibs, reader, writer, err := api.computeTxEnvForProof(ctx, engine, block, chainConfig, api._blockReader, batch, int(txnIndex), false)
	if err != nil {
		stream.WriteNil()
		fmt.Println("error1")
		return err
	}

	fmt.Println("wtf")

	// Trace the transaction and return
	return prover.GenerateProofForTest(
		ctx,
		msg,
		blockCtx,
		txCtx,
		ibs,
		block.Transactions(),
		receipts,
		rules,
		blockNum,
		txnIndex,
		stepIdx,
		big.NewInt(0),
		big.NewInt(0),
		batch,
		reader,
		writer,
		config,
		chainConfig,
		stream,
		api.evmCallTimeout,
	)
}

func (api *PrivateDebugAPIImpl) GenerateProofForBench(ctx context.Context, hash common.Hash, config *tracers.TraceConfig, stream *jsoniter.Stream) error {
	tx, err := api.db.BeginRo(ctx)
	if err != nil {
		stream.WriteNil()
		return err
	}
	defer tx.Rollback()
	chainConfig, err := api.chainConfig(tx)
	if err != nil {
		stream.WriteNil()
		return err
	}
	// Retrieve the transaction and assemble its EVM context
	blockNum, ok, err := api.txnLookup(ctx, tx, hash)
	if err != nil {
		stream.WriteNil()
		return err
	}
	if !ok {
		stream.WriteNil()
		return nil
	}

	// check pruning to ensure we have history at this block level
	err = api.BaseAPI.checkPruneHistory(tx, blockNum)
	if err != nil {
		stream.WriteNil()
		return err
	}

	// Private API returns 0 if transaction is not found.
	if blockNum == 0 && chainConfig.Bor != nil {
		blockNumPtr, err := rawdb.ReadBorTxLookupEntry(tx, hash)
		if err != nil {
			stream.WriteNil()
			return err
		}
		if blockNumPtr == nil {
			stream.WriteNil()
			return nil
		}
		blockNum = *blockNumPtr
	}
	block, err := api.blockByNumberWithSenders(ctx, tx, blockNum)
	if err != nil {
		stream.WriteNil()
		return err
	}
	if block == nil {
		stream.WriteNil()
		return nil
	}
	var txnIndex uint64
	var txn types.Transaction
	for i, transaction := range block.Transactions() {
		if transaction.Hash() == hash {
			txnIndex = uint64(i)
			txn = transaction
			break
		}
	}
	if txn == nil {
		var borTx types.Transaction
		borTx, err = rawdb.ReadBorTransaction(tx, hash)
		if err != nil {
			return err
		}

		if borTx != nil {
			stream.WriteNil()
			return nil
		}
		stream.WriteNil()
		return fmt.Errorf("transaction %#x not found", hash)
	}
	engine := api.engine()

	receipts, err := api.getReceipts(ctx, tx, chainConfig, block, nil)
	if err != nil {
		stream.WriteNil()
		return err
	}

	batch := memdb.NewMemoryBatch(tx, api.dirs.Tmp)
	defer batch.Rollback()

	fmt.Println("before")
	fmt.Println("historyV3", api.historyV3(batch))

	//msg, blockCtx, txCtx, ibs, _, err := transactions.ComputeTxEnv(ctx, engine, block, chainConfig, api._blockReader, tx, int(txnIndex), false)
	msg, blockCtx, txCtx, rules, ibs, reader, writer, err := api.computeTxEnvForProof(ctx, engine, block, chainConfig, api._blockReader, batch, int(txnIndex), false)
	if err != nil {
		stream.WriteNil()
		fmt.Println("error1")
		return err
	}

	fmt.Println("wtf")

	// Trace the transaction and return
	return prover.GenerateProofForBench(
		ctx,
		msg,
		blockCtx,
		txCtx,
		ibs,
		block.Transactions(),
		receipts,
		rules,
		blockNum,
		txnIndex,
		big.NewInt(0),
		big.NewInt(0),
		batch,
		reader,
		writer,
		config,
		chainConfig,
		stream,
		api.evmCallTimeout,
	)
}
