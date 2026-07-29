package fixproposals

import (
	"strings"
	"testing"
)

const simpleDiff = `--- a/main.go
+++ b/main.go
@@ -1,4 +1,4 @@
 package main

-func old() {}
+func new() {}
`

func TestParseUnifiedDiff_SingleFileSingleHunk(t *testing.T) {
	changes, err := ParseUnifiedDiff(simpleDiff)
	if err != nil {
		t.Fatalf("ParseUnifiedDiff() error: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("got %d file changes, want 1", len(changes))
	}
	fc := changes[0]
	if fc.OldPath != "main.go" || fc.NewPath != "main.go" {
		t.Errorf("paths = %q / %q, want main.go / main.go (a/ b/ prefixes stripped)", fc.OldPath, fc.NewPath)
	}
	if len(fc.Hunks) != 1 {
		t.Fatalf("got %d hunks, want 1", len(fc.Hunks))
	}
	h := fc.Hunks[0]
	if h.OldStart != 1 || h.OldLines != 4 || h.NewStart != 1 || h.NewLines != 4 {
		t.Errorf("hunk header = %+v", h)
	}
	if len(h.Lines) != 4 {
		t.Fatalf("got %d hunk lines, want 4", len(h.Lines))
	}
}

func TestParseUnifiedDiff_NewFile(t *testing.T) {
	diff := "--- /dev/null\n+++ b/new.txt\n@@ -0,0 +1,2 @@\n+line one\n+line two\n"
	changes, err := ParseUnifiedDiff(diff)
	if err != nil {
		t.Fatalf("ParseUnifiedDiff() error: %v", err)
	}
	if !changes[0].IsNew {
		t.Error("IsNew = false, want true for a /dev/null old path")
	}
	if changes[0].Path() != "new.txt" {
		t.Errorf("Path() = %q, want new.txt", changes[0].Path())
	}
}

func TestParseUnifiedDiff_DeletedFile(t *testing.T) {
	diff := "--- a/gone.txt\n+++ /dev/null\n@@ -1,2 +0,0 @@\n-line one\n-line two\n"
	changes, err := ParseUnifiedDiff(diff)
	if err != nil {
		t.Fatalf("ParseUnifiedDiff() error: %v", err)
	}
	if !changes[0].IsDeleted {
		t.Error("IsDeleted = false, want true for a /dev/null new path")
	}
	if changes[0].Path() != "gone.txt" {
		t.Errorf("Path() = %q, want gone.txt", changes[0].Path())
	}
}

func TestParseUnifiedDiff_MultipleFiles(t *testing.T) {
	diff := simpleDiff + "--- a/other.go\n+++ b/other.go\n@@ -1,1 +1,1 @@\n-a\n+b\n"
	changes, err := ParseUnifiedDiff(diff)
	if err != nil {
		t.Fatalf("ParseUnifiedDiff() error: %v", err)
	}
	if len(changes) != 2 {
		t.Fatalf("got %d file changes, want 2", len(changes))
	}
	if FilesChanged(changes)[0] != "main.go" || FilesChanged(changes)[1] != "other.go" {
		t.Errorf("FilesChanged() = %v", FilesChanged(changes))
	}
}

func TestParseUnifiedDiff_RangeWithoutCountDefaultsToOne(t *testing.T) {
	diff := "--- a/x\n+++ b/x\n@@ -5 +5 @@\n-old\n+new\n"
	changes, err := ParseUnifiedDiff(diff)
	if err != nil {
		t.Fatalf("ParseUnifiedDiff() error: %v", err)
	}
	h := changes[0].Hunks[0]
	if h.OldLines != 1 || h.NewLines != 1 {
		t.Errorf("hunk = %+v, want OldLines=1 NewLines=1", h)
	}
}

func TestParseUnifiedDiff_RejectsMalformedHunkLine(t *testing.T) {
	diff := "--- a/x\n+++ b/x\n@@ -1,1 +1,1 @@\n*not a valid diff line\n"
	if _, err := ParseUnifiedDiff(diff); err == nil {
		t.Fatal("expected an error for a hunk line without +/-/space prefix")
	}
}

func TestParseUnifiedDiff_RejectsEmptyDiff(t *testing.T) {
	if _, err := ParseUnifiedDiff(""); err == nil {
		t.Fatal("expected an error for an empty diff")
	}
}

func TestParseUnifiedDiff_RejectsHunkWithoutFileHeader(t *testing.T) {
	diff := "@@ -1,1 +1,1 @@\n-old\n+new\n"
	if _, err := ParseUnifiedDiff(diff); err == nil {
		t.Fatal("expected an error for a hunk with no preceding file header")
	}
}

func TestParseUnifiedDiff_IgnoresGitExtendedHeaders(t *testing.T) {
	diff := "diff --git a/main.go b/main.go\nindex abc123..def456 100644\n" + simpleDiff
	changes, err := ParseUnifiedDiff(diff)
	if err != nil {
		t.Fatalf("ParseUnifiedDiff() error: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("got %d file changes, want 1", len(changes))
	}
}

func TestParseUnifiedDiff_HandlesNoNewlineMarker(t *testing.T) {
	diff := "--- a/x\n+++ b/x\n@@ -1,1 +1,1 @@\n-old\n\\ No newline at end of file\n+new\n\\ No newline at end of file\n"
	changes, err := ParseUnifiedDiff(diff)
	if err != nil {
		t.Fatalf("ParseUnifiedDiff() error: %v", err)
	}
	if len(changes[0].Hunks[0].Lines) != 2 {
		t.Fatalf("Lines = %+v, want exactly the remove+add lines (marker skipped)", changes[0].Hunks[0].Lines)
	}
}

func TestParseUnifiedDiff_SkipsBlankLinesBetweenFileHeaders(t *testing.T) {
	diff := simpleDiff + "\n--- a/other.go\n+++ b/other.go\n@@ -1,1 +1,1 @@\n-a\n+b\n"
	if _, err := ParseUnifiedDiff(diff); err != nil {
		t.Fatalf("ParseUnifiedDiff() error: %v", err)
	}
}

func TestApplyFileChange_ModifiesExistingFile(t *testing.T) {
	changes, err := ParseUnifiedDiff(simpleDiff)
	if err != nil {
		t.Fatalf("ParseUnifiedDiff() error: %v", err)
	}
	original := "package main\n\nfunc old() {}\n"
	got, err := ApplyFileChange(original, changes[0])
	if err != nil {
		t.Fatalf("ApplyFileChange() error: %v", err)
	}
	want := "package main\n\nfunc new() {}\n"
	if got != want {
		t.Errorf("ApplyFileChange() = %q, want %q", got, want)
	}
}

func TestApplyFileChange_CreatesNewFile(t *testing.T) {
	diff := "--- /dev/null\n+++ b/new.txt\n@@ -0,0 +1,2 @@\n+line one\n+line two\n"
	changes, _ := ParseUnifiedDiff(diff)
	got, err := ApplyFileChange("", changes[0])
	if err != nil {
		t.Fatalf("ApplyFileChange() error: %v", err)
	}
	if got != "line one\nline two\n" {
		t.Errorf("ApplyFileChange() = %q", got)
	}
}

func TestApplyFileChange_RejectsContextMismatch(t *testing.T) {
	changes, _ := ParseUnifiedDiff(simpleDiff)
	// The file has drifted: "func old() {}" is no longer there.
	original := "package main\n\nfunc totally_different() {}\n"
	_, err := ApplyFileChange(original, changes[0])
	if err == nil || !strings.Contains(err.Error(), "does not apply cleanly") {
		t.Fatalf("ApplyFileChange() error = %v, want a does-not-apply-cleanly error", err)
	}
}

func TestApplyFileChange_RejectsHunkPastEndOfFile(t *testing.T) {
	diff := "--- a/x\n+++ b/x\n@@ -100,1 +100,1 @@\n-old\n+new\n"
	changes, _ := ParseUnifiedDiff(diff)
	_, err := ApplyFileChange("short\nfile\n", changes[0])
	if err == nil {
		t.Fatal("expected an error when the hunk starts past the end of the file")
	}
}

func TestApplyFileChange_PreservesNoTrailingNewline(t *testing.T) {
	diff := "--- a/x\n+++ b/x\n@@ -1,1 +1,1 @@\n-old\n+new\n"
	changes, _ := ParseUnifiedDiff(diff)
	got, err := ApplyFileChange("old", changes[0])
	if err != nil {
		t.Fatalf("ApplyFileChange() error: %v", err)
	}
	if got != "new" {
		t.Errorf("ApplyFileChange() = %q, want %q (no trailing newline, matching the original)", got, "new")
	}
}

func TestApplyFileChange_MultipleHunks(t *testing.T) {
	diff := "--- a/x\n+++ b/x\n" +
		"@@ -1,1 +1,1 @@\n-first\n+FIRST\n" +
		"@@ -5,1 +5,1 @@\n-fifth\n+FIFTH\n"
	changes, err := ParseUnifiedDiff(diff)
	if err != nil {
		t.Fatalf("ParseUnifiedDiff() error: %v", err)
	}
	original := "first\nsecond\nthird\nfourth\nfifth\n"
	got, err := ApplyFileChange(original, changes[0])
	if err != nil {
		t.Fatalf("ApplyFileChange() error: %v", err)
	}
	want := "FIRST\nsecond\nthird\nfourth\nFIFTH\n"
	if got != want {
		t.Errorf("ApplyFileChange() = %q, want %q", got, want)
	}
}
