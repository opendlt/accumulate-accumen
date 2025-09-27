package runtime

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"gitlab.com/accumulatenetwork/accumulate/pkg/url"
	"gitlab.com/accumulatenetwork/accumulate/protocol"

	"github.com/opendlt/accumulate-accumen/bridge/l0api"
	"github.com/opendlt/accumulate-accumen/bridge/pricing"
	"github.com/opendlt/accumulate-accumen/engine/gas"
	"github.com/opendlt/accumulate-accumen/engine/host"
	"github.com/opendlt/accumulate-accumen/internal/accutil"
)

// RegisterHostBindings registers all AccuWASM host functions with the given module builder
func RegisterHostBindings(ctx context.Context, builder wazero.HostModuleBuilder, hostAPI *host.API, gasMeter *gas.Meter, execContext *ExecutionContext) error {
	// Core state operations
	builder.NewFunctionBuilder().
		WithName("accuwasm_get").
		WithParameterNames("key_ptr", "key_len", "value_ptr", "value_len").
		WithResultNames("result_len").
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			// Charge gas for read operation
			if !gasMeter.TryConsume(gas.GasCost(100)) {
				stack[0] = uint64(^uint32(0)) // Return error (-1 as uint32)
				return
			}

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
			// Charge gas for write operation
			if !gasMeter.TryConsume(gas.GasCost(200)) {
				return
			}

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

	// L0 operations
	builder.NewFunctionBuilder().
		WithName("l0_write_data").
		WithParameterNames("account_ptr", "account_len", "data_ptr", "data_len").
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			// Charge gas for L0 operation
			if !gasMeter.TryConsume(gas.GasCost(1000)) {
				return
			}

			accountPtr := uint32(stack[0])
			accountLen := uint32(stack[1])
			dataPtr := uint32(stack[2])
			dataLen := uint32(stack[3])

			// Read account URL and data from WASM memory
			memory := mod.Memory()
			accountBytes, _ := memory.Read(accountPtr, accountLen)
			dataBytes, _ := memory.Read(dataPtr, dataLen)

			// Stage L0 operation using URL-based constructor
			op, err := NewStagedOp("write_data", string(accountBytes))
			if err != nil {
				panic("l0_write_data: invalid account URL: " + err.Error())
			}
			op.Data = dataBytes
			execContext.stagedOps = append(execContext.stagedOps, op)
		}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{}).
		Export("l0_write_data")

	builder.NewFunctionBuilder().
		WithName("l0_send_tokens").
		WithParameterNames("from_ptr", "from_len", "to_ptr", "to_len", "amount").
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			// Charge gas for L0 operation
			if !gasMeter.TryConsume(gas.GasCost(1500)) {
				return
			}

			fromPtr := uint32(stack[0])
			fromLen := uint32(stack[1])
			toPtr := uint32(stack[2])
			toLen := uint32(stack[3])
			amount := stack[4]

			// Read addresses from WASM memory
			memory := mod.Memory()
			fromBytes, _ := memory.Read(fromPtr, fromLen)
			toBytes, _ := memory.Read(toPtr, toLen)

			// Stage L0 operation using URL-based constructor
			op, err := NewTokenSendOp(string(fromBytes), string(toBytes), amount)
			if err != nil {
				panic("l0_send_tokens: invalid URLs: " + err.Error())
			}
			execContext.stagedOps = append(execContext.stagedOps, op)
		}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI64}, []api.ValueType{}).
		Export("l0_send_tokens")

	builder.NewFunctionBuilder().
		WithName("l0_update_auth").
		WithParameterNames("account_ptr", "account_len", "auth_ptr", "auth_len").
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			// Charge gas for L0 operation
			if !gasMeter.TryConsume(gas.GasCost(800)) {
				return
			}

			accountPtr := uint32(stack[0])
			accountLen := uint32(stack[1])
			authPtr := uint32(stack[2])
			authLen := uint32(stack[3])

			// Read account URL and auth data from WASM memory
			memory := mod.Memory()
			accountBytes, _ := memory.Read(accountPtr, accountLen)
			authBytes, _ := memory.Read(authPtr, authLen)

			// Stage L0 operation using URL-based constructor
			op, err := NewStagedOp("update_auth", string(accountBytes))
			if err != nil {
				panic("l0_update_auth: invalid account URL: " + err.Error())
			}
			op.Data = authBytes
			execContext.stagedOps = append(execContext.stagedOps, op)
		}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{}).
		Export("l0_update_auth")

	// Credits pricing
	builder.NewFunctionBuilder().
		WithName("credits_quote").
		WithParameterNames("gas_estimate").
		WithResultNames("credits_needed").
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			// Charge gas for quote operation
			if !gasMeter.TryConsume(gas.GasCost(50)) {
				stack[0] = 0
				return
			}

			gasEstimate := stack[0]

			// Simple placeholder: 150 credits per 1k gas (configurable later)
			const creditsPerKiloGas = 150
			creditsNeeded := (gasEstimate * creditsPerKiloGas) / 1000
			if creditsNeeded == 0 && gasEstimate > 0 {
				creditsNeeded = 1 // Minimum 1 credit
			}

			stack[0] = creditsNeeded
		}), []api.ValueType{api.ValueTypeI64}, []api.ValueType{api.ValueTypeI64}).
		Export("credits_quote")

	// Emit event
	builder.NewFunctionBuilder().
		WithName("emit_event").
		WithParameterNames("event_type_ptr", "event_type_len", "event_data_ptr", "event_data_len").
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			// Charge gas for event emission
			if !gasMeter.TryConsume(gas.GasCost(300)) {
				return
			}

			eventTypePtr := uint32(stack[0])
			eventTypeLen := uint32(stack[1])
			eventDataPtr := uint32(stack[2])
			eventDataLen := uint32(stack[3])

			// Read event type and data from WASM memory
			memory := mod.Memory()
			eventTypeBytes, _ := memory.Read(eventTypePtr, eventTypeLen)
			eventDataBytes, _ := memory.Read(eventDataPtr, eventDataLen)

			// Add event to execution context
			event := &Event{
				Type: string(eventTypeBytes),
				Data: eventDataBytes,
			}
			execContext.events = append(execContext.events, event)
		}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{}).
		Export("emit_event")

	// L0 read-only functions for safely querying L0 state
	builder.NewFunctionBuilder().
		WithName("l0_read_account_json").
		WithParameterNames("url_ptr", "url_len", "output_ptr", "output_len_ptr").
		WithResultNames("result").
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			// Charge base gas for L0 query operation
			if !gasMeter.TryConsume(gas.GasCost(500)) {
				stack[0] = 1 // Error
				return
			}

			urlPtr := uint32(stack[0])
			urlLen := uint32(stack[1])
			outputPtr := uint32(stack[2])
			outputLenPtr := uint32(stack[3])

			// Read URL from memory
			memory := mod.Memory()
			urlBytes, ok := memory.Read(urlPtr, urlLen)
			if !ok {
				stack[0] = 1 // Error
				return
			}

			// Parse URL
			accountURL, err := accutil.ParseURL(string(urlBytes))
			if err != nil {
				stack[0] = 1 // Error
				return
			}

			// Query account from L0 (requires querier in execution context)
			if execContext.querier == nil {
				stack[0] = 1 // No querier available
				return
			}

			accountRecord, err := execContext.querier.QueryAccount(ctx, accountURL)
			if err != nil {
				stack[0] = 1 // Query failed
				return
			}

			// Marshal to JSON
			jsonBytes, err := json.Marshal(accountRecord.Account)
			if err != nil {
				stack[0] = 1 // Marshal error
				return
			}

			// Charge gas based on bytes returned
			if execContext.schedule != nil {
				gasPerByte := execContext.schedule.PerByte.Read
				gasCost := uint64(len(jsonBytes)) * gasPerByte
				if !gasMeter.TryConsume(gas.GasCost(gasCost)) {
					stack[0] = 1 // Out of gas
					return
				}
			} else {
				// Fallback gas cost
				if !gasMeter.TryConsume(gas.GasCost(uint64(len(jsonBytes)))) {
					stack[0] = 1 // Out of gas
					return
				}
			}

			// Write length to output length pointer
			if !memory.WriteUint32Le(outputLenPtr, uint32(len(jsonBytes))) {
				stack[0] = 1 // Memory write error
				return
			}

			// Write JSON data to output buffer
			if !memory.Write(outputPtr, jsonBytes) {
				stack[0] = 1 // Memory write error
				return
			}

			stack[0] = 0 // Success
		}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export("l0_read_account_json")

	builder.NewFunctionBuilder().
		WithName("l0_get_balance").
		WithParameterNames("url_ptr", "url_len", "output_ptr", "output_len_ptr").
		WithResultNames("result").
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			// Charge base gas for L0 query operation
			if !gasMeter.TryConsume(gas.GasCost(300)) {
				stack[0] = 1 // Error
				return
			}

			urlPtr := uint32(stack[0])
			urlLen := uint32(stack[1])
			outputPtr := uint32(stack[2])
			outputLenPtr := uint32(stack[3])

			// Read URL from memory
			memory := mod.Memory()
			urlBytes, ok := memory.Read(urlPtr, urlLen)
			if !ok {
				stack[0] = 1 // Error
				return
			}

			// Parse URL
			tokenURL, err := accutil.ParseURL(string(urlBytes))
			if err != nil {
				stack[0] = 1 // Error
				return
			}

			// Query token account from L0
			if execContext.querier == nil {
				stack[0] = 1 // No querier available
				return
			}

			accountRecord, err := execContext.querier.QueryAccount(ctx, tokenURL)
			if err != nil {
				stack[0] = 1 // Query failed
				return
			}

			var balanceStr string
			switch acc := accountRecord.Account.(type) {
			case *protocol.TokenAccount:
				balanceStr = acc.Balance.String()
			case *protocol.LiteTokenAccount:
				balanceStr = acc.Balance.String()
			default:
				stack[0] = 1 // Not a token account
				return
			}

			balanceBytes := []byte(balanceStr)

			// Charge gas for read operation
			if execContext.schedule != nil {
				gasPerByte := execContext.schedule.PerByte.Read
				gasCost := uint64(len(balanceBytes)) * gasPerByte
				if !gasMeter.TryConsume(gas.GasCost(gasCost)) {
					stack[0] = 1 // Out of gas
					return
				}
			}

			// Write length to output length pointer
			if !memory.WriteUint32Le(outputLenPtr, uint32(len(balanceBytes))) {
				stack[0] = 1 // Memory write error
				return
			}

			// Write balance string to output buffer
			if !memory.Write(outputPtr, balanceBytes) {
				stack[0] = 1 // Memory write error
				return
			}

			stack[0] = 0 // Success
		}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export("l0_get_balance")

	builder.NewFunctionBuilder().
		WithName("l0_read_data_entry").
		WithParameterNames("url_ptr", "url_len", "key_ptr", "key_len", "output_ptr", "output_len_ptr").
		WithResultNames("result").
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			// Charge base gas for L0 query operation
			if !gasMeter.TryConsume(gas.GasCost(400)) {
				stack[0] = 1 // Error
				return
			}

			urlPtr := uint32(stack[0])
			urlLen := uint32(stack[1])
			keyPtr := uint32(stack[2])
			keyLen := uint32(stack[3])
			outputPtr := uint32(stack[4])
			outputLenPtr := uint32(stack[5])

			// Read URL and key from memory
			memory := mod.Memory()
			urlBytes, ok := memory.Read(urlPtr, urlLen)
			if !ok {
				stack[0] = 1 // Error
				return
			}

			keyBytes, ok := memory.Read(keyPtr, keyLen)
			if !ok {
				stack[0] = 1 // Error
				return
			}

			// Parse URL
			dataURL, err := accutil.ParseURL(string(urlBytes))
			if err != nil {
				stack[0] = 1 // Error
				return
			}

			// Query data entry from L0
			if execContext.querier == nil {
				stack[0] = 1 // No querier available
				return
			}

			entryRecord, err := execContext.querier.QueryDataEntry(ctx, dataURL, string(keyBytes))
			if err != nil {
				stack[0] = 1 // Entry not found or other error
				return
			}

			var dataBytes []byte
			if entryRecord != nil && entryRecord.Entry != nil {
				dataBytes = entryRecord.Entry.GetData()
			}

			// Charge gas based on bytes read
			if execContext.schedule != nil {
				gasPerByte := execContext.schedule.PerByte.Read
				gasCost := uint64(len(dataBytes)) * gasPerByte
				if !gasMeter.TryConsume(gas.GasCost(gasCost)) {
					stack[0] = 1 // Out of gas
					return
				}
			} else {
				if !gasMeter.TryConsume(gas.GasCost(uint64(len(dataBytes)))) {
					stack[0] = 1 // Out of gas
					return
				}
			}

			// Write length to output length pointer
			if !memory.WriteUint32Le(outputLenPtr, uint32(len(dataBytes))) {
				stack[0] = 1 // Memory write error
				return
			}

			// Write data to output buffer
			if !memory.Write(outputPtr, dataBytes) {
				stack[0] = 1 // Memory write error
				return
			}

			stack[0] = 0 // Success
		}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export("l0_read_data_entry")

	builder.NewFunctionBuilder().
		WithName("l0_credits_quote").
		WithParameterNames("gas", "credits_ptr", "acme_ptr", "acme_len_ptr").
		WithResultNames("result").
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			// Charge base gas for calculation
			if !gasMeter.TryConsume(gas.GasCost(50)) {
				stack[0] = 1 // Error
				return
			}

			gasAmount := stack[0]
			creditsPtr := uint32(stack[1])
			acmePtr := uint32(stack[2])
			acmeLenPtr := uint32(stack[3])

			// Check if pricing schedule is available
			if execContext.schedule == nil {
				stack[0] = 1 // No pricing schedule available
				return
			}

			// Calculate credits using the pricing schedule
			credits := execContext.schedule.CalculateCredits(gasAmount)

			// Calculate ACME required
			acmeRequired := float64(credits) / execContext.schedule.GCR
			acmeStr := fmt.Sprintf("%.8f", acmeRequired)
			acmeBytes := []byte(acmeStr)

			memory := mod.Memory()

			// Write credits as u64
			if !memory.WriteUint64Le(creditsPtr, credits) {
				stack[0] = 1 // Memory write error
				return
			}

			// Write ACME length
			if !memory.WriteUint32Le(acmeLenPtr, uint32(len(acmeBytes))) {
				stack[0] = 1 // Memory write error
				return
			}

			// Write ACME decimal string
			if !memory.Write(acmePtr, acmeBytes) {
				stack[0] = 1 // Memory write error
				return
			}

			stack[0] = 0 // Success
		}), []api.ValueType{api.ValueTypeI64, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export("l0_credits_quote")

	return nil
}

// RegisterHostBindingsWithMode registers host functions with execution mode restrictions
func RegisterHostBindingsWithMode(ctx context.Context, builder wazero.HostModuleBuilder, hostAPI *host.API, gasMeter *gas.Meter, execContext *ExecutionContext, mode ExecMode) error {
	// Register all the basic read functions (always available)
	registerReadOnlyFunctions(ctx, builder, hostAPI, gasMeter)

	// Register event emission (allowed in both modes)
	registerEventFunctions(ctx, builder, gasMeter, execContext)

	// Register functions based on execution mode
	if mode == Execute {
		// Full execution mode - register all functions
		registerWriteFunctions(ctx, builder, hostAPI, gasMeter)
		registerL0Functions(ctx, builder, gasMeter, execContext)
	} else {
		// Query mode - register restricted versions that return errors
		registerRestrictedFunctions(ctx, builder)
	}

	return nil
}

// registerReadOnlyFunctions registers functions that are always safe to call
func registerReadOnlyFunctions(ctx context.Context, builder wazero.HostModuleBuilder, hostAPI *host.API, gasMeter *gas.Meter) {
	// State read operations
	builder.NewFunctionBuilder().
		WithName("accuwasm_get").
		WithParameterNames("key_ptr", "key_len", "value_ptr", "value_len").
		WithResultNames("result_len").
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			// Charge gas for read operation
			if !gasMeter.TryConsume(gas.GasCost(100)) {
				stack[0] = uint64(^uint32(0)) // Return error (-1 as uint32)
				return
			}

			keyPtr := uint32(stack[0])
			keyLen := uint32(stack[1])
			valuePtr := uint32(stack[2])
			valueLen := uint32(stack[3])

			result := hostAPI.Get(ctx, mod, keyPtr, keyLen, valuePtr, valueLen)
			stack[0] = uint64(result)
		}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export("accuwasm_get")

	// Iterator functions (read-only)
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

	// Logging functions (read-only)
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

	// Transaction context functions (read-only)
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

	// Gas functions (read-only)
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

	// Block context functions (read-only)
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

	// Credits pricing (read-only)
	builder.NewFunctionBuilder().
		WithName("credits_quote").
		WithParameterNames("gas_estimate").
		WithResultNames("credits_needed").
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			// Charge gas for quote operation
			if !gasMeter.TryConsume(gas.GasCost(50)) {
				stack[0] = 0
				return
			}

			gasEstimate := stack[0]

			// Simple placeholder: 150 credits per 1k gas (configurable later)
			const creditsPerKiloGas = 150
			creditsNeeded := (gasEstimate * creditsPerKiloGas) / 1000
			if creditsNeeded == 0 && gasEstimate > 0 {
				creditsNeeded = 1 // Minimum 1 credit
			}

			stack[0] = creditsNeeded
		}), []api.ValueType{api.ValueTypeI64}, []api.ValueType{api.ValueTypeI64}).
		Export("credits_quote")
}

// registerWriteFunctions registers state mutation functions (Execute mode only)
func registerWriteFunctions(ctx context.Context, builder wazero.HostModuleBuilder, hostAPI *host.API, gasMeter *gas.Meter) {
	builder.NewFunctionBuilder().
		WithName("accuwasm_set").
		WithParameterNames("key_ptr", "key_len", "value_ptr", "value_len").
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			// Charge gas for write operation
			if !gasMeter.TryConsume(gas.GasCost(200)) {
				return
			}

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
}

// registerL0Functions registers L0 operation functions (Execute mode only)
func registerL0Functions(ctx context.Context, builder wazero.HostModuleBuilder, gasMeter *gas.Meter, execContext *ExecutionContext) {
	builder.NewFunctionBuilder().
		WithName("l0_write_data").
		WithParameterNames("account_ptr", "account_len", "data_ptr", "data_len").
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			// Charge gas for L0 operation
			if !gasMeter.TryConsume(gas.GasCost(1000)) {
				return
			}

			accountPtr := uint32(stack[0])
			accountLen := uint32(stack[1])
			dataPtr := uint32(stack[2])
			dataLen := uint32(stack[3])

			// Read account URL and data from WASM memory
			memory := mod.Memory()
			accountBytes, _ := memory.Read(accountPtr, accountLen)
			dataBytes, _ := memory.Read(dataPtr, dataLen)

			// Stage L0 operation using URL-based constructor
			op, err := NewStagedOp("write_data", string(accountBytes))
			if err != nil {
				panic("l0_write_data: invalid account URL: " + err.Error())
			}
			op.Data = dataBytes
			execContext.stagedOps = append(execContext.stagedOps, op)
		}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{}).
		Export("l0_write_data")

	builder.NewFunctionBuilder().
		WithName("l0_send_tokens").
		WithParameterNames("from_ptr", "from_len", "to_ptr", "to_len", "amount").
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			// Charge gas for L0 operation
			if !gasMeter.TryConsume(gas.GasCost(1500)) {
				return
			}

			fromPtr := uint32(stack[0])
			fromLen := uint32(stack[1])
			toPtr := uint32(stack[2])
			toLen := uint32(stack[3])
			amount := stack[4]

			// Read addresses from WASM memory
			memory := mod.Memory()
			fromBytes, _ := memory.Read(fromPtr, fromLen)
			toBytes, _ := memory.Read(toPtr, toLen)

			// Stage L0 operation
			op := &StagedOp{
				Type:   "send_tokens",
				From:   string(fromBytes),
				To:     string(toBytes),
				Amount: amount,
			}
			execContext.stagedOps = append(execContext.stagedOps, op)
		}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI64}, []api.ValueType{}).
		Export("l0_send_tokens")

	builder.NewFunctionBuilder().
		WithName("l0_update_auth").
		WithParameterNames("account_ptr", "account_len", "auth_ptr", "auth_len").
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			// Charge gas for L0 operation
			if !gasMeter.TryConsume(gas.GasCost(800)) {
				return
			}

			accountPtr := uint32(stack[0])
			accountLen := uint32(stack[1])
			authPtr := uint32(stack[2])
			authLen := uint32(stack[3])

			// Read account URL and auth data from WASM memory
			memory := mod.Memory()
			accountBytes, _ := memory.Read(accountPtr, accountLen)
			authBytes, _ := memory.Read(authPtr, authLen)

			// Stage L0 operation using URL-based constructor
			op, err := NewStagedOp("update_auth", string(accountBytes))
			if err != nil {
				panic("l0_update_auth: invalid account URL: " + err.Error())
			}
			op.Data = authBytes
			execContext.stagedOps = append(execContext.stagedOps, op)
		}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{}).
		Export("l0_update_auth")
}

// registerEventFunctions registers event emission functions (allowed in both modes)
func registerEventFunctions(ctx context.Context, builder wazero.HostModuleBuilder, gasMeter *gas.Meter, execContext *ExecutionContext) {
	builder.NewFunctionBuilder().
		WithName("emit_event").
		WithParameterNames("event_type_ptr", "event_type_len", "event_data_ptr", "event_data_len").
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			// Charge gas for event emission
			if !gasMeter.TryConsume(gas.GasCost(300)) {
				return
			}

			eventTypePtr := uint32(stack[0])
			eventTypeLen := uint32(stack[1])
			eventDataPtr := uint32(stack[2])
			eventDataLen := uint32(stack[3])

			// Read event type and data from WASM memory
			memory := mod.Memory()
			eventTypeBytes, _ := memory.Read(eventTypePtr, eventTypeLen)
			eventDataBytes, _ := memory.Read(eventDataPtr, eventDataLen)

			// Add event to execution context
			event := &Event{
				Type: string(eventTypeBytes),
				Data: eventDataBytes,
			}
			execContext.events = append(execContext.events, event)
		}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{}).
		Export("emit_event")
}

// registerRestrictedFunctions registers restricted versions of functions for Query mode
func registerRestrictedFunctions(ctx context.Context, builder wazero.HostModuleBuilder) {
	// State write operations - return determinism errors
	builder.NewFunctionBuilder().
		WithName("accuwasm_set").
		WithParameterNames("key_ptr", "key_len", "value_ptr", "value_len").
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			// Abort with determinism error
			panic("accuwasm_set: state mutations not allowed in query mode")
		}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{}).
		Export("accuwasm_set")

	builder.NewFunctionBuilder().
		WithName("accuwasm_delete").
		WithParameterNames("key_ptr", "key_len").
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			// Abort with determinism error
			panic("accuwasm_delete: state mutations not allowed in query mode")
		}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{}).
		Export("accuwasm_delete")

	// L0 operations - return determinism errors
	builder.NewFunctionBuilder().
		WithName("l0_write_data").
		WithParameterNames("account_ptr", "account_len", "data_ptr", "data_len").
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			// Abort with determinism error
			panic("l0_write_data: L0 operations not allowed in query mode")
		}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{}).
		Export("l0_write_data")

	builder.NewFunctionBuilder().
		WithName("l0_send_tokens").
		WithParameterNames("from_ptr", "from_len", "to_ptr", "to_len", "amount").
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			// Abort with determinism error
			panic("l0_send_tokens: L0 operations not allowed in query mode")
		}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI64}, []api.ValueType{}).
		Export("l0_send_tokens")

	builder.NewFunctionBuilder().
		WithName("l0_update_auth").
		WithParameterNames("account_ptr", "account_len", "auth_ptr", "auth_len").
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			// Abort with determinism error
			panic("l0_update_auth: L0 operations not allowed in query mode")
		}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{}).
		Export("l0_update_auth")
}
