# Git Chunked Storage Backend — 规划文档

---

## 1. 项目概述

实现一个基于分片的 git 存储后端，替代 git-lfs 的整文件存储方式。核心思路是将二进制大文件按 64KB 分片，以内容寻址（SHA-256 + zlib 压缩）存储分片，实现增量存储和分片级去重。

## 2. 核心设计

### 2.1 工作原理

```mermaid
flowchart TB
    subgraph Clean["clean filter（git add 触发）"]
        direction TB
        A1[工作区文件 binary.dat 1GB] --> A2[1. 读取文件全文]
        A2 --> A3[2. 计算全文 SHA-256 作为 oid]
        A3 --> A4[3. 按 64KB 切片]
        A4 --> A5[4. 每片计算 SHA-256]
        A5 --> A6[5. 对每片检查是否已存储 — 去重]
        A6 --> A7[6. 未存储的分片 → zlib 压缩后写入]
        A7 --> A8[7. 输出指针文件到 stdout]
    end

    A8 --> B1[.git/objects/ → 指针文件 几百字节]
    A7 --> B2[.git/chunked-objects/ → 各分片 zlib压缩 内容寻址去重]

    subgraph Smudge["smudge filter（git checkout 触发）"]
        direction TB
        C1[1. 从 stdin 读取指针文件] --> C2[2. 解析出 chunk-oids 列表]
        C2 --> C3[3. 逐片读取 → zlib 解压]
        C3 --> C4[4. 按序拼接还原完整文件]
        C4 --> C5[5. 输出到 stdout]
    end

    B1 -.->|指针文件| C1
    B2 -.->|按 oid 读取分片| C3
    C5 --> D1[工作区文件 binary.dat]
```

### 2.2 指针文件格式

```
version https://git-lfs-chunked/1
oid sha256:<全文SHA-256>
size <文件总字节数>
chunk-size 65536
chunks <分片总数>
chunk-oids sha256:<chunk1-hash> sha256:<chunk2-hash> ... sha256:<chunkN-hash>
```

- 小文件（< 64KB）时，只有一个分片，`chunks` 为 1
- 空文件时，`chunks` 为 0，`chunk-oids` 为空

### 2.3 分片存储格式

- 路径：`.git/chunked-objects/<sha256前2位>/<sha256剩余62位>`
- 内容：zlib 压缩后的原始分片数据
- 命名：分片的 **原始内容** SHA-256（压缩前计算，和 git objects 一致）

```
.git/chunked-objects/
├── a1/
│   └── b2c3d4e5f6...    ← zlib 压缩的分片数据
├── f6/
│   └── e7d8c9b0a1...    ← zlib 压缩的分片数据
└── ...
```

使用 2 位前缀做一级目录，避免单目录文件数过多，和 git objects 的 `xx/xxxx...` 结构一致。

### 2.4 去重机制

| 版本 | 操作 | chunked-objects 变化 |
|---|---|---|
| 首次 add 1GB 文件 | 存 16384 个分片 | +1GB（压缩后更小） |
| 修改头部后再次 add | 只新增 1 个分片 | +64KB（压缩后更小） |
| 总占用 | — | 所有出现过的不同分片的并集，每片仅存一份 |

### 2.5 zlib 压缩

- **写入时**：原始分片 → zlib 压缩 → 写入磁盘
- **读取时**：读取磁盘 → zlib 解压 → 得到原始分片
- **SHA-256**：始终基于**原始内容**计算，压缩是透明存储优化
- **性能开销**：1GB 文件约额外 1 秒，可忽略
- **存储收益**：压缩率取决于文件类型，文本类二进制（docx/xlsx）效果显著

## 3. 项目结构

```
interview-git-storage/
├── main.go                      # CLI 入口，注册子命令
├── cmd/
│   ├── clean.go                 # clean 子命令：stdin → 分片存储 + 指针文件 stdout
│   ├── smudge.go                # smudge 子命令：指针文件 stdin → 还原文件 stdout
│   └── setup.go                 # setup 子命令：配置 git filter 和 .gitattributes
├── internal/
│   ├── chunker/
│   │   └── chunker.go           # 分片逻辑：按 64KB 切片、SHA-256 计算
│   ├── store/
│   │   └── store.go             # 存储层：读写 .git/chunked-objects/、zlib 压缩/解压
│   └── pointer/
│       └── pointer.go           # 指针文件解析/序列化
├── go.mod
├── go.sum
└── .gitattributes               # filter=chunked 配置
```

## 4. 各模块详细设计

### 4.1 `cmd/clean.go` — Clean Filter

```go
func runClean(cmd *cobra.Command, args []string) {
    data := io.ReadAll(os.Stdin)
    fileSHA := sha256(data)
    fileSize := len(data)

    chunks := chunker.Split(data, 64*1024)

    chunkOids := []string{}
    for _, chunk := range chunks {
        oid := sha256(chunk)
        chunkOids = append(chunkOids, oid)
        store.Save(oid, chunk)
    }

    pointer := &Pointer{
        Oid:       fileSHA,
        Size:      fileSize,
        ChunkSize: 65536,
        Chunks:    len(chunkOids),
        ChunkOids: chunkOids,
    }
    fmt.Print(pointer.Serialize())
}
```

### 4.2 `cmd/smudge.go` — Smudge Filter

```go
func runSmudge(cmd *cobra.Command, args []string) {
    pointerData := io.ReadAll(os.Stdin)
    pointer := pointer.Parse(pointerData)

    var buf bytes.Buffer
    for _, oid := range pointer.ChunkOids {
        chunk := store.Load(oid)
        buf.Write(chunk)
    }
    os.Stdout.Write(buf.Bytes())
}
```

### 4.3 `internal/chunker/chunker.go` — 分片器

```go
func Split(data []byte, chunkSize int) [][]byte {
    var chunks [][]byte
    for offset := 0; offset < len(data); offset += chunkSize {
        end := min(offset+chunkSize, len(data))
        chunks = append(chunks, data[offset:end])
    }
    return chunks
}

func Hash(chunk []byte) string {
    h := sha256.Sum256(chunk)
    return hex.EncodeToString(h[:])
}
```

### 4.4 `internal/store/store.go` — 存储层

```go
type Store struct {
    basePath string  // .git/chunked-objects/
}

func (s *Store) Save(oid string, data []byte) error {
    path := s.oidToPath(oid)  // <basePath>/<前2位>/<剩余62位>
    if s.Exists(oid) {
        return nil  // 去重：已存在则跳过
    }
    os.MkdirAll(filepath.Dir(path), 0755)
    compressed := zlibCompress(data)
    return os.WriteFile(path, compressed, 0644)
}

func (s *Store) Load(oid string) ([]byte, error) {
    path := s.oidToPath(oid)
    compressed, _ := os.ReadFile(path)
    return zlibDecompress(compressed)
}

func (s *Store) Exists(oid string) bool {
    _, err := os.Stat(s.oidToPath(oid))
    return err == nil
}
```

### 4.5 `internal/pointer/pointer.go` — 指针文件

```go
type Pointer struct {
    Version   string   // "https://git-lfs-chunked/1"
    Oid       string   // 全文 SHA-256
    Size      int64    // 文件总大小
    ChunkSize int      // 65536
    Chunks    int      // 分片数量
    ChunkOids []string // 各分片 SHA-256
}

func (p *Pointer) Serialize() string {
    // 输出格式：
    // version https://git-lfs-chunked/1
    // oid sha256:<oid>
    // size <size>
    // chunk-size 65536
    // chunks <n>
    // chunk-oids sha256:<oid1> sha256:<oid2> ...
}

func Parse(data []byte) *Pointer {
    // 反向解析
}
```

### 4.6 `cmd/setup.go` — 自动配置

```go
func runSetup(cmd *cobra.Command, args []string) {
    binPath, _ := filepath.Abs("git-chunked-store")

    exec.Command("git", "config", "filter.chunked.clean", binPath+" clean").Run()
    exec.Command("git", "config", "filter.chunked.smudge", binPath+" smudge").Run()
    exec.Command("git", "config", "filter.chunked.required", "true").Run()
}
```

## 5. .gitattributes 配置

```gitattributes
# 使用 .gitattributes 手动标记哪些文件类型走分片存储
*.bin filter=chunked
*.zip filter=chunked
*.tar filter=chunked
*.gz filter=chunked
*.mp4 filter=chunked
*.avi filter=chunked
*.pdf filter=chunked
*.docx filter=chunked
*.xlsx filter=chunked
*.pptx filter=chunked
*.exe filter=chunked
*.dll filter=chunked
*.so filter=chunked
*.dylib filter=chunked
*.iso filter=chunked
*.dmg filter=chunked
```

用户可以按需增减，完全由 `.gitattributes` 控制。

## 6. 使用流程

```bash
# 1. 编译
go build -o git-chunked-store .

# 2. 初始化（自动配置 git filter）
./git-chunked-store setup

# 3. 正常使用 git，无需额外命令
cp big-file.bin ./repo/
cd repo
git add .
git commit -m "add big binary"      # clean filter 自动触发
git checkout other-branch            # smudge filter 自动触发

# 4. 查看分片存储
ls .git/chunked-objects/
```

## 7. 关键设计决策总结

| 决策 | 选择 | 原因 |
|---|---|---|
| 文件类型区分 | `.gitattributes` 手动标记 | 和 git-lfs 一致，显式配置，零性能浪费 |
| 分片大小 | 64KB | 题目要求 |
| 分片命名 | SHA-256（原始内容） | 内容寻址天然去重 |
| 存储压缩 | zlib | 和 git objects 一致，减少磁盘占用 |
| 小文件处理 | 仍走分片（1个分片） | 统一逻辑，简化实现 |
| 分片路径结构 | `xx/xxxx...`两级目录 | 避免单目录文件数过多 |
| 指针文件格式 | 类 git-lfs，扩展 chunk-oids 字段 | 兼容性好，可读性强 |

## 8. 可能的边界情况

- **空文件**：0 个分片，指针文件中 `chunks 0`，smudge 时输出空文件
- **恰好 64KB 整倍数的文件**：无特殊处理，最后一个分片刚好 64KB
- **非整倍数文件**：最后一个分片 < 64KB，正常存储
- **分片丢失**：smudge 时如果分片文件不存在，报错（不应发生，除非用户手动删除）
- **并发写入同一分片**：由于 SHA-256 碰撞概率极低，无需加锁；即使并发写同一文件，内容相同则幂等
