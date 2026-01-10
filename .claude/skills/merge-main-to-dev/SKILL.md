---
name: merge-main-to-dev
description: 从 main 分支合并代码到开发分支（developer），由 Claude 智能处理合并冲突，保护当前分支的自定义修改。用于：(1) 同步 main 分支的 bug 修复和新功能，(2) 智能分析冲突并保留开发分支的关键逻辑，(3) 特别保护 Claude/OpenAI 数据转换逻辑。当用户说"合并 main"、"同步主分支"、"更新开发分支"时触发。
---

# 从 Main 合并到开发分支

## 核心原则

1. **保护优先**: 冲突时优先保留当前分支（developer）的代码逻辑
2. **智能分析**: 分析每个冲突文件，理解双方改动意图后决策
3. **关键文件保护**: Claude/OpenAI 转换逻辑必须保留当前分支版本

## 关键保护文件

以下文件包含自定义的数据转换逻辑，冲突时**必须保留当前分支版本**：

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

### 步骤 3: 处理冲突

合并后检查冲突文件：

```bash
git diff --name-only --diff-filter=U
```

**冲突处理策略：**

1. **关键保护文件冲突**: 读取冲突文件，保留当前分支（`<<<<<<< HEAD` 到 `=======` 之间）的代码
2. **非关键文件冲突**: 分析双方改动：
   - 如果 main 是 bug 修复 → 接受 main 的改动
   - 如果 main 是新功能且不影响现有逻辑 → 合并两者
   - 如果有逻辑冲突 → 保留当前分支

### 步骤 4: 验证并提交

```bash
# 编译验证
go build -o new-api main.go

# 提交合并
git add .
git commit -m "Merge main into developer: sync bug fixes and new features"

# 恢复暂存
git stash pop
```

## 冲突解决示例

冲突标记格式：
```
<<<<<<< HEAD
// 当前分支（developer）的代码 - 优先保留
=======
// main 分支的代码
>>>>>>> main
```

对于关键文件，删除 main 的部分，保留 HEAD 的代码：
```go
// 保留这部分代码
```

## 回滚

```bash
git merge --abort  # 合并过程中取消
git reset --hard HEAD~1  # 合并后回滚
```

## 详细文件说明

参考 `references/protected_files.md` 了解每个保护文件的具体功能。
