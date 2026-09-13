package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// activeWriteAttempts bounds the read-compare-write loop below. Storing a
// selection touches one field; if the document is rewritten underneath that
// more often than this, reporting the conflict beats spinning.
const activeWriteAttempts = 3

// readActiveFile is a seam. The retry below only runs when the document changes
// between the read that builds the write and the read that checks it, and that
// is not something a test can arrange through the filesystem on both platforms.
// Swapping this in a test is the only way to exercise the loop deterministically.
var readActiveFile = os.ReadFile

// SetActive stores which provider the bare `clother` invocation should launch
// and which model tag to use with it.
//
// It takes a path rather than a *File deliberately, because it has to merge.
// The whole-file writers — `clother install` and `clother config` — load, modify
// and save this same document, and a selection made from inside a running
// session must not be able to revert something they wrote in the meantime. So
// this re-reads the file, keeps whatever every other field holds, and abandons
// its own write when the content changed under it, retrying instead. There is
// no cross-platform file lock in this module's dependency set, and a lost update
// here would silently undo a choice the user made in another window.
func SetActive(path, profile, model string) error {
	for attempt := 0; attempt < activeWriteAttempts; attempt++ {
		before, err := readActiveFile(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}

		cfg, err := decodeConfig(before)
		if err != nil {
			return fmt.Errorf("decode %s: %w", path, err)
		}
		cfg.Active = &Active{Profile: profile, Model: model}

		encoded, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			return err
		}
		encoded = append(encoded, '\n')

		// A rewrite that landed since the read above means this document was
		// built from a stale copy: drop it and look again rather than apply it.
		current, err := readActiveFile(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if !bytes.Equal(current, before) {
			continue
		}

		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		return writeAtomic(path, encoded, 0o644)
	}

	return fmt.Errorf("could not store the active provider in %s: the file changed underneath every attempt", path)
}
