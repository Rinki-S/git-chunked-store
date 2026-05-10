# Git Chunked Store

**English | [中文](README.md)**

A Git clean/smudge filter that stores large binary files as 64KB content-addressed chunks with zlib compression and per-chunk deduplication.

## How It Works

Instead of storing entire binary files in Git (like git-lfs), `git-chunked-store` splits them into 64KB shards, each identified by its SHA-256 hash and compressed with zlib. When you modify a single byte in a 1 GB file, only one new 64KB chunk is stored — the rest are deduplicated.

```
git add (clean filter)                     git checkout (smudge filter)
─────────────────────                      ────────────────────────
Working file (binary)                      Pointer file in git
        │                                         │
        ▼                                         ▼
  ┌───────────────┐                        ┌──────────────┐
  │ Split 64KB    │                        │ Parse pointer│
  │ SHA-256 each  │                        │ Read oids    │
  │ zlib compress │                        │ zlib decompress│
  │ Dedup & store │                        │ Concatenate  │
  └───────────────┘                        └──────────────┘
        │                                         │
        ▼                                         ▼
  .git/chunked-objects/                   Working file (binary)
  └── ab/c3d4e5f6...  (zlib compressed)
```

### Pointer File Format

Git stores a small pointer file instead of the binary:

```
version https://git-lfs-chunked/1
oid sha256:c17fa25800639d68256bcdf5fc1fbb98ed487265a7e292cf4cf00bb1ece4c33f
size 204800
chunk-size 65536
chunks 4
chunk-oids sha256:cef489e85c00... sha256:0b3e8860e838... sha256:80be0fd9404f... sha256:0a8053f6f58f...
```

### Chunk Storage

Each chunk is stored at:
```
.git/chunked-objects/<first-2-hex-chars>/<remaining-62-chars>
```

Content is zlib-compressed, so identical chunks across different files or versions are stored only once.

## Features

- **Per-chunk deduplication** — only changed chunks take up new space
- **Content-addressed** — SHA-256 ensures data integrity on every read
- **zlib compression** — chunks are compressed on disk, just like Git loose objects
- **Streaming I/O** — `ProcessCleanStreaming` processes files in 64KB increments, never loading the entire file into memory
- **Atomic writes** — chunks are written to temp files and renamed, preventing corruption from crashes
- **Path traversal protection** — all OIDs are validated as 64-char lowercase hex strings
- **Graceful passthrough** — smudge filter transparently passes through non-pointer content, so setup is safe on repos with existing binaries
- **Zero external dependencies** — pure Go, only standard library

## Quick Start

```bash
# Build
go build -o git-chunked-store .

# Configure git filter in your repository
./git-chunked-store setup

# Normal git workflow — filters are automatic
cp large-video.mp4 ./my-repo/
cd my-repo
git add .
git commit -m "add large binary"    # clean filter triggers automatically
git checkout other-branch           # smudge filter triggers automatically
```

`setup` configures three git settings and creates a `.gitattributes` file with common binary types.

## Subcommands

| Command | Triggered by | Input | Output |
|---------|-------------|-------|--------|
| `clean` | `git add` (via filter) | File content from stdin | Pointer file to stdout |
| `smudge` | `git checkout` (via filter) | Pointer file from stdin | File content to stdout |
| `setup` | Manual | — | Configures git filter + `.gitattributes` |

## Project Structure

```
├── main.go                          CLI entry point
├── cmd/
│   ├── clean.go                     Clean filter + streaming clean
│   ├── smudge.go                    Smudge filter with integrity check
│   ├── setup.go                     Git filter & .gitattributes setup
│   └── integration_test.go          End-to-end tests
├── internal/
│   ├── chunker/
│   │   ├── chunker.go               64KB splitting & SHA-256 hashing
│   │   └── chunker_test.go
│   ├── pointer/
│   │   ├── pointer.go               Pointer file format parsing/serialization
│   │   └── pointer_test.go
│   └── store/
│       ├── store.go                  Disk I/O, zlib, dedup, atomic writes, OID validation
│       └── store_test.go
├── go.mod
└── .gitattributes                   Default binary file type mappings
```

## Testing

```bash
go test ./... -cover -count=1
```

| Package | Coverage |
|---------|----------|
| `internal/chunker` | 100.0% |
| `internal/pointer` | 94.8% |
| `internal/store` | 75.8% |
| `cmd` | 50.0% |

94 test cases total, covering:

- Chunk splitting: empty, partial, exact boundary, one-byte-over
- Pointer format: serialize/parse round-trip, error cases, blank lines, unknown keys
- Store: save/load round-trip, deduplication, zlib compression, atomic writes, OID validation, path traversal rejection
- Integration: clean→smudge round-trip for all file sizes, integrity verification, passthrough for non-pointer content

## Architecture Decisions

| Decision | Choice | Reason |
|----------|--------|--------|
| File type selection | `.gitattributes` | Explicit, matches git-lfs convention |
| Chunk size | 64KB | Standard chunk size, balances granularity and overhead |
| Chunk naming | SHA-256 of raw content | Content-addressed = natural deduplication |
| Storage compression | zlib | Matches git loose objects, reduces disk usage |
| Small files | Still chunked (1 chunk) | Unified logic, no special case |
| Path layout | `xx/xxxx...` two-level dirs | Mirrors git objects, avoids large directories |
| OID validation | 64-char lowercase hex only | Prevents path traversal attacks |
| Smudge passthrough | Non-pointer content returned as-is | Safe on repos with pre-existing binaries |

## Security

- **OID validation**: All chunk identifiers must be 64-character lowercase hex-encoded SHA-256 hashes. Characters such as `/`, `..`, and uppercase letters are rejected, preventing path traversal attacks.
- **Integrity verification**: The smudge filter automatically computes a SHA-256 hash of the reconstructed file and compares it against the `oid` in the pointer file. Corrupted data is detected and reported immediately.
- **Atomic writes**: Chunks are first written to `.tmp` files and then renamed to their target paths. A crash will never leave a partially written chunk on disk.
- **Concurrent safety**: Concurrent writes to the same chunk use process IDs and atomic counters to generate unique temp file names. If a rename fails because another process already wrote the same chunk, it is treated as success — content-addressed storage guarantees data integrity.

## Limitations

- Chunks are stored in `.git/chunked-objects/` and are not managed by `git gc`. Cleaning up unreferenced chunks requires a custom garbage-collection tool (not yet implemented).
- Partial chunk-level downloads are not supported (unlike git-lfs's `git lfs fetch --include`).
- `.gitattributes` mappings must be maintained manually by the user.
