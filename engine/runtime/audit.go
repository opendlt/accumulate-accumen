package runtime

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
)

// AuditReport contains the results of WASM module analysis
type AuditReport struct {
	Ok       bool     `json:"ok"`
	Reasons  []string `json:"reasons"`
	MemPages uint32   `json:"memPages"`
	Exports  []string `json:"exports"`
}

// WASM constants
const (
	WASMMagicNumber = 0x6d736100 // "\0asm"
	WASMVersion     = 1

	// Section types
	SectionTypeType     = 1
	SectionTypeImport   = 2
	SectionTypeFunction = 3
	SectionTypeTable    = 4
	SectionTypeMemory   = 5
	SectionTypeGlobal   = 6
	SectionTypeExport   = 7
	SectionTypeStart    = 8
	SectionTypeElement  = 9
	SectionTypeCode     = 10
	SectionTypeData     = 11

	// Value types
	ValueTypeI32 = 0x7F
	ValueTypeI64 = 0x7E
	ValueTypeF32 = 0x7D // FORBIDDEN
	ValueTypeF64 = 0x7C // FORBIDDEN

	// External types
	ExternalTypeFunc   = 0x00
	ExternalTypeTable  = 0x01
	ExternalTypeMemory = 0x02
	ExternalTypeGlobal = 0x03

	// Limits
	MaxMemoryPages = 256 // 16MB limit
	MaxTableSize   = 1024
	MaxExports     = 64
	MaxImports     = 32
)

// WASM opcodes that are forbidden for determinism
var forbiddenOpcodes = map[byte]string{
	// Float operations
	0x38: "f32.load",
	0x39: "f64.load",
	0x3A: "f32.store",
	0x3B: "f64.store",
	0x43: "f32.const",
	0x44: "f64.const",
	0x91: "f32.eq",
	0x92: "f32.ne",
	0x93: "f32.lt",
	0x94: "f32.gt",
	0x95: "f32.le",
	0x96: "f32.ge",
	0x97: "f64.eq",
	0x98: "f64.ne",
	0x99: "f64.lt",
	0x9A: "f64.gt",
	0x9B: "f64.le",
	0x9C: "f64.ge",
	0x9D: "f32.abs",
	0x9E: "f32.neg",
	0x9F: "f32.ceil",
	0xA0: "f32.floor",
	0xA1: "f32.trunc",
	0xA2: "f32.nearest",
	0xA3: "f32.sqrt",
	0xA4: "f32.add",
	0xA5: "f32.sub",
	0xA6: "f32.mul",
	0xA7: "f32.div",
	0xA8: "f32.min",
	0xA9: "f32.max",
	0xAA: "f32.copysign",
	0xAB: "f64.abs",
	0xAC: "f64.neg",
	0xAD: "f64.ceil",
	0xAE: "f64.floor",
	0xAF: "f64.trunc",
	0xB0: "f64.nearest",
	0xB1: "f64.sqrt",
	0xB2: "f64.add",
	0xB3: "f64.sub",
	0xB4: "f64.mul",
	0xB5: "f64.div",
	0xB6: "f64.min",
	0xB7: "f64.max",
	0xB8: "f64.copysign",
	0xB9: "i32.wrap_i64",
	0xBA: "i32.trunc_f32_s",
	0xBB: "i32.trunc_f32_u",
	0xBC: "i32.trunc_f64_s",
	0xBD: "i32.trunc_f64_u",
	0xBE: "i64.extend_i32_s",
	0xBF: "i64.extend_i32_u",
	0xC0: "i64.trunc_f32_s",
	0xC1: "i64.trunc_f32_u",
	0xC2: "i64.trunc_f64_s",
	0xC3: "i64.trunc_f64_u",
	0xC4: "f32.convert_i32_s",
	0xC5: "f32.convert_i32_u",
	0xC6: "f32.convert_i64_s",
	0xC7: "f32.convert_i64_u",
	0xC8: "f32.demote_f64",
	0xC9: "f64.convert_i32_s",
	0xCA: "f64.convert_i32_u",
	0xCB: "f64.convert_i64_s",
	0xCC: "f64.convert_i64_u",
	0xCD: "f64.promote_f32",
}

// Forbidden import namespaces and functions
var forbiddenImports = map[string][]string{
	"wasi_snapshot_preview1": {"*"}, // All WASI functions
	"wasi_unstable":          {"*"}, // Legacy WASI
	"env": {
		"__wbindgen_throw",     // wasm-bindgen non-deterministic
		"__wbindgen_rethrow",   // wasm-bindgen non-deterministic
		"Math.random",          // Non-deterministic
		"Date.now",             // Non-deterministic
		"performance.now",      // Non-deterministic
		"crypto.getRandomValues", // Non-deterministic
	},
}

// Allowed import namespace
const AllowedImportNamespace = "accumen"

// AuditWASMModule analyzes a WASM module for determinism compliance
func AuditWASMModule(wasmBytes []byte) *AuditReport {
	report := &AuditReport{
		Ok:      true,
		Reasons: []string{},
		Exports: []string{},
	}

	if len(wasmBytes) < 8 {
		report.Ok = false
		report.Reasons = append(report.Reasons, "module too small")
		return report
	}

	// Check magic number and version
	magic := binary.LittleEndian.Uint32(wasmBytes[:4])
	version := binary.LittleEndian.Uint32(wasmBytes[4:8])

	if magic != WASMMagicNumber {
		report.Ok = false
		report.Reasons = append(report.Reasons, "invalid WASM magic number")
		return report
	}

	if version != WASMVersion {
		report.Ok = false
		report.Reasons = append(report.Reasons, fmt.Sprintf("unsupported WASM version: %d", version))
		return report
	}

	// Parse sections
	offset := 8
	for offset < len(wasmBytes) {
		if offset+1 >= len(wasmBytes) {
			break
		}

		sectionType := wasmBytes[offset]
		offset++

		// Read section size
		sectionSize, bytesRead := readULEB128(wasmBytes[offset:])
		if bytesRead == 0 {
			report.Ok = false
			report.Reasons = append(report.Reasons, "invalid section size")
			return report
		}
		offset += bytesRead

		if offset+int(sectionSize) > len(wasmBytes) {
			report.Ok = false
			report.Reasons = append(report.Reasons, "section size exceeds module bounds")
			return report
		}

		sectionData := wasmBytes[offset : offset+int(sectionSize)]

		// Analyze section based on type
		switch sectionType {
		case SectionTypeType:
			if !auditTypeSection(sectionData, report) {
				report.Ok = false
			}
		case SectionTypeImport:
			if !auditImportSection(sectionData, report) {
				report.Ok = false
			}
		case SectionTypeMemory:
			if !auditMemorySection(sectionData, report) {
				report.Ok = false
			}
		case SectionTypeTable:
			if !auditTableSection(sectionData, report) {
				report.Ok = false
			}
		case SectionTypeExport:
			if !auditExportSection(sectionData, report) {
				report.Ok = false
			}
		case SectionTypeCode:
			if !auditCodeSection(sectionData, report) {
				report.Ok = false
			}
		case 0: // Custom section
			if !auditCustomSection(sectionData, report) {
				report.Ok = false
			}
		}

		offset += int(sectionSize)
	}

	return report
}

// auditTypeSection checks function type definitions for forbidden types
func auditTypeSection(data []byte, report *AuditReport) bool {
	offset := 0
	count, bytesRead := readULEB128(data[offset:])
	if bytesRead == 0 {
		return false
	}
	offset += bytesRead

	for i := uint32(0); i < count; i++ {
		if offset >= len(data) {
			return false
		}

		// Skip function type indicator (0x60)
		if data[offset] != 0x60 {
			report.Reasons = append(report.Reasons, "invalid function type")
			return false
		}
		offset++

		// Read parameter count
		paramCount, bytesRead := readULEB128(data[offset:])
		if bytesRead == 0 {
			return false
		}
		offset += bytesRead

		// Check parameter types
		for j := uint32(0); j < paramCount; j++ {
			if offset >= len(data) {
				return false
			}
			if data[offset] == ValueTypeF32 || data[offset] == ValueTypeF64 {
				report.Reasons = append(report.Reasons, "function parameter uses forbidden float type")
				return false
			}
			offset++
		}

		// Read result count
		resultCount, bytesRead := readULEB128(data[offset:])
		if bytesRead == 0 {
			return false
		}
		offset += bytesRead

		// Check result types
		for j := uint32(0); j < resultCount; j++ {
			if offset >= len(data) {
				return false
			}
			if data[offset] == ValueTypeF32 || data[offset] == ValueTypeF64 {
				report.Reasons = append(report.Reasons, "function result uses forbidden float type")
				return false
			}
			offset++
		}
	}

	return true
}

// auditImportSection validates import declarations
func auditImportSection(data []byte, report *AuditReport) bool {
	offset := 0
	count, bytesRead := readULEB128(data[offset:])
	if bytesRead == 0 {
		return false
	}
	offset += bytesRead

	if count > MaxImports {
		report.Reasons = append(report.Reasons, fmt.Sprintf("too many imports: %d (max: %d)", count, MaxImports))
		return false
	}

	for i := uint32(0); i < count; i++ {
		// Read module name
		moduleLen, bytesRead := readULEB128(data[offset:])
		if bytesRead == 0 || offset+bytesRead+int(moduleLen) > len(data) {
			return false
		}
		offset += bytesRead
		moduleName := string(data[offset : offset+int(moduleLen)])
		offset += int(moduleLen)

		// Read field name
		fieldLen, bytesRead := readULEB128(data[offset:])
		if bytesRead == 0 || offset+bytesRead+int(fieldLen) > len(data) {
			return false
		}
		offset += bytesRead
		fieldName := string(data[offset : offset+int(fieldLen)])
		offset += int(fieldLen)

		// Check import type
		if offset >= len(data) {
			return false
		}
		importType := data[offset]
		offset++

		// Validate import
		if !isAllowedImport(moduleName, fieldName) {
			report.Reasons = append(report.Reasons, fmt.Sprintf("forbidden import: %s.%s", moduleName, fieldName))
			return false
		}

		// Skip import descriptor based on type
		switch importType {
		case ExternalTypeFunc:
			// Skip function type index
			_, bytesRead := readULEB128(data[offset:])
			if bytesRead == 0 {
				return false
			}
			offset += bytesRead
		case ExternalTypeTable:
			// Skip table type (element type + limits)
			if offset+1 >= len(data) {
				return false
			}
			offset++ // element type
			// Read limits
			if offset >= len(data) {
				return false
			}
			hasMax := data[offset]
			offset++
			// Skip min
			_, bytesRead := readULEB128(data[offset:])
			if bytesRead == 0 {
				return false
			}
			offset += bytesRead
			if hasMax == 1 {
				// Skip max
				_, bytesRead := readULEB128(data[offset:])
				if bytesRead == 0 {
					return false
				}
				offset += bytesRead
			}
		case ExternalTypeMemory:
			// Skip memory type (limits)
			if offset >= len(data) {
				return false
			}
			hasMax := data[offset]
			offset++
			// Skip min
			_, bytesRead := readULEB128(data[offset:])
			if bytesRead == 0 {
				return false
			}
			offset += bytesRead
			if hasMax == 1 {
				// Skip max
				_, bytesRead := readULEB128(data[offset:])
				if bytesRead == 0 {
					return false
				}
				offset += bytesRead
			}
		case ExternalTypeGlobal:
			// Skip global type (value type + mutability)
			if offset+2 > len(data) {
				return false
			}
			offset += 2
		}
	}

	return true
}

// auditMemorySection validates memory declarations
func auditMemorySection(data []byte, report *AuditReport) bool {
	offset := 0
	count, bytesRead := readULEB128(data[offset:])
	if bytesRead == 0 {
		return false
	}
	offset += bytesRead

	if count > 1 {
		report.Reasons = append(report.Reasons, "multiple memory sections not allowed")
		return false
	}

	if count == 1 {
		// Read memory limits
		if offset >= len(data) {
			return false
		}
		hasMax := data[offset]
		offset++

		// Read min pages
		minPages, bytesRead := readULEB128(data[offset:])
		if bytesRead == 0 {
			return false
		}
		offset += bytesRead

		if minPages > MaxMemoryPages {
			report.Reasons = append(report.Reasons, fmt.Sprintf("memory size too large: %d pages (max: %d)", minPages, MaxMemoryPages))
			return false
		}

		report.MemPages = minPages

		if hasMax == 1 {
			// Read max pages
			maxPages, bytesRead := readULEB128(data[offset:])
			if bytesRead == 0 {
				return false
			}
			if maxPages > MaxMemoryPages {
				report.Reasons = append(report.Reasons, fmt.Sprintf("memory max size too large: %d pages (max: %d)", maxPages, MaxMemoryPages))
				return false
			}
			report.MemPages = maxPages
		}
	}

	return true
}

// auditTableSection validates table declarations
func auditTableSection(data []byte, report *AuditReport) bool {
	offset := 0
	count, bytesRead := readULEB128(data[offset:])
	if bytesRead == 0 {
		return false
	}
	offset += bytesRead

	for i := uint32(0); i < count; i++ {
		// Skip element type
		if offset >= len(data) {
			return false
		}
		offset++

		// Read limits
		if offset >= len(data) {
			return false
		}
		hasMax := data[offset]
		offset++

		// Read min size
		minSize, bytesRead := readULEB128(data[offset:])
		if bytesRead == 0 {
			return false
		}
		offset += bytesRead

		if minSize > MaxTableSize {
			report.Reasons = append(report.Reasons, fmt.Sprintf("table size too large: %d (max: %d)", minSize, MaxTableSize))
			return false
		}

		if hasMax == 1 {
			// Read max size
			maxSize, bytesRead := readULEB128(data[offset:])
			if bytesRead == 0 {
				return false
			}
			offset += bytesRead

			if maxSize > MaxTableSize {
				report.Reasons = append(report.Reasons, fmt.Sprintf("table max size too large: %d (max: %d)", maxSize, MaxTableSize))
				return false
			}
		}
	}

	return true
}

// auditExportSection validates exports and collects export names
func auditExportSection(data []byte, report *AuditReport) bool {
	offset := 0
	count, bytesRead := readULEB128(data[offset:])
	if bytesRead == 0 {
		return false
	}
	offset += bytesRead

	if count > MaxExports {
		report.Reasons = append(report.Reasons, fmt.Sprintf("too many exports: %d (max: %d)", count, MaxExports))
		return false
	}

	for i := uint32(0); i < count; i++ {
		// Read export name
		nameLen, bytesRead := readULEB128(data[offset:])
		if bytesRead == 0 || offset+bytesRead+int(nameLen) > len(data) {
			return false
		}
		offset += bytesRead
		exportName := string(data[offset : offset+int(nameLen)])
		offset += int(nameLen)

		report.Exports = append(report.Exports, exportName)

		// Skip export descriptor
		if offset >= len(data) {
			return false
		}
		exportType := data[offset]
		offset++

		// Skip index
		_, bytesRead = readULEB128(data[offset:])
		if bytesRead == 0 {
			return false
		}
		offset += bytesRead

		// Validate export type
		switch exportType {
		case ExternalTypeFunc, ExternalTypeTable, ExternalTypeMemory, ExternalTypeGlobal:
			// Valid export types
		default:
			report.Reasons = append(report.Reasons, fmt.Sprintf("invalid export type: %d", exportType))
			return false
		}
	}

	return true
}

// auditCodeSection validates function code for forbidden opcodes
func auditCodeSection(data []byte, report *AuditReport) bool {
	offset := 0
	count, bytesRead := readULEB128(data[offset:])
	if bytesRead == 0 {
		return false
	}
	offset += bytesRead

	for i := uint32(0); i < count; i++ {
		// Read function size
		funcSize, bytesRead := readULEB128(data[offset:])
		if bytesRead == 0 || offset+bytesRead+int(funcSize) > len(data) {
			return false
		}
		offset += bytesRead

		funcData := data[offset : offset+int(funcSize)]
		if !auditFunctionCode(funcData, report) {
			return false
		}

		offset += int(funcSize)
	}

	return true
}

// auditCustomSection validates custom sections for forbidden features
func auditCustomSection(data []byte, report *AuditReport) bool {
	if len(data) == 0 {
		return true
	}

	// Read section name
	nameLen, bytesRead := readULEB128(data)
	if bytesRead == 0 || nameLen > uint32(len(data)-bytesRead) {
		return false
	}

	sectionName := string(data[bytesRead : bytesRead+int(nameLen)])

	// Check for forbidden custom sections
	forbiddenSections := []string{
		"bulk-memory",
		"threads",
		"simd",
		"mutable-globals",
		"sign-extension",
		"multi-value",
		"reference-types",
	}

	for _, forbidden := range forbiddenSections {
		if strings.Contains(sectionName, forbidden) {
			report.Reasons = append(report.Reasons, fmt.Sprintf("forbidden custom section: %s", sectionName))
			return false
		}
	}

	return true
}

// auditFunctionCode scans function bytecode for forbidden opcodes
func auditFunctionCode(funcData []byte, report *AuditReport) bool {
	offset := 0

	// Skip locals declaration
	localCount, bytesRead := readULEB128(funcData[offset:])
	if bytesRead == 0 {
		return false
	}
	offset += bytesRead

	for i := uint32(0); i < localCount; i++ {
		// Skip count
		_, bytesRead := readULEB128(funcData[offset:])
		if bytesRead == 0 || offset+bytesRead >= len(funcData) {
			return false
		}
		offset += bytesRead

		// Check local type
		if funcData[offset] == ValueTypeF32 || funcData[offset] == ValueTypeF64 {
			report.Reasons = append(report.Reasons, "function local uses forbidden float type")
			return false
		}
		offset++
	}

	// Scan opcodes
	for offset < len(funcData) {
		opcode := funcData[offset]

		if opcodeName, forbidden := forbiddenOpcodes[opcode]; forbidden {
			report.Reasons = append(report.Reasons, fmt.Sprintf("forbidden opcode: %s (0x%02X)", opcodeName, opcode))
			return false
		}

		// Check for bulk memory opcodes (0xFC prefix)
		if opcode == 0xFC {
			if offset+1 < len(funcData) {
				extOpcode := funcData[offset+1]
				if extOpcode <= 11 { // Bulk memory opcodes 0-11
					report.Reasons = append(report.Reasons, fmt.Sprintf("forbidden bulk memory opcode: 0xFC 0x%02X", extOpcode))
					return false
				}
			}
		}

		// Check for SIMD opcodes (0xFD prefix)
		if opcode == 0xFD {
			report.Reasons = append(report.Reasons, "forbidden SIMD opcodes detected")
			return false
		}

		// Check for thread opcodes (0xFE prefix)
		if opcode == 0xFE {
			report.Reasons = append(report.Reasons, "forbidden thread/atomic opcodes detected")
			return false
		}

		offset++
	}

	return true
}

// isAllowedImport checks if an import is allowed
func isAllowedImport(moduleName, fieldName string) bool {
	// Only allow imports from our host namespace
	if moduleName == AllowedImportNamespace {
		return true
	}

	// Check forbidden imports
	if forbiddenFuncs, exists := forbiddenImports[moduleName]; exists {
		for _, forbiddenFunc := range forbiddenFuncs {
			if forbiddenFunc == "*" || forbiddenFunc == fieldName {
				return false
			}
		}
	}

	// By default, reject unknown namespaces for security
	return false
}

// readULEB128 reads an unsigned LEB128 encoded integer
func readULEB128(data []byte) (uint32, int) {
	var result uint32
	var shift uint
	var bytesRead int

	for i, b := range data {
		if i >= 5 { // Prevent overflow
			return 0, 0
		}

		result |= uint32(b&0x7F) << shift
		bytesRead++

		if b&0x80 == 0 {
			break
		}

		shift += 7
		if shift >= 32 {
			return 0, 0
		}
	}

	return result, bytesRead
}