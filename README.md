# CloudflareSpeedTest（API 改造版）

基于 [XIU2/CloudflareSpeedTest](https://github.com/XIU2/CloudflareSpeedTest) 改造，保留原有测速能力，新增 **HTTP API 服务**，可供其他程序调用测速、查询结果、管理 IP 段与数据源。

## 技术栈

Go 1.18，标准库为主；第三方依赖仅 `pb/v3`（进度条）、`fatih/color`（彩色输出）、`ewma`（测速平滑）。API 用 `net/http` 手写，无 Web 框架、无数据库。

## 目录结构

```text
main.go          入口与命令行参数
api/             HTTP 接口（server/saved/ranges）
task/            测速核心、IP 源拉取、IP 段解析
utils/           进度、CSV、颜色工具
ip.txt/ipv6.txt  默认 IP 段数据
saved_ips.txt    收藏的优质 IP（接口自动创建）
```

## 运行

不需要编译 exe，直接源码启动。需 Go 1.18+。

```bash
cd CloudflareSpeedTest
go run . -server -addr 0.0.0.0:8080
```

- `-addr` 默认 `127.0.0.1:8080`，允许其他机器访问需改为 `0.0.0.0:8080` 并放行防火墙
- 不传 `-server` 则走原有命令行测速模式

常用测速参数（API 模式同样生效）：`-n` 线程、`-t` 延迟次数、`-dn` 下载测速数量、`-dt` 下载时长、`-dd` 禁用下载、`-tl/-tll` 延迟上下限、`-tlr` 丢包上限、`-sl` 速度下限、`-url`/`-httping`/`-cfcolo` 等。完整列表用 `go run . -h` 查看。

**新增参数：**

| 参数 | 说明 |
|------|------|
| `-server` | 启动 HTTP API 服务 |
| `-addr` | 监听地址，默认 `127.0.0.1:8080` |
| `-fetch` | 启动时从数据源拉取最新 IP 段（追加去重） |
| `-fetch-only` | 只拉取 IP 段后退出，不测速 |

## HTTP API

所有响应均为 JSON，带 CORS 头。

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/test` | 触发测速，Body 可选 `{"ip":"1.1.1.1,104.16.0.0/24"}` 指定 IP，不传则测本地 ip.txt |
| GET | `/api/status` | 测速状态、进度、上次错误 |
| GET | `/api/result` | 最新完整结果 |
| GET | `/api/best?n=10` | 前 N 个优质 IP 字符串数组，默认 10 |
| GET | `/api/stats` | 统计汇总：延迟/速度极值均值、丢包、地区分布 |
| GET | `/api/ips` | 当前 ip.txt 的 IP 段列表 |
| GET | `/api/ranges` | IP 段增删查：`?file=ip.txt` 指定文件 |
| POST | `/api/ranges` | Body `{"file":"ip.txt","ranges":"1.1.1.0/24,2.2.2.0/24"}` 追加（去重） |
| DELETE | `/api/ranges` | Body 同上，删除指定段 |
| GET | `/api/saved` | 已收藏的优质 IP 列表 |
| POST | `/api/saved` | Body `{"ip":"1.1.1.1"}` 收藏（去重） |
| DELETE | `/api/saved` | Body `{"ip":"1.1.1.1"}` 取消收藏 |
| GET | `/api/sources` | 当前 IP 数据源配置 |
| POST | `/api/fetch` | 触发所有数据源拉取（测速中拒绝） |

### 典型调用流程

```bash
# 1. 触发测速（可指定 IP）
curl -X POST http://127.0.0.1:8080/api/test \
  -H "Content-Type: application/json" \
  -d '{"ip":"104.16.0.3,104.16.1.0/24"}'

# 2. 轮询进度，等 running=false
curl http://127.0.0.1:8080/api/status

# 3. 取优质 IP
curl 'http://127.0.0.1:8080/api/best?n=10'
```

`/api/status` 示例：

```json
{
  "running": true,
  "last_run": "2026-11-18T01:21:41+08:00",
  "last_error": "",
  "count": 0,
  "progress": {"stage": "延迟测速", "done": 336, "total": 5956, "percent": 5.64}
}
```

`stage` 取值为 `延迟测速`/`下载测速`/`完成`；失败时 `last_error` 给出原因，`running` 变 `false`。

`/api/result` 单条结果：

```json
{"ip":"104.16.0.3","sent":4,"received":4,"loss_rate":0,"delay_ms":213.17,"download_mbps":5.42,"colo":"LAX"}
```

结果按质量排序：有下载速度时按速度降序，速度相同按延迟升序。

## IP 源扩展

数据源定义在 `task/fetch.go` 的 `IPSources`，新增源只需追加一项：

```go
{Name: "AWS", URL: "https://example.com/aws-ips.txt", File: "aws.txt"},
```

- 拉取结果为**追加去重**，不覆盖手动加入的段
- 单个源失败不影响其他源，失败时保留原文件
- `File` 用独立文件名可避免与 CF 官方段混在一起

`/api/ranges` 的 `file` 参数仅允许当前目录下的 `.txt` 文件，防止路径遍历。

## 数据持久化

- 测速结果为内存覆盖式，每次测速替换上次，不保留历史；需要留存请调用 `/api/saved` 收藏
- `ip.txt`/`ipv6.txt` 为数据源文件，`saved_ips.txt` 为收藏文件，互不影响

## 说明

本项目由上游改造而来，保留了原有测速逻辑。Cloudflare CDN 禁止代理方式使用，代理套 CDN 请自行承担风险。软件仅适用于网站，不支持 Cloudflare WARP（UDP）优选 IP。

原项目文档见 [上游 README](https://github.com/XIU2/CloudflareSpeedTest)。
