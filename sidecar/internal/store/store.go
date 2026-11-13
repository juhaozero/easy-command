package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

var moveFileEx = syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW")

type Store struct {
	Root  string
	locks [64]sync.Mutex
}

func Open(root string) (*Store, error) {
	if !filepath.IsAbs(root) {
		return nil, errors.New("data directory must be absolute")
	}
	s := &Store{Root: filepath.Clean(root)}
	for _, dir := range []string{"", "config", "library", "library/scripts", "history", "logs", "state"} {
		path := s.Path(dir)
		if err := os.MkdirAll(path, 0700); err != nil {
			return nil, err
		}
		if err := CheckPath(path); err != nil {
			return nil, err
		}
	}
	settings := s.Path("config", "settings.json")
	var current map[string]json.RawMessage
	err := s.ReadJSON(settings, &current)
	if errors.Is(err, os.ErrNotExist) {
		err = s.WriteJSON(settings, struct{}{})
	}
	return s, err
}

func (s *Store) Path(parts ...string) string {
	return filepath.Join(append([]string{s.Root}, parts...)...)
}

// Paths sharing a stripe serialize as well, which bounds lock memory over time.
func (s *Store) lock(path string) func() {
	var hash uint32
	for _, char := range strings.ToLower(filepath.Clean(path)) {
		hash = hash*31 + uint32(char)
	}
	mutex := &s.locks[hash%uint32(len(s.locks))]
	mutex.Lock()
	return mutex.Unlock
}

func CheckPath(path string) error {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	attrs, err := syscall.GetFileAttributes(p)
	if err != nil {
		return &os.PathError{Op: "attributes", Path: path, Err: err}
	}
	if attrs&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return fmt.Errorf("reparse point is forbidden: %s", path)
	}
	return nil
}

func (s *Store) Read(path string, limit int64) ([]byte, error) {
	defer s.lock(path)()
	if err := CheckPath(path); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(io.LimitReader(file, limit+1))
}

func (s *Store) ReadJSON(path string, dst any) error {
	defer s.lock(path)()
	if err := CheckPath(path); err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}

func (s *Store) WriteJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return s.Write(path, append(data, '\n'))
}

func (s *Store) Write(path string, data []byte) error {
	defer s.lock(path)()
	return atomicWrite(path, func(file *os.File) error {
		_, err := file.Write(data)
		return err
	})
}

func atomicWrite(path string, write func(*os.File) error) error {
	if err := CheckPath(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	err = write(file)
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err = errors.Join(err, closeErr); err != nil {
		return err
	}
	return move(file.Name(), path, true)
}

// Move also publishes staged script directories. No cross-volume copy fallback.
func Move(source, target string) error {
	return move(source, target, false)
}

func move(source, target string, replace bool) error {
	from, err := syscall.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	to, err := syscall.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	flags := uintptr(8) // MOVEFILE_WRITE_THROUGH
	if replace {
		flags |= 1 // MOVEFILE_REPLACE_EXISTING
	}
	result, _, err := moveFileEx.Call(uintptr(unsafe.Pointer(from)), uintptr(unsafe.Pointer(to)), flags)
	if result == 0 {
		return &os.LinkError{Op: "MoveFileExW", Old: source, New: target, Err: err}
	}
	return nil
}
