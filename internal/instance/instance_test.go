package instance

import (
	"errors"
	"testing"
)

func TestSingleInstance(t *testing.T) {
	dir := t.TempDir()
	l, err := Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(dir); !errors.Is(err, ErrRunning) {
		t.Fatalf("second Acquire: %v, want ErrRunning", err)
	}
	l.Release()
	l2, err := Acquire(dir)
	if err != nil {
		t.Fatalf("Acquire after release: %v", err)
	}
	l2.Release()
}
