package runtime

import (
	"context"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/opendlt/accumulate-accumen/engine/host"
)

// RegisterHostBindings registers all AccuWASM host functions with the given module builder
func RegisterHostBindings(ctx context.Context, builder wazero.HostModuleBuilder, hostAPI *host.API) error {
	// Core state operations
	builder.NewFunctionBuilder().
		WithName("accuwasm_get").
		WithParameterNames("key_ptr", "key_len", "value_ptr", "value_len").
		WithResultNames("result_len").
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			keyPtr := uint32(stack[0])
			keyLen := uint32(stack[1])
			valuePtr := uint32(stack[2])
			valueLen := uint32(stack[3])

			result := hostAPI.Get(ctx, mod, keyPtr, keyLen, valuePtr, valueLen)
			stack[0] = uint64(result)
		}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export("accuwasm_get")

	builder.NewFunctionBuilder().
		WithName("accuwasm_set").
		WithParameterNames("key_ptr", "key_len", "value_ptr", "value_len").
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			keyPtr := uint32(stack[0])
			keyLen := uint32(stack[1])
			valuePtr := uint32(stack[2])
			valueLen := uint32(stack[3])

			hostAPI.Set(ctx, mod, keyPtr, keyLen, valuePtr, valueLen)
		}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{}).
		Export("accuwasm_set")

	builder.NewFunctionBuilder().
		WithName("accuwasm_delete").
		WithParameterNames("key_ptr", "key_len").
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			keyPtr := uint32(stack[0])
			keyLen := uint32(stack[1])

			hostAPI.Delete(ctx, mod, keyPtr, keyLen)
		}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{}).
		Export("accuwasm_delete")

	// Iteration functions
	builder.NewFunctionBuilder().
		WithName("accuwasm_iterator_new").
		WithParameterNames("prefix_ptr", "prefix_len").
		WithResultNames("iterator_id").
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			prefixPtr := uint32(stack[0])
			prefixLen := uint32(stack[1])

			result := hostAPI.IteratorNew(ctx, mod, prefixPtr, prefixLen)
			stack[0] = uint64(result)
		}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export("accuwasm_iterator_new")

	builder.NewFunctionBuilder().
		WithName("accuwasm_iterator_next").
		WithParameterNames("iterator_id", "key_ptr", "key_len", "value_ptr", "value_len").
		WithResultNames("has_next").
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			iteratorID := uint32(stack[0])
			keyPtr := uint32(stack[1])
			keyLen := uint32(stack[2])
			valuePtr := uint32(stack[3])
			valueLen := uint32(stack[4])

			result := hostAPI.IteratorNext(ctx, mod, iteratorID, keyPtr, keyLen, valuePtr, valueLen)
			stack[0] = uint64(result)
		}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export("accuwasm_iterator_next")

	builder.NewFunctionBuilder().
		WithName("accuwasm_iterator_close").
		WithParameterNames("iterator_id").
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			iteratorID := uint32(stack[0])

			hostAPI.IteratorClose(ctx, mod, iteratorID)
		}), []api.ValueType{api.ValueTypeI32}, []api.ValueType{}).
		Export("accuwasm_iterator_close")

	// Logging functions
	builder.NewFunctionBuilder().
		WithName("accuwasm_log").
		WithParameterNames("level", "msg_ptr", "msg_len").
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			level := uint32(stack[0])
			msgPtr := uint32(stack[1])
			msgLen := uint32(stack[2])

			hostAPI.Log(ctx, mod, level, msgPtr, msgLen)
		}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{}).
		Export("accuwasm_log")

	// Transaction context functions
	builder.NewFunctionBuilder().
		WithName("accuwasm_tx_get_id").
		WithParameterNames("id_ptr", "id_len").
		WithResultNames("actual_len").
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			idPtr := uint32(stack[0])
			idLen := uint32(stack[1])

			result := hostAPI.TxGetID(ctx, mod, idPtr, idLen)
			stack[0] = uint64(result)
		}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export("accuwasm_tx_get_id")

	builder.NewFunctionBuilder().
		WithName("accuwasm_tx_get_sender").
		WithParameterNames("sender_ptr", "sender_len").
		WithResultNames("actual_len").
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			senderPtr := uint32(stack[0])
			senderLen := uint32(stack[1])

			result := hostAPI.TxGetSender(ctx, mod, senderPtr, senderLen)
			stack[0] = uint64(result)
		}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export("accuwasm_tx_get_sender")

	builder.NewFunctionBuilder().
		WithName("accuwasm_tx_get_data").
		WithParameterNames("data_ptr", "data_len").
		WithResultNames("actual_len").
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			dataPtr := uint32(stack[0])
			dataLen := uint32(stack[1])

			result := hostAPI.TxGetData(ctx, mod, dataPtr, dataLen)
			stack[0] = uint64(result)
		}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export("accuwasm_tx_get_data")

	// Gas functions
	builder.NewFunctionBuilder().
		WithName("accuwasm_gas_remaining").
		WithResultNames("gas").
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			result := hostAPI.GasRemaining(ctx, mod)
			stack[0] = uint64(result)
		}), []api.ValueType{}, []api.ValueType{api.ValueTypeI64}).
		Export("accuwasm_gas_remaining")

	builder.NewFunctionBuilder().
		WithName("accuwasm_gas_consume").
		WithParameterNames("amount").
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			amount := stack[0]

			hostAPI.GasConsume(ctx, mod, amount)
		}), []api.ValueType{api.ValueTypeI64}, []api.ValueType{}).
		Export("accuwasm_gas_consume")

	// Block context functions
	builder.NewFunctionBuilder().
		WithName("accuwasm_block_height").
		WithResultNames("height").
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			result := hostAPI.BlockHeight(ctx, mod)
			stack[0] = result
		}), []api.ValueType{}, []api.ValueType{api.ValueTypeI64}).
		Export("accuwasm_block_height")

	builder.NewFunctionBuilder().
		WithName("accuwasm_block_timestamp").
		WithResultNames("timestamp").
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			result := hostAPI.BlockTimestamp(ctx, mod)
			stack[0] = result
		}), []api.ValueType{}, []api.ValueType{api.ValueTypeI64}).
		Export("accuwasm_block_timestamp")

	// Memory management helpers
	builder.NewFunctionBuilder().
		WithName("accuwasm_alloc").
		WithParameterNames("size").
		WithResultNames("ptr").
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			size := uint32(stack[0])

			result := hostAPI.Alloc(ctx, mod, size)
			stack[0] = uint64(result)
		}), []api.ValueType{api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export("accuwasm_alloc")

	builder.NewFunctionBuilder().
		WithName("accuwasm_free").
		WithParameterNames("ptr").
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			ptr := uint32(stack[0])

			hostAPI.Free(ctx, mod, ptr)
		}), []api.ValueType{api.ValueTypeI32}, []api.ValueType{}).
		Export("accuwasm_free")

	// Error handling
	builder.NewFunctionBuilder().
		WithName("accuwasm_abort").
		WithParameterNames("msg_ptr", "msg_len").
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			msgPtr := uint32(stack[0])
			msgLen := uint32(stack[1])

			hostAPI.Abort(ctx, mod, msgPtr, msgLen)
		}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{}).
		Export("accuwasm_abort")

	return nil
}