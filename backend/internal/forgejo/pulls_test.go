package forgejo_test

import (
	"testing"

	"github.com/nielsuitterdijk22/quill/internal/forgejo"
)

const sampleDiff = `diff --git a/README.md b/README.md
index 352d55d..64e7b77 100644
--- a/README.md
+++ b/README.md
@@ -1,3 +1,4 @@ heading
 # widget
 
-old line
+new line
+added line
diff --git a/new.txt b/new.txt
new file mode 100644
index 0000000..e69de29
--- /dev/null
+++ b/new.txt
@@ -0,0 +1,2 @@
+hello
+world
diff --git a/gone.txt b/gone.txt
deleted file mode 100644
index e69de29..0000000
--- a/gone.txt
+++ /dev/null
@@ -1 +0,0 @@
-bye
`

func TestParseUnifiedDiff(t *testing.T) {
	files := forgejo.ParseUnifiedDiff(sampleDiff)
	if len(files) != 3 {
		t.Fatalf("expected 3 files, got %d", len(files))
	}

	readme := files[0]
	if readme.Path != "README.md" || readme.Status != "modified" {
		t.Fatalf("readme: path=%q status=%q", readme.Path, readme.Status)
	}
	if readme.Additions != 2 || readme.Deletions != 1 {
		t.Fatalf("readme stats: +%d -%d", readme.Additions, readme.Deletions)
	}
	if len(readme.Hunks) != 1 {
		t.Fatalf("readme: expected 1 hunk, got %d", len(readme.Hunks))
	}

	// The leading context line keeps both old and new line numbers.
	ctx := findLine(readme.Hunks[0].Lines, forgejo.DiffLineContext, "# widget")
	if ctx == nil || ctx.OldNumber != 1 || ctx.NewNumber != 1 {
		t.Fatalf("context line numbers wrong: %+v", ctx)
	}
	// An added line has a new number but no old number.
	add := findLine(readme.Hunks[0].Lines, forgejo.DiffLineAdd, "new line")
	if add == nil || add.NewNumber != 3 || add.OldNumber != 0 {
		t.Fatalf("added line numbers wrong: %+v", add)
	}
	// A deleted line has an old number but no new number.
	del := findLine(readme.Hunks[0].Lines, forgejo.DiffLineDel, "old line")
	if del == nil || del.OldNumber != 3 || del.NewNumber != 0 {
		t.Fatalf("deleted line numbers wrong: %+v", del)
	}

	added := files[1]
	if added.Path != "new.txt" || added.Status != "added" || added.Additions != 2 {
		t.Fatalf("added file: %+v", added)
	}

	deleted := files[2]
	if deleted.Path != "gone.txt" || deleted.Status != "deleted" || deleted.Deletions != 1 {
		t.Fatalf("deleted file: %+v", deleted)
	}
}

func TestParseUnifiedDiffEmpty(t *testing.T) {
	if files := forgejo.ParseUnifiedDiff(""); len(files) != 0 {
		t.Fatalf("expected no files for empty diff, got %d", len(files))
	}
}

func findLine(lines []forgejo.DiffLine, typ, content string) *forgejo.DiffLine {
	for i := range lines {
		if lines[i].Type == typ && lines[i].Content == content {
			return &lines[i]
		}
	}
	return nil
}
