---
name: merge-main-to-dev
description: 从 main 分支合并代码到开发分支（developer），由 Claude 深度分析每个冲突文件的改动内容后智能合并。用于：(1) 同步 main 分支的 bug 修复和新功能，(2) 深度分析冲突内容决定最佳合并策略，(3) 不自动提交，由用户确认后手动提交。当用户说"合并 main"、"同步主分支"、"更新开发分支"时触发。
---

# 从 Main 合并到开发分支

## 核心原则

1. **深度分析优先**: 对每个冲突文件，必须先分析双方的具体改动内容、改动目的、commit 历史
2. **智能决策**: 根据分析结果决定保留哪个版本，或者合并两者的改动
3. **不自动提交**: 合并完成后只验证编译，由用户确认后手动提交

## 关键文件清单

以下文件包含自定义的数据转换逻辑，冲突时需要**特别仔细分析**：

```
# Claude 相关
relay/channel/claude/adaptor.go
relay/channel/claude/dto.go
relay/channel/claude/relay-claude.go
relay/channel/claude/constants.go
relay/claude_handler.go

# OpenAI 相关
relay/channel/openai/adaptor.go
relay/channel/openai/relay-openai.go
relay/channel/openai/helper.go
relay/channel/openai/relay_responses.go

# 核心转换服务
service/convert.go

# 其他适配器
relay/channel/gemini/relay-gemini.go
relay/channel/aws/adaptor.go
relay/compatible_handler.go
```

## 合并工作流

### 步骤 1: 准备工作

```bash
# 确认当前分支和状态
git branch --show-current
git status

# 如有未提交更改，先暂存
git stash
```

### 步骤 2: 更新并合并

```bash
git fetch origin
git checkout main && git pull origin main
git checkout developer
git merge main --no-commit
```

### 步骤 3: 深度分析冲突

合并后检查冲突文件：

```bash
git diff --name-only --diff-filter=U
```

**对每个冲突文件执行以下分析流程：**

#### 3.1 查看双方的具体改动

```bash
# 查看 main 分支相对于 developer 分支的改动
git diff developer...main -- <file_path>

# 查看改动该文件的 commit 历史
git log --oneline main -- <file_path> | head -10
git log --oneline developer -- <file_path> | head -10
```

#### 3.2 分析改动内容

对于每个改动，需要回答以下问题：

1. **main 分支改了什么？** 具体的代码变更是什么
2. **改动的目的是什么？** 是 bug 修复、新功能、重构还是优化
3. **developer 分支有没有相关改动？** 是否已经有类似的修复或不同的实现
4. **两个改动是否冲突？** 逻辑上是否互斥

#### 3.3 决策策略

根据分析结果选择合并策略：

| 场景 | 策略 |
|------|------|
| main 是 bug 修复，developer 没有相关改动 | 接受 main 的改动 |
| main 是 bug 修复，developer 有不同的修复方式 | 分析哪个更好，或合并两者 |
| main 是新功能，不影响 developer 的逻辑 | 接受 main 的改动 |
| main 是新功能，与 developer 的改动冲突 | 保留 developer，记录 main 的功能待后续处理 |
| main 是重构/优化，developer 有自定义逻辑 | 保留 developer 的逻辑，考虑是否采用 main 的优化方式 |
| developer 的改动是有意覆盖 main 的旧逻辑 | 保留 developer |

#### 3.4 解决冲突

根据决策结果解决冲突：

```bash
# 保留当前分支（developer）版本
git checkout --ours <file_path>

# 保留 main 分支版本
git checkout --theirs <file_path>

# 手动编辑合并两者
# 读取文件，手动编辑冲突标记
```

### 步骤 4: 验证编译

```bash
# 编译验证
go build -o new-api main.go

# 检查编译结果
ls -la new-api
```

### 步骤 5: 等待用户确认

**不要自动提交！** 向用户报告：

1. 合并了哪些文件
2. 每个冲突文件的处理决策和原因
3. 编译是否成功
4. 等待用户确认后手动提交

用户确认后的提交命令示例：

```bash
git add .
git commit -m "Merge main into developer: <简要说明合并内容>"
```

## 冲突标记格式

```
<<<<<<< HEAD
// 当前分支（developer）的代码
=======
// main 分支的代码
>>>>>>> main
```

## 回滚

```bash
git merge --abort  # 合并过程中取消
git reset --hard HEAD~1  # 合并后回滚
```

## 分析示例

### 示例：分析 helper.go 的冲突

```bash
# 1. 查看 main 的改动
git diff developer...main -- relay/channel/openai/helper.go

# 2. 查看相关 commit
git log --oneline main -- relay/channel/openai/helper.go | head -5
# 输出: 80638979 fix: glm 4.7 finish reason (#2545)

# 3. 查看完整 commit 信息
git show 80638979 -p

# 4. 分析结论
# - main 的改动：将 Done=true 移到处理之后，修复 GLM 4.7 的 finish reason 问题
# - developer 的改动：移除了 lastStreamData 的重复解析，因为已在 HandleStreamFormat 中处理
# - 决策：两个改动解决不同问题，需要判断 developer 的优化是否会影响 GLM 4.7 的修复
```

## 详细文件说明

参考 `references/protected_files.md` 了解每个关键文件的具体功能。
