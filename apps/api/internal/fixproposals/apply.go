package fixproposals

import (
	"fmt"
	"strings"
)

// ErrPatchDoesNotApply is returned when a hunk's context/removed lines
// don't match the file being patched — the file has drifted since the
// diff was generated, or the diff is wrong. Never applied partially.
var ErrPatchDoesNotApply = fmt.Errorf("fixproposals: patch does not apply cleanly")

// ApplyFileChange applies fc's hunks to original (the file's current
// content, "" for a new file) and returns the resulting content. It
// verifies every context and removed line against original before
// changing anything conceptually — on any mismatch it returns
// ErrPatchDoesNotApply rather than writing a corrupted result.
func ApplyFileChange(original string, fc FileChange) (string, error) {
	origLines := splitLines(original)
	var result []string
	origIdx := 0

	for _, h := range fc.Hunks {
		target := h.OldStart - 1
		if fc.IsNew {
			target = 0
		}
		if target < origIdx || target > len(origLines) {
			return "", fmt.Errorf("%w: hunk starting at line %d is out of range for a %d-line file", ErrPatchDoesNotApply, h.OldStart, len(origLines))
		}

		result = append(result, origLines[origIdx:target]...)
		origIdx = target

		for _, dl := range h.Lines {
			switch dl.Kind {
			case LineContext, LineRemove:
				if origIdx >= len(origLines) {
					return "", fmt.Errorf("%w: expected %q at line %d, but the file has only %d lines", ErrPatchDoesNotApply, dl.Text, origIdx+1, len(origLines))
				}
				if origLines[origIdx] != dl.Text {
					return "", fmt.Errorf("%w: line %d is %q, expected %q", ErrPatchDoesNotApply, origIdx+1, origLines[origIdx], dl.Text)
				}
				if dl.Kind == LineContext {
					result = append(result, dl.Text)
				}
				origIdx++
			case LineAdd:
				result = append(result, dl.Text)
			}
		}
	}

	result = append(result, origLines[origIdx:]...)

	joined := strings.Join(result, "\n")
	if len(result) > 0 && (original == "" || strings.HasSuffix(original, "\n")) {
		joined += "\n"
	}
	return joined, nil
}

// splitLines splits s into lines without a trailing empty element for a
// final newline (unlike strings.Split, which would leave a trailing "").
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	trimmed := strings.TrimSuffix(s, "\n")
	return strings.Split(trimmed, "\n")
}

// FilesChanged returns the set of file paths a parsed diff touches, in
// diff order.
func FilesChanged(changes []FileChange) []string {
	out := make([]string, 0, len(changes))
	for _, fc := range changes {
		out = append(out, fc.Path())
	}
	return out
}
