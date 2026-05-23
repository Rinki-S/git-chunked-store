# Reproducible Demo

This demo shows how Git Chunked Store stores repeated binary content as
deduplicated chunks and how a small localized change adds only one new chunk.

The commands use an isolated temporary Git repository, so they do not modify the
project repository.

## 1. Build the binary

From the Git Chunked Store repository:

```bash
go build -o /tmp/git-chunked-store-demo-bin .
```

## 2. Create a demo repository

```bash
DEMO_DIR=$(mktemp -d /tmp/git-chunked-store-demo.XXXXXX)
cd "$DEMO_DIR"

git init
git config user.name "Demo User"
git config user.email "demo@example.com"

/tmp/git-chunked-store-demo-bin setup
```

## 3. Add an 8 MiB binary file

The file is filled with zero bytes. It contains 128 logical 64KB chunks, but all
chunks have the same content, so the chunk store only needs one physical chunk.

```bash
dd if=/dev/zero of=large.bin bs=1M count=8

git add .gitattributes large.bin
git commit -m "add large binary"

find .git/chunked-objects -type f ! -name "*.tmp*" | wc -l
```

Expected chunk count:

```text
1
```

## 4. Modify one 64KB chunk

This overwrites one chunk-sized region with `A` bytes.

```bash
perl -e 'open my $fh, "+<", "large.bin" or die $!; binmode $fh; seek($fh, 3 * 65536, 0) or die $!; print $fh "A" x 65536; close $fh or die $!;'

git add large.bin
git commit -m "modify one chunk"

find .git/chunked-objects -type f ! -name "*.tmp*" | wc -l
```

Expected chunk count:

```text
2
```

The second commit adds one new physical chunk: the modified 64KB region. The
unchanged zero-filled chunks still refer to the existing chunk.

## 5. Inspect store metrics

```bash
/tmp/git-chunked-store-demo-bin stats
```

Output from one run:

```text
Collecting chunk references from git history...
Scanning 2 commit(s)...
Reading 3 unique blob(s) with git cat-file --batch...
Stored chunks: 2
Referenced chunks: 2
Orphaned chunks: 0
Missing referenced chunks: 0
Logical chunk size: 128.0 KB (131072 bytes)
Compressed size: 177 B (177 bytes)
Orphaned compressed size: 0 B (0 bytes)
Compression ratio: 0.1%
```

`Logical chunk size` is 128KB because the store contains two unique chunks. The
working tree file is still 8 MiB; repeated chunk references are represented in
the pointer file rather than duplicated on disk.

## 6. Verify integrity

```bash
/tmp/git-chunked-store-demo-bin fsck
```

Output from one run:

```text
Scanning git history for chunked pointer files...
Checked 2 pointer file(s), 2 unique chunk(s)
OK: chunk store is consistent
```

## Summary

In this run:

```text
Initial physical chunks: 1
Physical chunks after one 64KB modification: 2
New physical chunks: 1
```

This is the intended behavior: content addressing deduplicates identical chunks,
and localized changes only add the chunks whose content changed.
