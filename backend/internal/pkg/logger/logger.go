package logger

import (
	"os"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"

	"video-canvas/internal/config"
	"video-canvas/internal/pkg/idcodec"
)

// 在 Init 之前调用也是安全的，只是不输出任何内容。
var (
	l = zap.NewNop()
	// payload 是 payload.log 的 logger：每次上游调用的请求和响应全文；未开启时为 nil
	payload *zap.Logger
	// payloadBuf 是 payload.log 的缓冲写入，Close 时刷盘并停掉后台协程
	payloadBuf *zapcore.BufferedWriteSyncer
	// files 是打开的滚动文件，Close 时关闭（测试里反复 Init 不泄漏文件句柄）
	files []*lumberjack.Logger
)

// payload.log 的缓冲：攒够 1MB 或过 1 秒刷一次盘，进程崩溃最多丢这么多
const (
	payloadBufferSize    = 1 << 20
	payloadFlushInterval = time.Second
)

// Init 初始化全局 logger。主日志始终写标准输出（格式按 cfg.Format），配置了 filename 时同时写滚动文件（固定 JSON 行）；
// 开启 payload 时另建一个只写文件的 payload logger。每行都带 instance 字段。重复调用会先关掉上一次打开的文件。
func Init(cfg config.Log) error {
	level, err := zapcore.ParseLevel(cfg.Level)
	if err != nil {
		return err
	}
	Close()
	instance := cfg.Instance
	if instance == "" {
		instance, _ = os.Hostname()
	}
	base := zap.String("instance", instance)

	// 1. 主日志：标准输出 + app.log
	cores := []zapcore.Core{zapcore.NewCore(stdoutEncoder(cfg), zapcore.AddSync(os.Stdout), level)}
	if cfg.Filename != "" {
		f := rolling(cfg.Filename, cfg.MaxSize, cfg.MaxBackups, cfg.MaxAge, true)
		cores = append(cores, zapcore.NewCore(zapcore.NewJSONEncoder(encoderConfig()), zapcore.AddSync(f), level))
	}
	l = zap.New(zapcore.NewTee(cores...), zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel)).With(base)
	zap.ReplaceGlobals(l)

	// 2. payload.log：只写文件、不分级别；缓冲写入，不拖慢业务
	if cfg.Payload.Enabled && cfg.Payload.Filename != "" {
		f := rolling(cfg.Payload.Filename, cfg.Payload.MaxSize, cfg.Payload.MaxBackups, cfg.Payload.MaxAge, cfg.Payload.Compress)
		payloadBuf = &zapcore.BufferedWriteSyncer{WS: zapcore.AddSync(f), Size: payloadBufferSize, FlushInterval: payloadFlushInterval}
		payload = zap.New(zapcore.NewCore(zapcore.NewJSONEncoder(encoderConfig()), payloadBuf, zapcore.DebugLevel)).With(base)
	}
	return nil
}

// encoderConfig 是两个文件共用的 JSON 编码配置：time / level / msg / caller，ISO8601 时间。
func encoderConfig() zapcore.EncoderConfig {
	encCfg := zap.NewProductionEncoderConfig()
	encCfg.TimeKey = "time"
	encCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	return encCfg
}

// stdoutEncoder 按配置选标准输出的格式；console 格式在没写文件时（本地开发）带颜色。
func stdoutEncoder(cfg config.Log) zapcore.Encoder {
	encCfg := encoderConfig()
	if cfg.Format == "json" {
		return zapcore.NewJSONEncoder(encCfg)
	}
	if cfg.Filename == "" {
		encCfg.EncodeLevel = zapcore.CapitalColorLevelEncoder
	} else {
		encCfg.EncodeLevel = zapcore.CapitalLevelEncoder // 生产写文件时标准输出进 docker logs，不带颜色控制符
	}
	return zapcore.NewConsoleEncoder(encCfg)
}

// rolling 打开一个滚动文件并登记，Close 时统一关闭。
func rolling(name string, maxSize, maxBackups, maxAge int, compress bool) *lumberjack.Logger {
	f := &lumberjack.Logger{Filename: name, MaxSize: maxSize, MaxBackups: maxBackups, MaxAge: maxAge, Compress: compress}
	files = append(files, f)
	return f
}

// Close 刷盘并关闭日志文件，进程退出前调用；之后的日志不再写文件（主日志退回空操作）。可以重复调用。
func Close() {
	_ = l.Sync()
	if payloadBuf != nil {
		_ = payloadBuf.Stop() // 刷出缓冲里剩下的 payload
	}
	for _, f := range files {
		_ = f.Close()
	}
	l, payload, payloadBuf, files = zap.NewNop(), nil, nil, nil
}

func L() *zap.Logger { return l }

// TaskID 是任务日志的统一 task_id 字段：值是界面上展示的十六进制任务编号（idcodec 编码），
// 用户把编号报过来就能原样搜到；不要再写 zap.Uint64("task_id", id)。
func TaskID(id uint64) zap.Field { return zap.String("task_id", idcodec.Encode(id)) }

// Sync 把主日志和 payload.log 的缓冲刷到磁盘，不关闭文件。
func Sync() {
	_ = l.Sync()
	if payloadBuf != nil {
		_ = payloadBuf.Sync()
	}
}

// 以下快捷函数跳过一层调用栈，使日志里的 caller 指向真正的调用方。

func Debug(msg string, fields ...zap.Field) {
	l.WithOptions(zap.AddCallerSkip(1)).Debug(msg, fields...)
}

func Info(msg string, fields ...zap.Field) {
	l.WithOptions(zap.AddCallerSkip(1)).Info(msg, fields...)
}

func Warn(msg string, fields ...zap.Field) {
	l.WithOptions(zap.AddCallerSkip(1)).Warn(msg, fields...)
}

func Error(msg string, fields ...zap.Field) {
	l.WithOptions(zap.AddCallerSkip(1)).Error(msg, fields...)
}
