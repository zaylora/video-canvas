@echo off
chcp 65001 >nul
setlocal EnableExtensions

rem Docker 开发环境启动脚本（Windows）
set "ROOT=%~dp0"
set "COMPOSE_FILE=%ROOT%docker-compose.dev.yml"
set "BUILD=--build"
set "DETACH="

if not exist "%COMPOSE_FILE%" (
  echo [错误] 未找到 %COMPOSE_FILE%
  exit /b 1
)

where docker >nul 2>nul
if errorlevel 1 (
  echo [错误] 未找到 Docker，请先安装 Docker Desktop。
  exit /b 1
)

docker info >nul 2>nul
if errorlevel 1 (
  echo [错误] Docker daemon 未运行，请先启动 Docker Desktop。
  exit /b 1
)

for %%A in (%*) do (
  if /I "%%A"=="--no-build" set "BUILD="
  if /I "%%A"=="-d" set "DETACH=-d"
  if /I "%%A"=="--detach" set "DETACH=-d"
  if /I "%%A"=="-h" goto :help
  if /I "%%A"=="--help" goto :help
)

if defined BUILD (
  echo ==^> 构建 video-canvas Docker 镜像
  docker compose -f "%COMPOSE_FILE%" build
  if errorlevel 1 exit /b 1
)

echo ==^> 清理悬空 Docker 镜像
docker image prune --force
if errorlevel 1 exit /b 1

echo ==^> 启动 video-canvas Docker 开发环境
docker compose -f "%COMPOSE_FILE%" up --remove-orphans %DETACH%
if errorlevel 1 exit /b %errorlevel%

if defined DETACH (
  echo.
  echo 前端：http://localhost:5173
  echo 后端：http://localhost:8080/health
  echo 日志：docker compose -f docker-compose.dev.yml logs -f backend web
)
exit /b 0

:help
echo 用法：start-docker.bat [选项]
echo.
echo 选项：
echo   -d, --detach   后台启动
echo   --no-build     不重新构建镜像
echo   -h, --help     显示帮助
exit /b 0
