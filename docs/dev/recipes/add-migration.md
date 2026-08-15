# 配方：加一个数据库迁移

## 何时用

要给 `data/emo.db` 加表、加列或改索引时。

MemoryCore 的 `data/memory.db` **不在此列** —— 它的 schema 归 MemoryCore 自己管，从本仓库改它违反 [`../invariants.md`](../invariants.md) 第 2 条。

## 锚点文件

| 锚点 | 作用 |
|---|---|
| `internal/storage/schema.go` | `var migrations = []Migration{...}` 是全部迁移；`ApplyMigrations` 是执行入口；`ApplySchemaRepairs` 是补救入口 |
| `internal/storage/db_test.go` | 既有测试示范了怎么遍历 `migrations` 做全局断言 |

## 步骤

### 1. 在切片末尾追加

`internal/storage/schema.go` 的 `migrations` 切片，版本号接着当前最大值往下排：

```go
{
	Version: 41,
	SQL: `
ALTER TABLE my_table ADD COLUMN my_column TEXT NOT NULL DEFAULT '';
`,
},
```

要点：

- `SQL` 用反引号原始字符串，一条迁移里可以写多条语句
- 加列优先 `ALTER TABLE ... ADD COLUMN ... NOT NULL DEFAULT ...` —— **SQLite 不允许加无默认值的 NOT NULL 列**
- 建表用 `CREATE TABLE IF NOT EXISTS`
- 每条迁移在**独立事务**里执行，失败会回滚并中止整个启动流程

### 2. 补 CRUD

在 `internal/storage/` 下对应的文件里加读写函数。跟着邻近代码的写法走。

### 3. 跑测试

见下方验证段。

## 硬约束：只能追加，绝不能改历史版本

`ApplyMigrations` 的逻辑是：

```go
if m.Version <= current {
	continue
}
```

`current` 取自 `SELECT MAX(version) FROM schema_version`。也就是说 —— **已经升级过的库，永远不会重跑旧版本**。

你在 `Version: 12` 里改一个字，对自己已有的 `data/emo.db` 毫无影响；但一个全新的库会执行修改后的版本。结果是两台机器上的同名版本对应着不同的表结构，而 `schema_version` 里的数字完全一样，无从分辨。

### 已发布的迁移写错了怎么办

不要回去改它。两条路：

1. **加一条新迁移**修正它 —— 首选
2. 改 `ApplySchemaRepairs`（`internal/storage/schema.go`）—— 它在所有迁移之后无条件运行，内部是一组 `ensureXxxSchema` 幂等函数，专门用来把结构补齐到期望状态。适合处理"某些库有、某些库没有"的历史遗留分裂

## 验证

```bash
go test ./internal/storage/
```

`internal/storage/db_test.go` 里的 `TestMigrationsDoNotDropPersonasTable` 示范了跨版本全局断言的写法 —— 遍历 `migrations` 检查所有迁移都满足某条规则。新增这类约束时照它写。

确认真实库升上去了（**必须 `mode=ro`** —— 应用可能正在运行，SQLite WAL 是单写者）：

```bash
python -c "import sqlite3; con=sqlite3.connect('file:data/emo.db?mode=ro', uri=True); print(list(con.execute('SELECT MAX(version) FROM schema_version')))"
```

查新结构是否符合预期：

```bash
python -c "import sqlite3; con=sqlite3.connect('file:data/emo.db?mode=ro', uri=True); [print(r) for r in con.execute('PRAGMA table_info(my_table)')]"
```
