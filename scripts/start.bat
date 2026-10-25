@echo off
chcp 65001 >nul
setlocal
rem 一键启动前后端：分别在两个新窗口中运行，关闭窗口即停止对应服务
rem   后端: backend\ (Go, 默认 :8080)
rem   前端: web\     (Vite, 默认 :5173，/api 与 /files 代理到后端)

for %%I in ("%~dp0..") do set "ROOT=%%~fI\"

where go >nul 2>nul
if errorlevel 1 (
  echo [错误] 未找到 go，请先安装 Go
  pause
  exit /b 1
)

rem 前端包管理器：项目用 bun.lock，优先 bun，没有则退回 npm
set "PM="
where bun >nul 2>nul && set "PM=bun"
if not defined PM (
  where npm >nul 2>nul && set "PM=npm"
)
if not defined PM (
  echo [错误] 未找到 bun 或 npm，请先安装其中之一
  pause
  exit /b 1
)

rem 配置文件：优先用本机的 config.local.yaml（已被 gitignore），没有就用默认配置
set "CONFIG=configs/config.yaml"
if exist "%ROOT%backend\configs\config.local.yaml" set "CONFIG=configs/config.local.yaml"

rem 首次运行自动安装前端依赖
set "WEB_CMD=%PM% run dev"
if not exist "%ROOT%web\node_modules" set "WEB_CMD=%PM% install && %PM% run dev"

rem 画布 Agent 是独立的服务（agent\，要求 Node >= 22.19，并在 agent\ 里 npm ci 装好依赖）；没装依赖就跳过，后端的 Agent 保持关闭
set "AGENT_SET="
where node >nul 2>nul
if not errorlevel 1 if exist "%ROOT%agent\node_modules" (
  set "AGENT_TOKEN=video-canvas-local-agent-token-0123456789abcdef"
  echo ==^> 启动画布 Agent 服务 ^(127.0.0.1:8090^)
  start "video-canvas agent" /d "%ROOT%agent" cmd /k "chcp 65001 >nul & set AGENT_SERVICE_TOKEN=%AGENT_TOKEN%& set AGENT_LISTEN=127.0.0.1:8090& set AGENT_DB_PATH=%ROOT%backend\data\agent\agent.sqlite& set AGENT_BRIDGE_URL=http://127.0.0.1:47001& set AGENT_ID_KEY=id_key& node --disable-warning=ExperimentalWarning src/serve.ts"
  set "AGENT_SET=set APP_AGENT_ENABLED=true& set APP_AGENT_URL=http://127.0.0.1:8090& set APP_AGENT_BRIDGE_URL=http://127.0.0.1:47001& set APP_AGENT_SERVICE_TOKEN=%AGENT_TOKEN%& "
) else (
  echo [提示] 没有 agent\node_modules ^(或没有 node^)，画布 Agent 不启动。需要的话：cd agent ^&^& npm ci
)

echo ==^> 启动后端 (配置: %CONFIG%)
start "video-canvas backend" /d "%ROOT%backend" cmd /k "chcp 65001 >nul & %AGENT_SET%go run ./cmd/server -c %CONFIG%"

echo ==^> 启动前端 (%PM%)
start "video-canvas web" /d "%ROOT%web" cmd /k "chcp 65001 >nul & %WEB_CMD%"

echo.
echo 前端 http://localhost:5173    后端 http://localhost:8080
echo 关闭对应窗口即可停止服务
endlocal
