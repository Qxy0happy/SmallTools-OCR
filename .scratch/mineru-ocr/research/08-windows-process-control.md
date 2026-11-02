# Ticket 08：Windows 进程控制实验报告

> 环境：Windows 10，Go 1.27.3，uv 0.13.6，CUDA 机器。所有脚本/二进制位于 `%TEMP%`（`D:\Cache\Temp\User\30625\mineru-exp`），未修改 MinerU 或 OCR 仓库。

## 实验 1：CREATE_NEW_PROCESS_GROUP + CTRL_BREAK_EVENT

Go 主机程序启动：

```
uv run --project D:/Projects/Python/MinerU python -c "<child.py>"
```

子进程 Python 代码（写日志、注册 `SIGINT`/`SIGBREAK` 处理器）：

```python
import os, signal, time, sys
pid = os.getpid()
log = r"<TEMP>\mineru-child.log"
def h(signum, frame):
    with open(log, "a") as f:
        f.write("received signal %d\n" % signum)
    sys.exit(0)
signal.signal(signal.SIGINT, h)
try:
    signal.signal(signal.SIGBREAK, h)
except (AttributeError, ValueError) as e:
    ...
with open(log, "w") as f:
    f.write("started pid=%d ppid=%d\n" % (pid, os.getppid()))
while True:
    time.sleep(0.5)
```

Go 侧：

```go
cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000200} // CREATE_NEW_PROCESS_GROUP
cmd.Start()
windows.GenerateConsoleCtrlEvent(CTRL_BREAK_EVENT /*=1*/, uint32(cmd.Process.Pid))
```

**输出**：

```
starting uv...
uv pid=22988 started in 8.0257ms
child log after start:
started pid=25204 ppid=8316
calling GenerateConsoleCtrlEvent(CTRL_BREAK, uv pid)...
GenerateConsoleCtrlEvent err=<nil>
uv exited in 3.016136s: exit status 0xc000013a
child log final:
started pid=25204 ppid=8316
received signal 21
tasklist python:
信息: 没有运行的任务匹配指定标准。
tasklist uv:
信息: 没有运行的任务匹配指定标准。
```

结论：(a) uv 退出；(b) Python 子进程收到信号 `21`（Windows `SIGBREAK`）并优雅退出；(c) 无残留 `python.exe`/`uv.exe`。

## 实验 2：taskkill /T /F /PID <uv pid>

同一程序，模式切换为 `taskkill`。

**输出**（关键段）：

```
running taskkill /T /F ...
taskkill: 成功: 已终止 PID 32984 ...; 4540 ...; 18964 ...; 29804 ...
uv exited: exit status 1
tasklist python:
"python.exe","21568",...
"python.exe","32512",...
tasklist uv:
"uv.exe","35396",...
"uv.exe","37488",...
```

经 `Get-CimInstance` 核对，残留的 `python.exe (21568/32512)` 与 `uv.exe (35396/37488)` 命令行均为 `mineru-kit api-server --port 18081/18082`，**属于实验前已存在的服务，不是本实验子进程**。本实验启动的进程树（PID 29804→18964→4540→32984）已被 `/T` 全部终止，子进程未收到信号（日志无 `received signal`），属于强杀。

结论：`taskkill /T /F` 能杀死整棵进程树，但不优雅（子进程无信号处理机会）；残留检查必须按命令行/端口过滤，避免误判其他服务。

## 实验 3：真实 api-server

启动命令：

```
uv run --project D:/Projects/Python/MinerU mineru-kit api-server --port 18099 --tier flash --upload-dir <TEMP>
```

健康检查端点：`GET http://127.0.0.1:18099/v1/health`。

### 3.1 CTRL_BREAK

**输出**（关键段）：

```
uv pid=16444 started
...
Application startup complete.
Uvicorn running on http://127.0.0.1:18099
...
health 200 at 2.0342165s
sending CTRL_BREAK...
GenerateConsoleCtrlEvent err=<nil>
process exited in 5.4396ms: exit status 0xc000013a
...
INFO:     Shutting down
INFO:     Waiting for application shutdown.
port released in 502.019ms
INFO:     Application shutdown complete.
```

- 健康 200 耗时：≈2.03 s
- CTRL_BREAK 后进程退出：≈5 ms（信号送达）；端口释放/进程树消失：≈0.50 s（优雅 shutdown）

### 3.2 taskkill /T /F

**输出**（关键段）：

```
health 200 at 2.0332521s
running taskkill /T /F ...
taskkill in 279.7025ms: 成功: 已终止 PID 30256/26372/25044/22728/9228 ...
process exited: exit status 1
port released in 0s
```

- taskkill 耗时：≈0.28 s；端口立即释放；无优雅 shutdown 日志。

## 实验 4：宿主被强杀

启动 Go 主机 `hosthold.exe`（PID 24040），其内 `CREATE_NEW_PROCESS_GROUP` 启动 `uv`（PID 9868）→ python（PID 20984，`time.sleep(3600)`）。

执行 `taskkill /F /PID 24040`。

**结果**：

```
成功: 已终止 PID 为 24040 的进程。
host process check: 没有运行的任务匹配指定标准。
uv.exe PID 9868: 仍在运行
python.exe PID 20984: 仍在运行
```

结论：宿主被强杀后，`uv` 与 `python` **全部存活**（孤儿进程），下次启动必须通过端口/进程命令行/lock 文件识别并清理残留。

## 推荐关停序列

1. **优雅**：对 uv 进程组发送 `GenerateConsoleCtrlEvent(CTRL_BREAK_EVENT, uv_pid)`；等待进程退出，阈值 **5 s**（实测 0.5 s 内完成）。
2. **超时**：若 5 s 内未退出，记录日志并进入强杀。
3. **强杀**：`taskkill /T /F /PID <uv_pid>`（实测 ≈0.28 s），随后轮询端口/进程树直到消失（最多再等 5 s）。
4. **宿主被强杀的恢复**：启动前检查：
   - 目标端口是否被占用（`/v1/health`）；
   - `.mineru-service.lock` 是否被占用；
   - 命令行含 `mineru-kit api-server --port <port>` 的 `uv.exe`/`python.exe` 残留；
   对识别到的残留执行 `taskkill /T /F` 后再启动。

## 证据索引

- `%TEMP%\mineru-exp\out_ctrlbreak.txt`
- `%TEMP%\mineru-exp\out_taskkill.txt`
- `%TEMP%\mineru-exp\out_api_ctrlbreak.txt`
- `%TEMP%\mineru-exp\out_api_taskkill.txt`
- hosthold 实验：`tasklist` 输出见上文。

所有实验进程已清理；本实验启动的 uv/python 无残留。`18081/18082` 端口上的 mineru api-server 为实验前已存在的服务，未在本实验中启动或终止。
