package exec

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strings"
	"syscall"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"
)

const maxOutputText = 160 << 10
const truncation = "\n…[truncated]"
const decodingError = " [decode error]"

var multiByteToWide = kernel.NewProc("MultiByteToWideChar")
var getACP = kernel.NewProc("GetACP")
var isLeadByte = kernel.NewProc("IsDBCSLeadByteEx")

func decodeLine(raw []byte, codepage uintptr) (string, bool) {
	if codepage == 65001 {
		return string(bytes.ToValidUTF8(raw, []byte("�"))), !utf8.Valid(raw)
	}
	if len(raw) == 0 {
		return "", false
	}
	wide := make([]uint16, len(raw)*2)
	convert := func(part []byte) int {
		count, _, _ := multiByteToWide.Call(codepage, 8, uintptr(unsafe.Pointer(&part[0])), uintptr(len(part)), uintptr(unsafe.Pointer(&wide[0])), uintptr(len(wide)))
		return int(count)
	}
	if count := convert(raw); count != 0 {
		return string(utf16.Decode(wide[:count])), false
	}
	var result strings.Builder
	for len(raw) > 0 {
		lead, _, _ := isLeadByte.Call(codepage, uintptr(raw[0]))
		if lead != 0 && len(raw) > 1 {
			if count := convert(raw[:2]); count > 0 {
				result.WriteString(string(utf16.Decode(wide[:count])))
				raw = raw[2:]
				continue
			}
		}
		if count := convert(raw[:1]); count > 0 {
			result.WriteString(string(utf16.Decode(wide[:count])))
		} else {
			result.WriteRune('�')
		}
		raw = raw[1:]
	}
	return result.String(), true
}

func stream(reader io.Reader, codepage uintptr, send func(string, bool)) error {
	return streamOutput(reader, codepage, send, nil)
}

func streamOutput(reader io.Reader, codepage uintptr, send func(string, bool), log func(string, bool)) error {
	input := bufio.NewReader(reader)
	var line []byte
	var full []byte
	var logContinued bool
	const logBlock = 32 << 10
	truncated := false
	for {
		fragment, err := input.ReadSlice('\n')
		fragment = bytes.TrimSuffix(fragment, []byte{'\n'})
		if log != nil {
			full = append(full, fragment...)
			if len(full) >= logBlock && errors.Is(err, bufio.ErrBufferFull) {
				end := decodeBoundary(full, codepage)
				if end == len(full) && full[end-1] == '\r' {
					end--
				}
				value, _ := decodeLine(full[:end], codepage)
				log(value, true)
				logContinued = true
				full = append(full[:0], full[end:]...)
			}
			if !errors.Is(err, bufio.ErrBufferFull) && (len(full) > 0 || err == nil || logContinued) {
				value, _ := decodeLine(bytes.TrimSuffix(full, []byte{'\r'}), codepage)
				log(value, false)
				full = full[:0]
				logContinued = false
			}
		}
		available := maxOutputText - len(line)
		if len(fragment) > available {
			truncated = true
		}
		line = append(line, fragment[:min(len(fragment), available)]...)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if len(line) > 0 || err == nil || truncated {
			line = bytes.TrimSuffix(line, []byte{'\r'})
			value, invalid := decodeLine(line, codepage)
			if len(value) > maxOutputText {
				end := maxOutputText
				for !utf8.RuneStart(value[end]) {
					end--
				}
				value = value[:end]
				truncated = true
			}
			if invalid {
				value += decodingError
			}
			if truncated {
				value += truncation
			}
			send(value, truncated)
		}
		line = line[:0]
		truncated = false
		if errors.Is(err, io.EOF) || errors.Is(err, syscall.ERROR_BROKEN_PIPE) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func decodeBoundary(raw []byte, codepage uintptr) int {
	if codepage == 65001 {
		start := len(raw) - 1
		for start > len(raw)-4 && !utf8.RuneStart(raw[start]) {
			start--
		}
		if !utf8.FullRune(raw[start:]) {
			return start
		}
		return len(raw)
	}
	for offset := 0; offset < len(raw); {
		lead, _, _ := isLeadByte.Call(codepage, uintptr(raw[offset]))
		if lead != 0 && offset+1 == len(raw) {
			return offset
		}
		offset++
		if lead != 0 {
			offset++
		}
	}
	return len(raw)
}
