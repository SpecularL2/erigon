package jsonrpc

import (
	"context"
	"fmt"
	"github.com/ledgerwatch/erigon-lib/chain"
	libcommon "github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon-lib/kv"
	"github.com/ledgerwatch/erigon-lib/kv/memdb"
	"github.com/ledgerwatch/erigon/consensus"
	"github.com/ledgerwatch/erigon/core"
	"github.com/ledgerwatch/erigon/core/state"
	"github.com/ledgerwatch/erigon/core/vm"
	"github.com/ledgerwatch/erigon/core/vm/evmtypes"
	"github.com/ledgerwatch/erigon/eth/stagedsync"
	"github.com/ledgerwatch/erigon/rpc"
	"github.com/ledgerwatch/erigon/turbo/rpchelper"
	"github.com/ledgerwatch/erigon/turbo/services"
	"github.com/ledgerwatch/erigon/turbo/trie"
	"github.com/ledgerwatch/log/v3"

	jsoniter "github.com/json-iterator/go"
	"github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon/core/rawdb"
	"github.com/ledgerwatch/erigon/core/types"
	"github.com/ledgerwatch/erigon/eth/tracers"
	"github.com/ledgerwatch/erigon/turbo/transactions"
)

// TODO: implement https://github.com/SpecularL2/specular/blob/08d42a39a0cdbae89fe9064f2916888b927705aa/clients/geth/specular/prover/test_api.go#L31
func (api *PrivateDebugAPIImpl) GenerateProofForTest(ctx context.Context, hash common.Hash, config *tracers.TraceConfig, stream *jsoniter.Stream) error {
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

	//msg, blockCtx, txCtx, ibs, _, err := transactions.ComputeTxEnv(ctx, engine, block, chainConfig, api._blockReader, tx, int(txnIndex), false)
	msg, blockCtx, txCtx, ibs, _, err := api.computeTxEnvForProof(ctx, engine, block, chainConfig, api._blockReader, tx, int(txnIndex), false)
	if err != nil {
		stream.WriteNil()
		return err
	}
	// Trace the transaction and return
	return transactions.ProveTx(ctx, msg, blockCtx, txCtx, ibs, config, chainConfig, stream, api.evmCallTimeout)
}

// ComputeTxEnv returns the execution environment of a certain transaction.
// TODO: use PlainStateWriter instead of PlainState
func (api *PrivateDebugAPIImpl) computeTxEnvForProof(ctx context.Context, engine consensus.EngineReader, block *types.Block, cfg *chain.Config, headerReader services.HeaderReader, dbtx kv.Tx, txIndex int, historyV3 bool) (core.Message, evmtypes.BlockContext, evmtypes.TxContext, *state.IntraBlockState, state.StateReader, error) {
	//reader, err := rpchelper.CreateHistoryStateReader(dbtx, block.NumberU64(), txIndex, historyV3, cfg.ChainName)

	batch := memdb.NewMemoryBatch(dbtx, "temp")
	defer batch.Rollback()

	rl := trie.NewRetainList(0)

	unwindState := &stagedsync.UnwindState{UnwindPoint: block.Header().Number.Uint64()}

	latestBlock, err := rpchelper.GetLatestBlockNumber(dbtx)
	if err != nil {
		return nil, evmtypes.BlockContext{}, evmtypes.TxContext{}, nil, nil, err
	}
	//stagedsync.UnwindIntermediateHashesStage()
	stageState := &stagedsync.StageState{BlockNumber: -latestBlock} // some random number

	var lgr log.Logger = nil // TODO: replace nil with logger
	hashStageCfg := stagedsync.StageHashStateCfg(nil, api.dirs, api.historyV3(batch))
	if err := stagedsync.UnwindHashStateStage(unwindState, stageState, batch, hashStageCfg, ctx, lgr); err != nil {
		return nil, evmtypes.BlockContext{}, evmtypes.TxContext{}, nil, nil, err
	}

	interHashStageCfg := stagedsync.StageTrieCfg(nil, false, false, false, api.dirs.Tmp, api._blockReader, nil, api.historyV3(batch), api._agg)
	_, err = stagedsync.UnwindIntermediateHashesForTrieLoader("eth_getProof", rl, unwindState, stageState, batch, interHashStageCfg, nil, nil, ctx.Done(), lgr)
	if err != nil {
		return nil, evmtypes.BlockContext{}, evmtypes.TxContext{}, nil, nil, err
	}
	tx := batch

	blockNumber := rpc.BlockNumber(block.Header().Number.Int64())
	blockNumberOrHash := rpc.BlockNumberOrHash{
		BlockNumber: &blockNumber,
	}

	reader, err := rpchelper.CreateStateReader(ctx, tx, blockNumberOrHash, 0, api.filters, api.stateCache, api.historyV3(tx), "")

	// Create the parent state database
	statedb := state.New(reader)

	if txIndex == 0 && len(block.Transactions()) == 0 {
		return nil, evmtypes.BlockContext{}, evmtypes.TxContext{}, statedb, reader, nil
	}
	getHeader := func(hash libcommon.Hash, n uint64) *types.Header {
		h, _ := headerReader.HeaderByNumber(ctx, dbtx, n)
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
		return msg, blockContext, TxContext, statedb, reader, nil
	}
	vmenv := vm.NewEVM(blockContext, evmtypes.TxContext{}, statedb, cfg, vm.Config{})
	rules := vmenv.ChainRules()

	consensusHeaderReader := stagedsync.NewChainReaderImpl(cfg, dbtx, nil)

	core.InitializeBlockExecution(engine.(consensus.Engine), consensusHeaderReader, header, block.Transactions(), block.Uncles(), cfg, statedb)

	for idx, txn := range block.Transactions() {
		select {
		default:
		case <-ctx.Done():
			return nil, evmtypes.BlockContext{}, evmtypes.TxContext{}, nil, nil, ctx.Err()
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
			return msg, blockContext, TxContext, statedb, reader, nil
		}
		vmenv.Reset(TxContext, statedb)
		// Not yet the searched for transaction, execute on top of the current state
		if _, err := core.ApplyMessage(vmenv, msg, new(core.GasPool).AddGas(txn.GetGas()).AddDataGas(txn.GetDataGas()), true /* refunds */, false /* gasBailout */); err != nil {
			return nil, evmtypes.BlockContext{}, evmtypes.TxContext{}, nil, nil, fmt.Errorf("transaction %x failed: %w", txn.Hash(), err)
		}
		// Ensure any modifications are committed to the state
		// Only delete empty objects if EIP161 (part of Spurious Dragon) is in effect
		_ = statedb.FinalizeTx(rules, reader.(*state.PlainState))

		if idx+1 == len(block.Transactions()) {
			// Return the state from evaluating all txs in the block, note no msg or TxContext in this case
			return nil, blockContext, evmtypes.TxContext{}, statedb, reader, nil
		}
	}
	return nil, evmtypes.BlockContext{}, evmtypes.TxContext{}, nil, nil, fmt.Errorf("transaction index %d out of range for block %x", txIndex, block.Hash())
}
