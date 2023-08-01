package full

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"

	"go.uber.org/fx"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/exitcode"
	"github.com/ipfs/go-cid"

	builtin2 "github.com/filecoin-project/go-state-types/builtin"
	builtinactors "github.com/filecoin-project/lotus/chain/actors/builtin"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/store"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/types/ethtypes"
)

type EthTraceAPI interface {
	TraceBlock(ctx context.Context, blkNum string) (interface{}, error)
	TraceReplayBlockTransactions(ctx context.Context, blkNum string, traceTypes []string) (interface{}, error)
}

var (
	_ EthTraceAPI = *new(api.FullNode)
)

type EthTrace struct {
	fx.In

	Chain        *store.ChainStore
	StateManager *stmgr.StateManager

	ChainAPI
	EthModuleAPI
}

var _ EthTraceAPI = (*EthTrace)(nil)

type Trace struct {
	Action       Action `json:"action"`
	Result       Result `json:"result"`
	Subtraces    int    `json:"subtraces"`
	TraceAddress []int  `json:"traceAddress"`
	Type         string `json:"Type"`
}

type TraceBlock struct {
	*Trace
	BlockHash           ethtypes.EthHash `json:"blockHash"`
	BlockNumber         int64            `json:"blockNumber"`
	TransactionHash     ethtypes.EthHash `json:"transactionHash"`
	TransactionPosition int              `json:"transactionPosition"`
}

type TraceReplayBlockTransaction struct {
	Output          string           `json:"output"`
	StateDiff       *string          `json:"stateDiff"`
	Trace           []*Trace         `json:"trace"`
	TransactionHash ethtypes.EthHash `json:"transactionHash"`
	VmTrace         *string          `json:"vmTrace"`
}

type Action struct {
	CallType string             `json:"callType"`
	From     string             `json:"from"`
	To       string             `json:"to"`
	Gas      ethtypes.EthUint64 `json:"gas"`
	Input    string             `json:"input"`
	Value    ethtypes.EthBigInt `json:"value"`

	Method  abi.MethodNum `json:"method"`
	CodeCid cid.Cid       `json:"codeCid"`
}

type Result struct {
	GasUsed ethtypes.EthUint64 `json:"gasUsed"`
	Output  string             `json:"output"`
}

func (e *EthTrace) TraceBlock(ctx context.Context, blkNum string) (interface{}, error) {
	ts, err := e.getTipsetByBlockNr(ctx, blkNum, false)
	if err != nil {
		return nil, err
	}

	_, trace, err := e.StateManager.ExecutionTrace(ctx, ts)
	if err != nil {
		return nil, xerrors.Errorf("failed to compute base state: %w", err)
	}

	tsParent, err := e.ChainAPI.ChainGetTipSetByHeight(ctx, ts.Height()+1, e.Chain.GetHeaviestTipSet().Key())
	if err != nil {
		return nil, fmt.Errorf("cannot get tipset at height: %v", ts.Height()+1)
	}

	msgs, err := e.ChainGetParentMessages(ctx, tsParent.Blocks()[0].Cid())
	if err != nil {
		return nil, err
	}

	cid, err := ts.Key().Cid()
	if err != nil {
		return nil, err
	}

	blkHash, err := ethtypes.EthHashFromCid(cid)
	if err != nil {
		return nil, err
	}

	allTraces := make([]*TraceBlock, 0, len(trace))
	for _, ir := range trace {
		// ignore messages from f00
		if ir.Msg.From.String() == "f00" {
			continue
		}

		idx := -1
		for msgIdx, msg := range msgs {
			if ir.Msg.From == msg.Message.From {
				idx = msgIdx
				break
			}
		}
		if idx == -1 {
			log.Warnf("cannot resolve message index for cid: %s", ir.MsgCid)
			continue
		}

		txHash, err := e.EthGetTransactionHashByCid(ctx, ir.MsgCid)
		if err != nil {
			return nil, err
		}
		if txHash == nil {
			log.Warnf("cannot find transaction hash for cid %s", ir.MsgCid)
			continue
		}

		traces := []*Trace{}
		buildTraces(&traces, []int{}, ir.ExecutionTrace, nil, int64(ts.Height()))

		traceBlocks := make([]*TraceBlock, 0, len(trace))
		for _, trace := range traces {
			traceBlocks = append(traceBlocks, &TraceBlock{
				Trace:               trace,
				BlockHash:           blkHash,
				BlockNumber:         int64(ts.Height()),
				TransactionHash:     *txHash,
				TransactionPosition: idx,
			})
		}

		allTraces = append(allTraces, traceBlocks...)
	}

	return allTraces, nil
}

func (e *EthTrace) TraceReplayBlockTransactions(ctx context.Context, blkNum string, traceTypes []string) (interface{}, error) {
	if len(traceTypes) != 1 || traceTypes[0] != "trace" {
		return nil, fmt.Errorf("only 'trace' is supported")
	}

	ts, err := e.getTipsetByBlockNr(ctx, blkNum, false)
	if err != nil {
		return nil, err
	}

	_, trace, err := e.StateManager.ExecutionTrace(ctx, ts)
	if err != nil {
		return nil, xerrors.Errorf("failed when calling ExecutionTrace: %w", err)
	}

	allTraces := make([]*TraceReplayBlockTransaction, 0, len(trace))
	for _, ir := range trace {
		// ignore messages from f00
		if ir.Msg.From.String() == "f00" {
			continue
		}

		txHash, err := e.EthGetTransactionHashByCid(ctx, ir.MsgCid)
		if err != nil {
			return nil, err
		}
		if txHash == nil {
			log.Warnf("cannot find transaction hash for cid %s", ir.MsgCid)
			continue
		}

		t := TraceReplayBlockTransaction{
			Output:          hex.EncodeToString(ir.MsgRct.Return),
			TransactionHash: *txHash,
			StateDiff:       nil,
			VmTrace:         nil,
		}

		buildTraces(&t.Trace, []int{}, ir.ExecutionTrace, nil, int64(ts.Height()))

		allTraces = append(allTraces, &t)
	}

	return allTraces, nil
}

func write_padded[T any](w io.Writer, data T, size int) error {
	tmp := &bytes.Buffer{}

	// first write data to tmp buffer to get the size
	err := binary.Write(tmp, binary.BigEndian, data)
	if err != nil {
		return err
	}

	if tmp.Len() > size {
		return fmt.Errorf("data is larger than size")
	}

	// write tailing zeros to pad up to size
	cnt := size - tmp.Len()
	for i := 0; i < cnt; i++ {
		err = binary.Write(w, binary.BigEndian, uint8(0))
		if err != nil {
			return err
		}
	}

	// finally write the actual value
	err = binary.Write(w, binary.BigEndian, tmp.Bytes())
	if err != nil {
		return err
	}

	return nil
}

func handle_filecoin_method_input(method abi.MethodNum, codec uint64, params []byte) ([]byte, error) {
	NATIVE_METHOD_SELECTOR := []byte{0x86, 0x8e, 0x10, 0xc4}
	EVM_WORD_SIZE := 32

	staticArgs := []uint64{
		uint64(method),
		codec,
		uint64(EVM_WORD_SIZE) * 3,
		uint64(len(params)),
	}
	totalWords := len(staticArgs) + (len(params) / EVM_WORD_SIZE)
	if len(params)%EVM_WORD_SIZE != 0 {
		totalWords += 1
	}
	len := 4 + totalWords*EVM_WORD_SIZE

	w := &bytes.Buffer{}
	err := binary.Write(w, binary.BigEndian, NATIVE_METHOD_SELECTOR)
	if err != nil {
		return nil, err
	}

	for _, arg := range staticArgs {
		err := write_padded(w, arg, 32)
		if err != nil {
			return nil, err
		}
	}
	binary.Write(w, binary.BigEndian, params)
	remain := len - w.Len()
	for i := 0; i < remain; i++ {
		binary.Write(w, binary.BigEndian, uint8(0))
	}

	return w.Bytes(), nil
}

func handle_filecoin_method_output(exitCode exitcode.ExitCode, codec uint64, data []byte) ([]byte, error) {
	w := &bytes.Buffer{}

	values := []interface{}{uint32(exitCode), codec, uint32(w.Len()), uint32(len(data))}
	for _, v := range values {
		err := write_padded(w, v, 32)
		if err != nil {
			return nil, err
		}
	}
	binary.Write(w, binary.BigEndian, []byte(data))

	return w.Bytes(), nil
}

// buildTraces recursively builds the traces for a given ExecutionTrace by walking the subcalls
func buildTraces(traces *[]*Trace, addr []int, et types.ExecutionTrace, parentEt *types.ExecutionTrace, height int64) {
	callType := "call"
	if et.Msg.ReadOnly {
		callType = "staticcall"
	}

	trace := &Trace{
		Action: Action{
			CallType: callType,
			From:     et.Msg.From.String(),
			To:       et.Msg.To.String(),
			Gas:      ethtypes.EthUint64(et.Msg.GasLimit),
			Input:    hex.EncodeToString(et.Msg.Params),
			Value:    ethtypes.EthBigInt(et.Msg.Value),
			Method:   et.Msg.Method,
			CodeCid:  et.Msg.CodeCid,
		},
		Result: Result{
			GasUsed: ethtypes.EthUint64(et.SumGas().TotalGas),
			Output:  hex.EncodeToString(et.MsgRct.Return),
		},
		Subtraces:    len(et.Subcalls),
		TraceAddress: addr,
		Type:         callType,
	}

	// Native calls
	//
	// When an EVM actor is invoked with a method number above 1023 that's not frc42(InvokeEVM)
	// then we need to format native calls in a way that makes sense to Ethereum tooling (convert
	// the input & output to solidity ABI format).
	if parentEt != nil {
		if builtinactors.IsEvmActor(parentEt.Msg.CodeCid) && et.Msg.Method > 1023 && et.Msg.Method != builtin2.MethodsEVM.InvokeContract {
			log.Infof("found Native call! method:%d, code:%s, height:%d", et.Msg.Method, et.Msg.CodeCid.String(), height)
			input, _ := handle_filecoin_method_input(et.Msg.Method, et.Msg.ParamsCodec, et.Msg.Params)
			trace.Action.Input = hex.EncodeToString(input)
			output, _ := handle_filecoin_method_output(et.MsgRct.ExitCode, et.MsgRct.ReturnCodec, et.MsgRct.Return)
			trace.Result.Output = hex.EncodeToString(output)
		}
	}

	// Native actor creation
	//
	// TODO...

	// EVM contract creation
	//
	// TODO...

	// EVM call special casing
	//
	// Any outbound call from an EVM actor on methods 1-1023 are side-effects from EVM instructions
	// and should be dropped from the trace.
	if parentEt != nil {
		if builtinactors.IsEvmActor(parentEt.Msg.CodeCid) && et.Msg.Method > 0 && et.Msg.Method <= 1023 {
			log.Infof("found outbound call from an EVM actor on method 1-1023 method:%d, code:%s, height:%d", et.Msg.Method, et.Msg.CodeCid.String(), height)

			// skip current trace but process subcalls
			for i, call := range et.Subcalls {
				buildTraces(traces, append(addr, i), call, &et, height)
			}

			return
		}
	}

	// EVM -> EVM calls
	//
	// Check for normal EVM to EVM calls and decode the params and return values
	if parentEt != nil {
		if builtinactors.IsEvmActor(parentEt.Msg.CodeCid) && builtinactors.IsEthAccountActor(et.Msg.CodeCid) && et.Msg.Method == builtin2.MethodsEVM.InvokeContract {
			log.Infof("evm to evm! ! ")
			input, _ := handle_filecoin_method_input(et.Msg.Method, et.Msg.ParamsCodec, et.Msg.Params)
			trace.Action.Input = hex.EncodeToString(input)
			output, _ := handle_filecoin_method_output(et.MsgRct.ExitCode, et.MsgRct.ReturnCodec, et.MsgRct.Return)
			trace.Result.Output = hex.EncodeToString(output)
		}
	}

	if et.Msg.From == et.Msg.To && et.Msg.Method == builtin2.MethodsEVM.InvokeContractDelegate {
		log.Info("from and to are the same, and method is InvokeContractDelegate!!!!!!!!, height:%d", height)
	}

	*traces = append(*traces, trace)

	for i, call := range et.Subcalls {
		buildTraces(traces, append(addr, i), call, &et, height)
	}
}

// TODO: refactor this to be shared code
func (e *EthTrace) getTipsetByBlockNr(ctx context.Context, blkParam string, strict bool) (*types.TipSet, error) {
	if blkParam == "earliest" {
		return nil, fmt.Errorf("block param \"earliest\" is not supported")
	}

	head := e.Chain.GetHeaviestTipSet()
	switch blkParam {
	case "pending":
		return head, nil
	case "latest":
		parent, err := e.Chain.GetTipSetFromKey(ctx, head.Parents())
		if err != nil {
			return nil, fmt.Errorf("cannot get parent tipset")
		}
		return parent, nil
	default:
		var num ethtypes.EthUint64
		err := num.UnmarshalJSON([]byte(`"` + blkParam + `"`))
		if err != nil {
			return nil, fmt.Errorf("cannot parse block number: %v", err)
		}
		if abi.ChainEpoch(num) > head.Height()-1 {
			return nil, fmt.Errorf("requested a future epoch (beyond 'latest')")
		}
		ts, err := e.ChainAPI.ChainGetTipSetByHeight(ctx, abi.ChainEpoch(num), head.Key())
		if err != nil {
			return nil, fmt.Errorf("cannot get tipset at height: %v", num)
		}
		if strict && ts.Height() != abi.ChainEpoch(num) {
			return nil, ErrNullRound
		}
		return ts, nil
	}
}
