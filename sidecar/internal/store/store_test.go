package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestAtomicWriteSurvivesProcessDeath(t *testing.T) {
	if path := os.Getenv("EASY_COMMAND_ATOMIC_TEST_PATH"); path != "" {
		err := atomicWrite(path, func(file *os.File) error {
			if _, err := file.WriteString(`{"half":`); err != nil {
				return err
			}
			if err := file.Sync(); err != nil {
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
	path := filepath.Join(root, "config", "test.json")
	if err := s.WriteJSON(path, map[string]int{"before": 1}); err != nil {
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
	if err != nil || !json.Valid(got) || !bytes.Equal(old, got) {
		t.Fatalf("invalid committed JSON %q: %v", got, err)
	}
	if err := s.WriteJSON(path, map[string]int{"after": 2}); err != nil {
		t.Fatal(err)
	}
	var updated map[string]int
	if err := s.ReadJSON(path, &updated); err != nil || updated["after"] != 2 || len(updated) != 1 {
		t.Fatalf("replace failed: %#v %v", updated, err)
	}
}
