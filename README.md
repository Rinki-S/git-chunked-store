# Git Chunked Store

一个 Git clean/smudge 过滤器，将大型二进制文件按 64KB 分片，以 SHA-256 内容寻址 + zlib 压缩存储，实现分片级去重。

**[English](README_en.md) | 中文**

## 工作原理

不同于 git-lfs 的整文件存储方式，`git-chunked-store` 将二进制文件按 64KB 切片，每个分片用 SHA-256 命名、zlib 压缩后存储。修改 1 GB 文件的一个字节，只会新增一个 64KB 分片——其余分片全部去重。

```
git add（clean 过滤器）                    git checkout（smudge 过滤器）
───────────────────────                    ─────────────────────────
工作区文件（二进制）                        Git 中存储的指针文件
        │                                           │
        ▼                                           ▼
  ┌──────────────────┐                      ┌──────────────────┐
  │ 按 64KB 切片      │                      │ 解析指针文件      │
  │ 每片计算 SHA-256  │                      │ 按 oid 读取分片   │
  │ zlib 压缩         │                      │ zlib 解压         │
  │ 去重后写入磁盘    │                      │ 按序拼接还原      │
  └──────────────────┘                      └──────────────────┘
        │                                           │
        ▼                                           ▼
  .git/chunked-objects/                     工作区文件（二进制）
  └── ab/c3d4e5f6...（zlib 压缩）
```

### 指针文件格式

Git 中只存储一份简短的指针文件，而非原始二进制：

```
version https://git-lfs-chunked/1
oid sha256:c17fa25800639d68256bcdf5fc1fbb98ed487265a7e292cf4cf00bb1ece4c33f
size 204800
chunk-size 65536
chunks 4
chunk-oids sha256:cef489e85c00... sha256:0b3e8860e838... sha256:80be0fd9404f... sha256:0a8053f6f58f...
```

### 分片存储

每个分片存储路径为：
```
.git/chunked-objects/<sha256前2位>/<sha256剩余62位>
```

内容经 zlib 压缩，相同分片（跨文件、跨版本）只存一份。

## 特性

- **分片级去重** — 仅变更的分片占用新空间
- **内容寻址** — SHA-256 校验确保每次读取的数据完整性
- **zlib 压缩** — 分片压缩存储，与 Git 松散对象一致
- **流式处理** — `ProcessCleanStreaming` 按 64KB 逐片处理，不将整个文件载入内存
- **原子写入** — 分片先写临时文件再 rename，防止崩溃导致数据损坏
- **路径穿越防护** — 所有 OID 严格校验为 64 字符小写 hex，杜绝恶意路径注入
- **优雅透传** — smudge 过滤器对非指针内容原样返回，`setup` 后对已有二进制文件安全无害
- **零外部依赖** — 纯 Go 实现，仅使用标准库

## 快速开始

```bash
# 编译
go build -o git-chunked-store .

# 在仓库中配置 git filter
./git-chunked-store setup

# 正常使用 git，过滤器自动生效
cp large-video.mp4 ./my-repo/
cd my-repo
git add .
git commit -m "add large binary"    # 自动触发 clean 过滤器
git checkout other-branch           # 自动触发 smudge 过滤器
```

`setup` 命令会配置三项 git 设置，并创建包含常见二进制文件类型的 `.gitattributes` 文件。

## 子命令

| 命令 | 触发方式 | 输入 | 输出 |
|------|---------|------|------|
| `clean` | `git add`（通过过滤器） | stdin 接收文件内容 | stdout 输出指针文件 |
| `smudge` | `git checkout`（通过过滤器） | stdin 接收指针文件 | stdout 输出文件内容 |
| `setup` | 手动执行 | — | 配置 git filter + `.gitattributes` |

## 项目结构

```
├── main.go                          CLI 入口
├── cmd/
│   ├── clean.go                     clean 过滤器 + 流式处理
│   ├── smudge.go                    smudge 过滤器 + 完整性校验
│   ├── setup.go                     git filter & .gitattributes 配置
│   └── integration_test.go          端到端集成测试
├── internal/
│   ├── chunker/
│   │   ├── chunker.go               64KB 分片 & SHA-256 哈希
│   │   └── chunker_test.go
│   ├── pointer/
│   │   ├── pointer.go               指针文件格式解析/序列化
│   │   └── pointer_test.go
│   └── store/
│       ├── store.go                  磁盘 I/O、zlib 压缩、去重、原子写入、OID 校验
│       └── store_test.go
├── go.mod
└── .gitattributes                   默认二进制文件类型映射
```

## 测试

```bash
go test ./... -cover -count=1
```

| 包 | 覆盖率 |
|---|--------|
| `internal/chunker` | 100.0% |
| `internal/pointer` | 94.8% |
| `internal/store` | 75.8% |
| `cmd` | 50.0% |

共 94 个测试用例，覆盖：

- 分片逻辑：空文件、不足一片、恰好整倍数、非整倍数、边界值
- 指针格式：序列化/解析往返、错误格式、空行、未知字段
- 存储层：存取往返、去重、zlib 压缩、原子写入、OID 校验、路径穿越拒绝
- 集成测试：clean→smudge 往返（所有文件大小）、完整性校验、非指针内容透传

## 架构决策

| 决策 | 选择 | 原因 |
|------|------|------|
| 文件类型区分 | `.gitattributes` 手动标记 | 与 git-lfs 一致，显式配置零性能浪费 |
| 分片大小 | 64KB | 兼顾粒度与开销 |
| 分片命名 | 原始内容 SHA-256 | 内容寻址天然去重 |
| 存储压缩 | zlib | 与 git objects 一致，减少磁盘占用 |
| 小文件处理 | 仍走分片（1 个分片） | 统一逻辑无特殊分支 |
| 路径结构 | `xx/xxxx...` 两级目录 | 与 git objects 一致，避免单目录文件过多 |
| OID 校验 | 仅允许 64 字符小写 hex | 防止路径穿越攻击 |
| Smudge 透传 | 非指针内容原样返回 | 已有二进制文件的仓库也能安全 setup |

## 安全性

- **OID 校验**：所有分片标识符必须为 64 字符小写 hex 编码的 SHA-256 哈希。非法字符（`/`、`..`、大写字母等）会被拒绝，杜绝路径穿越攻击。
- **完整性校验**：smudge 还原后自动计算全文 SHA-256，与指针文件中的 oid 比对，数据损坏时立即报错。
- **原子写入**：分片先写入 `.tmp` 文件，再 `rename` 到目标路径，崩溃不会留下半写文件。
- **并发安全**：同一分片的并发写入使用进程 ID + 原子计数器生成唯一临时文件名，rename 冲突时检测到目标已存在则视为成功（内容寻址保证数据一致）。

## 限制

- 分片存储在 `.git/chunked-objects/` 中，不会被 `git gc` 管理。如需清理未被引用的分片，需自行实现类似 `git gc` 的机制。
- 不支持分片级部分下载（git-lfs 的 `git lfs fetch --include` 类似功能）。
- `.gitattributes` 需要用户手动维护文件类型映射。
