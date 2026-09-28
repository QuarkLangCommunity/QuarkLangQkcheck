# QuarkLangQkcheck

QuarkLang 静态检查（lint）——工具链成员。基于语言自身的词法/语法/类型检查（`internal/lang`），在**能编译**的前提下再做额外检查。

## 用法

```sh
go build -o qkcheck .        # 需要同目录下的 QuarkLang 主仓（go.mod 里 replace 指向 ../QuarkLang）
qkcheck [options] files...
```

| 选项 | 说明 |
|---|---|
| `-json` | 每行一条 JSON 警告（`{"file","line","col","kind","msg"}`），便于编辑器/CI 解析 |
| `-no-unused` | 关闭「未使用局部变量」 |
| `-no-unreachable` | 关闭「不可达代码」 |
| `-shadow` | 开启「变量遮蔽」（默认关闭：语言本身禁止同一函数内重复声明，遮蔽会被编译期直接拒绝） |

退出码：有警告 `1`；全部干净 `0`；某文件无法编译 `2`（该文件的编译错误打到 stderr，其余文件继续检查）。

## 检查项

- **未使用局部变量**：声明后从未被读取（形参不计，`_` 前缀跳过）
- **不可达代码**：同一块中 `return`/`break`/`log` 之后仍有语句

## 实测

```
$ qkcheck lint2.qk
lint2.qk:4:9: 警告: [unused] 局部变量 "neverUsed" 声明后未被使用
qkcheck: 1 个警告（1 个文件）

$ qkcheck clean.qk
qkcheck: 0 个警告（1 个文件）

$ qkcheck $(find ../QuarkLangLibs-Style -name '*.qk' -not -path '*/.git/*')
.../style.qk:1524:35: 警告: [unused] 局部变量 "style" 声明后未被使用     # 真实死代码：tickNode 里取了 getStyle() 却没用
qkcheck: 1 个警告（5 个文件）
```

各库抽查：Cleg 11 文件 0 警告、Regex 5 文件 0 警告、Json/Actions 各 1 文件 0 警告。
