package bash

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"easy-command/sidecar/internal/store"
)

type Result struct {
	Path   *string `json:"path"`
	Source *string `json:"source"`
}

type Settings struct {
	store *store.Store
}

var ErrInvalidPath = errors.New("请选择 Git for Windows 安装目录中的 bash.exe")

func New(db *store.Store) Settings { return Settings{store: db} }

func (s Settings) read() (map[string]json.RawMessage, error) {
	fields := make(map[string]json.RawMessage)
	err := s.store.ReadJSON(s.store.Path("config", "settings.json"), &fields)
	if fields == nil {
		fields = make(map[string]json.RawMessage)
	}
	return fields, err
}

func (s Settings) OutputEncoding() (string, error) {
	fields, err := s.read()
	if err != nil {
		return "", err
	}
	var encoding string
	if raw := fields["outputEncoding"]; raw != nil {
		if err := json.Unmarshal(raw, &encoding); err != nil {
			return "", err
		}
	}
	if encoding == "" {
		encoding = "ansi"
	}
	if encoding != "ansi" && encoding != "utf-8" {
		return "", errors.New("outputEncoding 必须为 ansi 或 utf-8")
	}
	return encoding, nil
}

func (s Settings) Detect() (Result, error) {
	fields, err := s.read()
	if err != nil {
		return Result{}, err
	}
	var manual string
	if raw := fields["bashPath"]; raw != nil {
		if err := json.Unmarshal(raw, &manual); err != nil {
			return Result{}, err
		}
	}
	if manual != "" {
		if path := valid(manual); path != "" {
			source := "manual"
			return Result{&path, &source}, nil
		}
		return Result{}, nil
	}
	for _, view := range []uint32{syscall.KEY_WOW64_64KEY, syscall.KEY_WOW64_32KEY} {
		if root := installPath(view); root != "" {
			if path := valid(filepath.Join(root, "bin", "bash.exe")); path != "" {
				source := "registry"
				return Result{&path, &source}, nil
			}
		}
	}
	for _, root := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), `C:\Program Files`, `C:\Program Files (x86)`} {
		if path := valid(filepath.Join(root, "Git", "bin", "bash.exe")); path != "" {
			source := "common"
			return Result{&path, &source}, nil
		}
	}
	return Result{}, nil
}

func (s Settings) SetPath(path *string) (Result, error) {
	if path != nil {
		resolved := valid(*path)
		if resolved == "" {
			return Result{}, ErrInvalidPath
		}
		path = &resolved
	}
	if err := s.store.UpdateJSONObject(s.store.Path("config", "settings.json"), "bashPath", path); err != nil {
		return Result{}, err
	}
	return s.Detect()
}

func valid(path string) string {
	if !filepath.IsAbs(path) || !strings.EqualFold(filepath.Base(path), "bash.exe") {
		return ""
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return ""
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() || !strings.EqualFold(filepath.Base(resolved), "bash.exe") {
		return ""
	}
	windows := os.Getenv("SystemRoot")
	if windows == "" {
		windows = `C:\Windows`
	}
	for _, folder := range []string{"System32", "SysWOW64", "Sysnative", ""} {
		systemPath := filepath.Join(windows, folder, "bash.exe")
		if strings.EqualFold(resolved, systemPath) {
			return ""
		}
		if systemInfo, err := os.Stat(systemPath); err == nil && os.SameFile(info, systemInfo) {
			return ""
		}
	}
	// Git for Windows ships git.exe at the installation root or in cmd.
	root := filepath.Dir(filepath.Dir(resolved))
	if strings.EqualFold(filepath.Base(filepath.Dir(resolved)), "bin") && strings.EqualFold(filepath.Base(root), "usr") {
		root = filepath.Dir(root)
	}
	for _, git := range []string{filepath.Join(root, "git.exe"), filepath.Join(root, "cmd", "git.exe")} {
		if info, err := os.Stat(git); err == nil && info.Mode().IsRegular() {
			return resolved
		}
	}
	return ""
}

func installPath(view uint32) string {
	keyName, _ := syscall.UTF16PtrFromString(`SOFTWARE\GitForWindows`)
	valueName, _ := syscall.UTF16PtrFromString("InstallPath")
	var key syscall.Handle
	if syscall.RegOpenKeyEx(syscall.HKEY_LOCAL_MACHINE, keyName, 0, syscall.KEY_QUERY_VALUE|view, &key) != nil {
		return ""
	}
	defer syscall.RegCloseKey(key)
	var typ uint32
	var value [4096]uint16
	size := uint32(unsafe.Sizeof(value))
	if syscall.RegQueryValueEx(key, valueName, nil, &typ, (*byte)(unsafe.Pointer(&value[0])), &size) != nil || typ != syscall.REG_SZ || size%2 != 0 || size > uint32(unsafe.Sizeof(value)) {
		return ""
	}
	return syscall.UTF16ToString(value[:size/2])
}
