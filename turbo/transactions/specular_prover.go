package transactions

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	jsoniter "github.com/json-iterator/go"
	"github.com/ledgerwatch/erigon-lib/chain"
	"github.com/ledgerwatch/erigon/consensus/bor/statefull"
	"github.com/ledgerwatch/erigon/core"
	"github.com/ledgerwatch/erigon/core/vm"
	"github.com/ledgerwatch/erigon/core/vm/evmtypes"
	"github.com/ledgerwatch/erigon/eth/tracers"
	"github.com/ledgerwatch/erigon/eth/tracers/logger"
)

//
//// ComputeTxEnv returns the execution environment of a certain transaction.
//// TODO: use PlainStateWriter instead of PlainState
//func ComputeTxEnvForProof(ctx context.Context, engine consensus.EngineReader, block *types.Block, cfg *chain.Config, headerReader services.HeaderReader, dbtx kv.Tx, txIndex int, historyV3 bool, api jsonrpc.PrivateDebugAPIImpl) (core.Message, evmtypes.BlockContext, evmtypes.TxContext, *state.IntraBlockState, state.StateReader, error) {
//	//reader, err := rpchelper.CreateHistoryStateReader(dbtx, block.NumberU64(), txIndex, historyV3, cfg.ChainName)
//
//	batch := memdb.NewMemoryBatch(dbtx, "temp")
//	defer batch.Rollback()
//
//	unwindState := &stagedsync.UnwindState{UnwindPoint: block.Header().Number.Uint64()}
//
//	//stagedsync.UnwindIntermediateHashesStage()
//	stageState := &stagedsync.StageState{BlockNumber: -1} // some random number
//
//	hashStageCfg := stagedsync.StageHashStateCfg(nil, api.Dirs, api.HistoryV3(batch))
//	if err := stagedsync.UnwindHashStateStage(unwindState, stageState, batch, hashStageCfg, ctx, api.logger); err != nil {
//		return nil, err
//	}
//
//	interHashStageCfg := stagedsync.StageTrieCfg(nil, false, false, false, api.Dirs.Tmp, api._blockReader, nil, api.HistoryV3(batch), api._agg)
//	loader, err = stagedsync.UnwindIntermediateHashesForTrieLoader("eth_getProof", rl, unwindState, stageState, batch, interHashStageCfg, nil, nil, ctx.Done(), api.logger)
//	if err != nil {
//		return nil, err
//	}
//	tx = batch
//
//	if err != nil {
//		return nil, evmtypes.BlockContext{}, evmtypes.TxContext{}, nil, nil, err
//	}
//
//	// Create the parent state database
//	statedb := state.New(reader)
//
//	if txIndex == 0 && len(block.Transactions()) == 0 {
//		return nil, evmtypes.BlockContext{}, evmtypes.TxContext{}, statedb, reader, nil
//	}
//	getHeader := func(hash libcommon.Hash, n uint64) *types.Header {
//		h, _ := headerReader.HeaderByNumber(ctx, dbtx, n)
//		return h
//	}
//	header := block.HeaderNoCopy()
//
//	blockContext := core.NewEVMBlockContext(header, core.GetHashFn(header, getHeader), engine, nil)
//
//	// Recompute transactions up to the target index.
//	signer := types.MakeSigner(cfg, block.NumberU64(), block.Time())
//	if historyV3 {
//		rules := cfg.Rules(blockContext.BlockNumber, blockContext.Time)
//		txn := block.Transactions()[txIndex]
//		statedb.SetTxContext(txn.Hash(), block.Hash(), txIndex)
//		msg, _ := txn.AsMessage(*signer, block.BaseFee(), rules)
//		if msg.FeeCap().IsZero() && engine != nil {
//			syscall := func(contract libcommon.Address, data []byte) ([]byte, error) {
//				return core.SysCallContract(contract, data, cfg, statedb, header, engine, true /* constCall */)
//			}
//			msg.SetIsFree(engine.IsServiceTransaction(msg.From(), syscall))
//		}
//
//		TxContext := core.NewEVMTxContext(msg)
//		return msg, blockContext, TxContext, statedb, reader, nil
//	}
//	vmenv := vm.NewEVM(blockContext, evmtypes.TxContext{}, statedb, cfg, vm.Config{})
//	rules := vmenv.ChainRules()
//
//	consensusHeaderReader := stagedsync.NewChainReaderImpl(cfg, dbtx, nil)
//
//	core.InitializeBlockExecution(engine.(consensus.Engine), consensusHeaderReader, header, block.Transactions(), block.Uncles(), cfg, statedb)
//
//	for idx, txn := range block.Transactions() {
//		select {
//		default:
//		case <-ctx.Done():
//			return nil, evmtypes.BlockContext{}, evmtypes.TxContext{}, nil, nil, ctx.Err()
//		}
//		statedb.SetTxContext(txn.Hash(), block.Hash(), idx)
//
//		// Assemble the transaction call message and return if the requested offset
//		msg, _ := txn.AsMessage(*signer, block.BaseFee(), rules)
//		if msg.FeeCap().IsZero() && engine != nil {
//			syscall := func(contract libcommon.Address, data []byte) ([]byte, error) {
//				return core.SysCallContract(contract, data, cfg, statedb, header, engine, true /* constCall */)
//			}
//			msg.SetIsFree(engine.IsServiceTransaction(msg.From(), syscall))
//		}
//
//		TxContext := core.NewEVMTxContext(msg)
//		if idx == txIndex {
//			return msg, blockContext, TxContext, statedb, reader, nil
//		}
//		vmenv.Reset(TxContext, statedb)
//		// Not yet the searched for transaction, execute on top of the current state
//		if _, err := core.ApplyMessage(vmenv, msg, new(core.GasPool).AddGas(txn.GetGas()).AddDataGas(txn.GetDataGas()), true /* refunds */, false /* gasBailout */); err != nil {
//			return nil, evmtypes.BlockContext{}, evmtypes.TxContext{}, nil, nil, fmt.Errorf("transaction %x failed: %w", txn.Hash(), err)
//		}
//		// Ensure any modifications are committed to the state
//		// Only delete empty objects if EIP161 (part of Spurious Dragon) is in effect
//		_ = statedb.FinalizeTx(rules, reader.(*state.PlainState))
//
//		if idx+1 == len(block.Transactions()) {
//			// Return the state from evaluating all txs in the block, note no msg or TxContext in this case
//			return nil, blockContext, evmtypes.TxContext{}, statedb, reader, nil
//		}
//	}
//	return nil, evmtypes.BlockContext{}, evmtypes.TxContext{}, nil, nil, fmt.Errorf("transaction index %d out of range for block %x", txIndex, block.Hash())
//}

// TraceTx configures a new tracer according to the provided configuration, and
// executes the given message in the provided environment. The return value will
// be tracer dependent.
// TODO: inject prover
func ProveTx(
	ctx context.Context,
	message core.Message,
	blockCtx evmtypes.BlockContext,
	txCtx evmtypes.TxContext,
	ibs evmtypes.IntraBlockState,
	config *tracers.TraceConfig,
	chainConfig *chain.Config,
	stream *jsoniter.Stream,
	callTimeout time.Duration,
) error {
	// Assemble the structured logger or the JavaScript tracer
	var (
		tracer vm.EVMLogger
		err    error
	)
	var streaming bool
	switch {
	case config != nil && config.Tracer != nil:
		// Define a meaningful timeout of a single transaction trace
		timeout := callTimeout
		if config.Timeout != nil {
			if timeout, err = time.ParseDuration(*config.Timeout); err != nil {
				stream.WriteNil()
				return err
			}
		}
		// Construct the JavaScript tracer to execute with
		cfg := json.RawMessage("{}")
		if config != nil && config.TracerConfig != nil {
			cfg = *config.TracerConfig
		}
		if tracer, err = tracers.New(*config.Tracer, &tracers.Context{
			TxHash: txCtx.TxHash,
		}, cfg); err != nil {
			stream.WriteNil()
			return err
		}
		// Handle timeouts and RPC cancellations
		deadlineCtx, cancel := context.WithTimeout(ctx, timeout)
		go func() {
			<-deadlineCtx.Done()
			tracer.(tracers.Tracer).Stop(errors.New("execution timeout"))
		}()
		defer cancel()
		streaming = false

	case config == nil:
		tracer = logger.NewJsonStreamLogger(nil, ctx, stream)
		streaming = true

	default:
		tracer = logger.NewJsonStreamLogger(config.LogConfig, ctx, stream)
		streaming = true
	}
	// Run the transaction with tracing enabled.
	vmenv := vm.NewEVM(blockCtx, txCtx, ibs, chainConfig, vm.Config{Debug: true, Tracer: tracer})
	var refunds = true
	if config != nil && config.NoRefunds != nil && *config.NoRefunds {
		refunds = false
	}
	if streaming {
		stream.WriteObjectStart()
		stream.WriteObjectField("structLogs")
		stream.WriteArrayStart()
	}

	var result *core.ExecutionResult
	if config != nil && config.BorTx != nil && *config.BorTx {
		callmsg := prepareCallMessage(message)
		result, err = statefull.ApplyBorMessage(*vmenv, callmsg)
	} else {
		fmt.Printf("apply message, message: %v vmenv: %v, refunds: %v\n", message, vmenv, refunds)
		result, err = core.ApplyMessage(vmenv, message, new(core.GasPool).AddGas(message.Gas()).AddDataGas(message.DataGas()), refunds, false /* gasBailout */)
	}

	if err != nil {
		if streaming {
			stream.WriteArrayEnd()
			stream.WriteObjectEnd()
			stream.WriteMore()
			stream.WriteObjectField("resultHack") // higher-level func will assing it to NULL
		} else {
			stream.WriteNil()
		}
		return fmt.Errorf("tracing failed: %w", err)
	}
	// Depending on the tracer type, format and return the output
	if streaming {
		stream.WriteArrayEnd()
		stream.WriteMore()
		stream.WriteObjectField("gas")
		stream.WriteUint64(result.UsedGas)
		stream.WriteMore()
		stream.WriteObjectField("failed")
		stream.WriteBool(result.Failed())
		stream.WriteMore()
		// If the result contains a revert reason, return it.
		returnVal := hex.EncodeToString(result.Return())
		if len(result.Revert()) > 0 {
			returnVal = hex.EncodeToString(result.Revert())
		}
		stream.WriteObjectField("returnValue")
		stream.WriteString(returnVal)
		stream.WriteObjectEnd()
	} else {
		if r, err1 := tracer.(tracers.Tracer).GetResult(); err1 == nil {
			stream.Write(r)
		} else {
			return err1
		}
	}
	return nil
}
