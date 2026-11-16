//go:build !windows

package window

import "errors"

// ErrNotFound means the app has no visible top-level window.
var ErrNotFound = errors.New("window not found")

func RestoreTitle(string) error { return nil }

func FindPID(int) (uintptr, error) { return 0, ErrNotFound }

func FindTitled(int, string) (uintptr, error) { return 0, ErrNotFound }

func Raise(uintptr) error { return ErrNotFound }

func ConsoleWindowOf(int) (uintptr, error) { return 0, ErrNotFound }

func ConsoleWindow(int) (uintptr, error) { return 0, ErrNotFound }
