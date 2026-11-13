package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
)

func (s *Store) AppendLine(path string, data []byte, durable bool) error {
	defer s.lock(path)()
	file, err := openAppend(path)
	if err != nil {
		return err
	}
	defer file.Close()
	end, err := file.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	if _, err = file.Write(append(data, '\n')); err != nil {
		return errors.Join(err, file.Truncate(end))
	}
	if durable {
		return file.Sync()
	}
	return nil
}

func openAppend(path string) (*os.File, error) {
	if err := CheckPath(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
}

// A process can die after writing a prefix. Discard only the incomplete final line.
func RepairAppendTail(path string) error {
	if err := CheckPath(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	end, err := file.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	offset := end
	var block [8192]byte
	for offset > 0 {
		count := min(offset, int64(len(block)))
		offset -= count
		if _, err := file.ReadAt(block[:count], offset); err != nil {
			return err
		}
		if index := bytes.LastIndexByte(block[:count], '\n'); index >= 0 {
			target := offset + int64(index) + 1
			if target == end {
				return nil
			}
			if err := file.Truncate(target); err != nil {
				return err
			}
			return file.Sync()
		}
	}
	if end == 0 {
		return nil
	}
	if err := file.Truncate(0); err != nil {
		return err
	}
	return file.Sync()
}

func (s *Store) Rewrite(path string, write func(io.Writer) error) error {
	defer s.lock(path)()
	return atomicWrite(path, func(file *os.File) error { return write(file) })
}

// UpdateJSONObject serializes the read-modify-write cycle and preserves sibling settings.
func (s *Store) UpdateJSONObject(path, key string, value any) error {
	return s.UpdateJSONFields(path, map[string]any{key: value})
}

func (s *Store) UpdateJSONFields(path string, patch map[string]any) error {
	defer s.lock(path)()
	if err := CheckPath(path); err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if fields == nil {
		fields = map[string]json.RawMessage{}
	}
	for key, value := range patch {
		fields[key], err = json.Marshal(value)
		if err != nil {
			return err
		}
	}
	updated, err := json.MarshalIndent(fields, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, func(file *os.File) error {
		_, err := file.Write(append(updated, '\n'))
		return err
	})
}
