package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestAtomicWriteSurvivesProcessDeath(t *testing.T) {
	if path := os.Getenv("EASY_COMMAND_ATOMIC_TEST_PATH"); path != "" {
		s, err := Open(filepath.Dir(filepath.Dir(path)))
		if err != nil {
			t.Fatal(err)
		}
		err = s.Rewrite(path, func(file io.Writer) error {
			if _, err := io.WriteString(file, `{"half":`); err != nil {
				return err
			}
			if err := os.WriteFile(path+".ready", nil, 0600); err != nil {
				return err
			}
			time.Sleep(30 * time.Second)
			return fmt.Errorf("parent did not stop writer")
		})
		t.Fatal(err)
	}
	root := t.TempDir()
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "history", "runs.jsonl")
	if err := s.Write(path, []byte("{\"before\":1}\n{\"before\":2}\n")); err != nil {
		t.Fatal(err)
	}
	old, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestAtomicWriteSurvivesProcessDeath$")
	child.Env = append(os.Environ(), "EASY_COMMAND_ATOMIC_TEST_PATH="+path)
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Process.Kill(); _ = child.Wait() })
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(path + ".ready"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child never reached partial write")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := child.Wait(); err == nil {
		t.Fatal("child should have been killed")
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(old, got) {
		t.Fatalf("invalid committed JSON %q: %v", got, err)
	}
	for _, line := range bytes.Split(bytes.TrimSpace(got), []byte{'\n'}) {
		if !json.Valid(line) {
			t.Fatalf("invalid history line: %q", line)
		}
	}
	if err := s.Rewrite(path, func(w io.Writer) error {
		return json.NewEncoder(w).Encode(map[string]int{"after": 2})
	}); err != nil {
		t.Fatal(err)
	}
	var updated map[string]int
	if err := s.ReadJSON(path, &updated); err != nil || updated["after"] != 2 || len(updated) != 1 {
		t.Fatalf("replace failed: %#v %v", updated, err)
	}
}
