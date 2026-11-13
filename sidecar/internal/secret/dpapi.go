package secret

import (
	"bytes"
	"errors"
	"runtime"
	"unicode/utf8"
	"unsafe"

	"golang.org/x/sys/windows"
)

const MaxPlaintext = 4096
const description = "Easy Command password parameter"

var ErrInvalid = errors.New("密码须为有效 UTF-8，长度为 1 至 4096 字节，且不能含 NUL")
var ErrProtect = errors.New("无法使用 Windows DPAPI 保存密码")
var ErrDecrypt = errors.New("密文无法在当前用户下解密，请重新设置")
var ErrNotFound = errors.New("尚未保存此密码，请先设置")

type Entry struct {
	Key        string `json:"key"`
	Ciphertext []byte `json:"ciphertext"`
	UpdatedAt  string `json:"updatedAt"`
}

func valid(data []byte) bool {
	return len(data) > 0 && len(data) <= MaxPlaintext && utf8.Valid(data) && bytes.IndexByte(data, 0) == -1
}

func Protect(plaintext []byte) ([]byte, error) {
	if !valid(plaintext) {
		return nil, ErrInvalid
	}
	input := windows.DataBlob{Size: uint32(len(plaintext)), Data: &plaintext[0]}
	name, err := windows.UTF16PtrFromString(description)
	if err != nil {
		return nil, ErrProtect
	}
	var output windows.DataBlob
	err = windows.CryptProtectData(&input, name, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &output)
	runtime.KeepAlive(plaintext)
	if err != nil || output.Data == nil {
		return nil, ErrProtect
	}
	defer windows.LocalFree(windows.Handle(uintptr(unsafe.Pointer(output.Data))))
	return append([]byte(nil), unsafe.Slice(output.Data, int(output.Size))...), nil
}

func Unprotect(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) == 0 {
		return nil, ErrNotFound
	}
	if len(ciphertext) > 64<<10 {
		return nil, ErrDecrypt
	}
	input := windows.DataBlob{Size: uint32(len(ciphertext)), Data: &ciphertext[0]}
	var output windows.DataBlob
	err := windows.CryptUnprotectData(&input, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &output)
	runtime.KeepAlive(ciphertext)
	if err != nil || output.Data == nil {
		return nil, ErrDecrypt
	}
	native := unsafe.Slice(output.Data, int(output.Size))
	defer func() {
		clear(native)
		windows.LocalFree(windows.Handle(uintptr(unsafe.Pointer(output.Data))))
	}()
	if !valid(native) {
		return nil, ErrDecrypt
	}
	return append([]byte(nil), native...), nil
}
