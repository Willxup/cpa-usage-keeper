# UI 优化计划与验收目标

## 1. 文档状态

- 状态：实施合同
- 适用范围：CPA Usage Keeper Web UI
- 基线：当前工作树中的共享 `AppShell`、Ant Design 组件体系和语义化主题 Token
- 目标读者：设计、前端实现、代码审查和发布验收人员
- 最终验收记录：实施完成后新建 `docs/UI_OPTIMIZATION_ACCEPTANCE_REPORT.md`

本文档同时定义实施顺序和验收合同。实施过程允许调整具体 CSS 数值，但不得在没有更新本文档及说明理由的情况下放宽标记为“必须”的验收目标。

## 2. 产品定位与设计目标

CPA Usage Keeper 是面向 CPA 使用者和运维人员的用量、成本、请求健康及凭证管理控制台。页面的单一核心任务是：让用户快速发现请求可靠性、成本或额度异常，并能继续定位到请求或凭证。

本轮优化目标：

1. 建立清晰的核心指标和次级指标层级。
2. 提高 Request Events 宽表的扫描、筛选和定位效率。
3. 减少嵌套边框和无意义容器造成的视觉噪声。
4. 在移动端、常见桌面和超宽屏上保持稳定、可预测的布局。
5. 保持 Authenticated、Viewer、Guest 和 Embed 场景的一致性。
6. 将 Request Health Timeline 强化为与 Keeper 业务直接相关的识别元素。
7. 将可访问性、响应式和回归验证变成可重复的验收流程。

## 3. 约束与非目标

### 3.1 必须遵守的约束

- 保留 React、Ant Design 和现有图表技术栈。
- 保留共享 `AppShell`、232px 桌面侧栏和语义化主题 Token。
- 保留 `--page-max-width: 1440px` 和完整 Shell 约 1672px 的有界画布策略。
- 不为 Sparkline 或视觉效果新增后端接口或轮询请求。
- 不破坏 API Key Viewer、Login 和 CPAMC Embed。
- 不把 UI 优化与无关后端修改混入同一提交。
- 优先扩展现有组件和 Token，不增加新的运行时依赖。

### 3.2 非目标

- 不重写整个前端。
- 不通过全局拉伸填满所有超宽屏。
- 不恢复旧版的大量渐变、玻璃效果和胶囊按钮。
- 不为了减少边框而取消必要的控件边界、焦点状态或表格结构。
- 不在本轮重构业务 API、统计口径或数据库模型。
- 不以主观评分作为验收依据。

## 4. 设计基线

### 4.1 色彩角色

继续使用现有语义角色：

| 角色 | Light 基线 | 用途 |
|---|---:|---|
| Canvas | `#e9eef5` | 应用画布 |
| Content | `#f4f7fb` | 主内容背景 |
| Surface | `#ffffff` | 一级内容表面 |
| Primary text | `#192230` | 标题和主要数据 |
| Interactive | `#2563eb` | 主要操作和焦点 |
| Success | `#15803d` | 成功状态 |
| Warning | `#b45309` | 警告和不完整状态 |
| Danger | `#be123c` | 失败和危险操作 |

结构色、交互色、状态色和数据系列色不得相互代替。新增加的结构和状态颜色必须进入主题 Token 或由既有 Token 推导。

### 4.2 字体角色

- 界面正文：现有 Noto Sans/Inter 系统字体栈。
- 指标、数字和表格数值：现有 data font stack，并启用 tabular numbers。
- 页面标题：20px/28px。
- 区块标题：18px/26px。
- 正文：14px/22px。
- 辅助文字：不得小于 12px/18px，第三方图表自动生成的极短轴标签除外。

### 4.3 容器规则

- 页面内容之间优先使用间距和背景区分。
- 一级卡片可以使用结构边框。
- 一级卡片内部的图表或指标区域优先使用背景差异，不重复添加同等强度边框。
- 任意主内容区域不得同时出现超过两级清晰可见的带边框容器。
- 控件、表格分隔和焦点边界不计入上述两级容器限制。

### 4.4 Keeper 识别元素

Request Health Timeline 是本轮唯一需要强化的视觉签名。它必须表达真实的时间、请求成功和失败状态，不增加与数据无关的装饰动画。其他页面保持安静、明确和数据优先。

## 5. 实施流程

采用以下顺序：

```text
当前基线 -> 可丢弃探针 -> 决策门 -> 基础收敛 -> 页面优化 -> 回归治理 -> 最终验收
```

在阶段 0 和阶段 1 完成前，不进入大范围组件改造。

## 6. 阶段 0：建立可复现基线

### 6.1 工作项

- 固定测试数据，覆盖正常、空、Loading、错误、价格缺失和超长字段。
- 记录当前页面级横向溢出、文本换行、控件重叠和焦点问题。
- 捕获第 12 节定义的页面、视口、主题和语言矩阵。
- 记录当前 `web/dist` 构建大小。
- 记录 `make verify` 的当前结果；如果失败，区分既有失败和本轮新增失败。

### 6.2 阶段出口

- 基线截图具有相同数据和相同浏览器缩放设置。
- 每个已知问题都有页面、状态和复现步骤。
- 不以包含 API 500、调试横幅或测试通知的截图作为正式视觉基线。

## 7. 阶段 1：可丢弃探针与决策门

探针只用于验证布局和框架行为，不直接成为最终实现。

### 7.1 KPI 层级探针

比较：

- A：现有六项等权。
- B：Request Health 和 Total Cost 为主，Tokens、RPM、TPM、Cache Rate 为次。
- C：Request Health Timeline 为主，Total Cost 为辅，其余指标紧凑排列。

默认进入详细实现的方案为 B，除非截图和实际任务验证表明 C 更清晰。

### 7.2 宽表探针

验证：

- 桌面端冻结左侧时间列和右侧结果列。
- 动态隐藏被冻结列时是否留下空白或遮挡。
- 单行省略、Tooltip 和固定列组合是否能在 Light/Dark 下正常工作。
- 1280px 和 200% 缩放下是否仍可使用。

### 7.3 边框密度探针

比较：

- 保留全部现有边框。
- 保留一级卡片边框，移除内部图表面板边框。
- 全部改用无边框背景块。

默认采用第二种。

### 7.4 决策门

进入正式实现前，必须记录：

- 最终 KPI 方案及选择原因。
- Request Events 实际冻结列。
- Analysis 使用的容器层级方案。
- 被放弃探针的原因。

## 8. 阶段 2：基础和 Shell 收敛

主要文件：

- `web/src/styles/themes.scss`
- `web/src/styles/variables.scss`
- `web/src/components/layout/AppShell.module.scss`
- `web/src/components/layout/PageLayout.module.scss`
- `web/src/theme/AntdProvider.tsx`

工作项：

- 收敛结构边框、内部边框和控件边框角色。
- 统一页面、区块、卡片、表格和控件间距。
- 清理新增的结构色和状态色魔法值。
- 保留 `100svh` Flex Shell，不增加重复的 `calc(100vh - header)`。
- 保持 Shell 在超宽屏居中。
- 只为 Request Events 等经验证需要更宽空间的页面提供显式 breakout。
- 确认移动端侧栏切换为 Drawer，桌面端侧栏保持 sticky。

## 9. 阶段 3：Request Events 宽表优化

主要文件：

- `web/src/components/usage/RequestEventsDetailsCard.tsx`
- `web/src/components/usage/RequestEventsDetailsCard.module.scss`

工作项：

- 移除模型、来源和 API Key 的 `word-break: break-all`。
- 长字段使用单行省略，Tooltip 同时支持 hover、focus 和 click。
- 时间、结果、Token、费用和延迟列禁止换行。
- 数值列右对齐并使用 tabular numbers。
- 为高频字段定义稳定列宽。
- 根据探针结果在桌面端冻结时间列，并决定是否冻结结果列。
- 保留列选择、筛选、导出、分页和请求日志功能。
- 保持表格内部滚动，不产生页面级横向滚动。
- 空状态不显示无意义的横向滚动条。

## 10. 阶段 4：Overview 指标层级

主要文件：

- `web/src/components/usage/StatCards.tsx`
- `web/src/components/usage/UsageOverview.module.scss`
- `web/src/components/usage/ServiceHealthCard.tsx`

目标布局：

- `>= 1280px`：Request Health 和 Total Cost 各占主指标行的一半；Tokens、RPM、TPM、Cache Rate 在次级指标行四等分。
- `769px–1279px`：两列布局；主指标保持在次级指标之前。
- `<= 768px`：单列布局。

工作项：

- 将当前 Total Requests 卡升级为 Request Health，集中呈现总请求、失败量和成功率。
- 将 Total Cost 提升为第二主指标。
- 未配置价格时明确显示“成本不可用”，不得把 `$0.0000` 表达成真实成本。
- Tokens、RPM、TPM 和 Cache Rate 使用更紧凑的次级卡片。
- 日均值只作为辅助上下文。
- 仅主指标允许使用趋势图；数据少于三个有效点时不绘制。
- 趋势图只复用当前接口已有 series，不新增请求。
- Request Health Timeline 保留真实时间和成功/失败语义，并作为 Overview 的业务识别元素。

## 11. 阶段 5：Analysis、Credentials 和 Settings 收口

### 11.1 Analysis

主要文件：

- `web/src/components/usage/analysis/AnalysisPanel.tsx`
- `web/src/components/usage/analysis/AnalysisPanel.module.scss`

工作项：

- 保留一级分析卡边框，移除不必要的内部图表边框。
- 主趋势图有效绘图区高度不得低于 280px。
- 桌面端小图有效绘图区高度不得低于 200px，移动端不得低于 180px。
- `>= 1280px` 的并列洞察使用两列；较窄布局回落为单列。
- 保留图表的可访问数据表、原始 Tooltip 数值和触控行为。
- 验证容器宽度变化后图表重新计算尺寸。

### 11.2 Credentials

工作项：

- 保持账号和额度窗口的父子层级。
- 统一 Auth File 和 AI Provider 的标题、过滤、表格和分页规则。
- 长邮箱、账号和 Provider 名使用单行省略及可访问 Tooltip。
- 凭证状态、额度和高频操作保持稳定位置。
- 移动端允许内部表格滚动，不压缩到不可读。

### 11.3 Settings

工作项：

- 保持 Tabs 信息架构。
- 统一表单宽度、帮助文字、保存反馈和危险操作确认。
- 删除与侧栏重复的偏好或产品级操作。

## 12. 验收矩阵

### 12.1 必测页面

| 页面 | 正常 | 空数据 | Loading | 错误 | 长文本 |
|---|---:|---:|---:|---:|---:|
| Login | 必须 | 不适用 | 必须 | 必须 | 必须 |
| Overview | 必须 | 必须 | 必须 | 必须 | 必须 |
| Analysis | 必须 | 必须 | 必须 | 必须 | 必须 |
| Request Events | 必须 | 必须 | 必须 | 必须 | 必须 |
| Auth Files | 必须 | 必须 | 必须 | 必须 | 必须 |
| AI Provider | 必须 | 必须 | 必须 | 必须 | 必须 |
| Settings | 必须 | 必须 | 必须 | 必须 | 必须 |
| API Key Viewer | 必须 | 必须 | 必须 | 必须 | 必须 |
| CPAMC Embed | 必须 | 必须 | 必须 | 必须 | 必须 |

### 12.2 必测视口

| 标识 | 视口 | 用途 |
|---|---:|---|
| V1 | 390×844 | 手机 |
| V2 | 768×1024 | 移动断点边界 |
| V3 | 1024×768 | 小型桌面 |
| V4 | 1280×720 | 常见桌面 |
| V5 | 1440×900 | 标准桌面 |
| V6 | 1920×1080 | 宽屏 |
| V7 | 2560×1440 | 超宽屏 |

V1、V4、V6、V7 必须覆盖中文/英文及 Light/Dark；其余视口至少覆盖英文 Light，并对发现问题的组合补测。

浏览器缩放必须额外验证 100% 和 200%。

## 13. 可判定的验收目标

以下目标全部为“必须”，除非明确标记为决策门。

### 13.1 Shell 与响应式

| ID | 验收目标 | 判定方法 |
|---|---|---|
| UI-SHELL-01 | V1–V7 均无页面级非预期横向滚动 | `document.documentElement.scrollWidth <= document.documentElement.clientWidth + 1` |
| UI-SHELL-02 | `<= 768px` 隐藏桌面侧栏并通过 Drawer 导航；`>= 769px` 显示桌面侧栏 | 自动化 DOM/CSS 断言和截图 |
| UI-SHELL-03 | 视口宽度大于 1672px 时 Authenticated Shell 居中，左右外边距差不超过 1px | `getBoundingClientRect()` |
| UI-SHELL-04 | 主内容最大宽度保持 1440px，完整 Shell 最大宽度保持 1672px | Token 测试和 DOM 测量 |
| UI-SHELL-05 | 内容不足一屏时 Shell 仍覆盖完整可视高度，侧栏工具区位于视口底部 | V4、V6 截图和 DOM 测量 |
| UI-SHELL-06 | Viewer、Guest 和 Embed 不出现 Authenticated 专属侧栏 | 组件测试 |

### 13.2 Overview

| ID | 验收目标 | 判定方法 |
|---|---|---|
| UI-OV-01 | `>= 1280px` 时 Request Health 与 Total Cost 位于主指标行，四个次级指标位于其后 | DOM 顺序和布局测试 |
| UI-OV-02 | 主指标单卡宽度至少为次级指标单卡宽度的 1.8 倍 | V4–V7 DOM 测量 |
| UI-OV-03 | Request Health 同时展示总请求、失败数和成功率 | 组件测试 |
| UI-OV-04 | 价格不可用时显示不可用状态，不显示具有真实成本含义的 `$0.0000` | 组件测试 |
| UI-OV-05 | 趋势图只使用已有数据；页面加载不因趋势图增加 API 请求数 | 网络记录和逻辑测试 |
| UI-OV-06 | 少于三个有效趋势点时不渲染 Sparkline | 组件测试 |
| UI-OV-07 | Request Health Timeline 的每个可交互块均可通过键盘获得说明 | 键盘检查和组件测试 |

### 13.3 Request Events

| ID | 验收目标 | 判定方法 |
|---|---|---|
| UI-EVT-01 | 模型、来源和 API Key 不使用 `word-break: break-all` | 样式治理测试 |
| UI-EVT-02 | 长模型、来源和 API Key 不增加数据行高度；长短样例行高度差不超过 1px | DOM 测量 |
| UI-EVT-03 | 被省略的完整值可通过 hover、键盘 focus 和 click 查看 | 交互测试 |
| UI-EVT-04 | 时间、结果和数值列不换行 | 样式和 DOM 测量 |
| UI-EVT-05 | 默认 7 列及全 21 列状态均只在表格内部横向滚动 | V3–V7 截图和 DOM 测量 |
| UI-EVT-06 | 冻结列开启时无覆盖、透明背景、错位或隐藏列残留空白 | Light/Dark 交互检查 |
| UI-EVT-07 | 分页和列选择不会随表格横向滚动离开可操作区域 | 交互检查 |
| UI-EVT-08 | 空状态不显示无意义横向滚动条 | 空状态截图 |

### 13.4 Analysis 与容器层级

| ID | 验收目标 | 判定方法 |
|---|---|---|
| UI-AN-01 | 任意 Analysis 主区域不超过两级清晰可见的带边框容器 | 截图审查和样式审查 |
| UI-AN-02 | 主图有效绘图区高度不少于 280px | DOM 测量 |
| UI-AN-03 | 桌面端小图有效绘图区不少于 200px，移动端不少于 180px | DOM 测量 |
| UI-AN-04 | 中英文图例不覆盖绘图区，超长标签采用省略并可查看完整值 | V1、V4、V6 截图和交互检查 |
| UI-AN-05 | 图表保留屏幕阅读器可用的数据表替代内容 | DOM/组件测试 |
| UI-AN-06 | 页面或侧栏尺寸变化后图表没有裁切或空白 | Resize 交互检查 |

### 13.5 Credentials 与 Settings

| ID | 验收目标 | 判定方法 |
|---|---|---|
| UI-CRED-01 | Auth File 和 AI Provider 使用相同的区块标题、筛选、表格和分页规则 | 组件与样式测试 |
| UI-CRED-02 | 账号和额度窗口的父子层级在桌面和移动端均可辨认 | 截图审查 |
| UI-CRED-03 | 长账号、邮箱和 Provider 名不导致数据行异常增高 | DOM 测量 |
| UI-CRED-04 | 额度状态不只依赖颜色表达 | DOM/可访问性检查 |
| UI-SET-01 | 危险操作均有确认流程，Loading 时不可重复提交 | 组件测试 |
| UI-SET-02 | 保存、成功提示和错误提示使用一致动作名称 | 文案和组件测试 |

### 13.6 文字、颜色和交互

| ID | 验收目标 | 判定方法 |
|---|---|---|
| UI-A11Y-01 | 普通文字对比度至少 4.5:1，大号文字至少 3:1 | 计算后的颜色对比度检查 |
| UI-A11Y-02 | 控件边界和焦点等非文本信息对比度至少 3:1 | 对比度检查 |
| UI-A11Y-03 | 自定义可见辅助文字不小于 12px/18px | 样式治理测试 |
| UI-A11Y-04 | 所有可操作元素具有可见 focus 状态，且不被裁切 | 全键盘操作检查 |
| UI-A11Y-05 | 状态同时使用文字、图标或数值，不只使用颜色 | DOM 审查 |
| UI-A11Y-06 | Tooltip、Popover、Modal 和 Drawer 可使用键盘打开和关闭，并恢复焦点 | 交互检查 |
| UI-A11Y-07 | `prefers-reduced-motion: reduce` 下关闭非必要动画 | 媒体查询测试 |

### 13.7 工程质量

| ID | 验收目标 | 判定方法 |
|---|---|---|
| UI-ENG-01 | 不新增 UI 运行时依赖 | `web/package.json` diff |
| UI-ENG-02 | UI 优化不新增后端 API 或轮询 | 网络记录和 API diff |
| UI-ENG-03 | `make verify` 全部通过 | 命令输出 |
| UI-ENG-04 | 优化后前端 gzip 构建体积相对基线增幅不超过 5%，超过时必须记录原因并批准 | 构建产物对比 |
| UI-ENG-05 | API Key Viewer、Login 和 CPAMC Embed 的既有测试全部通过 | 测试输出 |
| UI-ENG-06 | 所有验收目标在最终报告中有通过、失败或不适用状态及证据指针 | 验收报告审查 |

## 14. 验证命令

每个实施阶段至少运行：

```bash
npm --prefix ./web run test
npm --prefix ./web run lint
npm --prefix ./web run typecheck
npm --prefix ./web run build
```

最终验收运行仓库统一入口：

```bash
make verify
```

如果涉及 Docker 打包或静态资源嵌入，再运行：

```bash
make verify-docker
```

视觉验收使用固定浏览器、固定数据和第 12 节矩阵。暂不在本轮无条件引入新的视觉测试依赖；若手工视觉回归在实施中重复发生，再单独评估仓库级 Playwright 基线。

## 15. 验收证据格式

最终的 `docs/UI_OPTIMIZATION_ACCEPTANCE_REPORT.md` 至少包含：

1. 被验收的 commit。
2. 环境、浏览器版本、设备像素比和缩放比例。
3. 每个验收 ID 的状态：通过、失败、不适用。
4. 自动化命令及结果摘要。
5. 页面矩阵截图路径。
6. 网络请求数量对比。
7. 构建产物大小对比。
8. 所有例外、剩余风险和批准理由。

正式截图不得包含：

- 调试通知或 `visual-check` 横幅；
- 打开的临时测试弹层，除非该截图专门验收弹层；
- 后端 500 导致的非目标错误状态；
- 未脱敏的 API Key、邮箱或请求日志。

## 16. 提交计划

建议按以下顺序提交：

1. `refactor(ui): tighten design tokens and shell contracts`
2. `feat(request-events): improve wide-table scanning`
3. `feat(overview): establish metric hierarchy`
4. `refactor(analysis): reduce nested visual chrome`
5. `refactor(credentials): align credential surfaces`
6. `test(ui): add responsive and accessibility governance`
7. `docs(ui): record optimization acceptance evidence`

每个提交必须可独立构建和测试。已有后端成本计算修改必须与上述 UI 提交分离。

## 17. 风险与回退策略

| 风险 | 处理方式 |
|---|---|
| 固定列与动态列选择冲突 | 探针阶段验证；不通过则只冻结时间列或取消冻结 |
| KPI 层级在窄桌面过于拥挤 | 在 1280px 决策门截图中验证；必要时提前回落到两列 |
| Sparkline 增加噪声或成本 | 仅主指标、复用现有数据；不满足条件则不渲染 |
| 图表 resize 后裁切 | 增加 Resize 回归检查，保留单列回退 |
| Dark 模式边框过重 | 使用独立 dark Token，不用降低文字对比度换取层次 |
| Embed 继承主应用样式 | 通过 variant 和现有 Embed 测试隔离 |
| 视觉调整扩大当前脏工作树 | 分阶段提交；不覆盖、不重置现有修改 |

任一阶段出现无法在本阶段解决的回归时，回退该阶段的布局修改，不回退此前已验收阶段。

## 18. 完成定义

本轮 UI 优化只有在以下条件全部满足时才完成：

- 阶段 0–5 的工作项完成。
- 第 13 节所有“必须”验收目标通过或具有明确批准的例外。
- V1、V4、V6、V7 的中英文和 Light/Dark 正式截图完成。
- Request Events 长字段不再破坏行扫描。
- Overview 建立 Request Health/Total Cost 主层级。
- Analysis 不再出现超过两级的清晰嵌套边框。
- Viewer、Login、Embed 无回归。
- `make verify` 通过。
- 最终验收报告包含证据和 commit 指针。
- 代码、测试、本文档和验收报告保持一致。
