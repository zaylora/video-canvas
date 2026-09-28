package logger

import (
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"

	"video-canvas/internal/config"
)

// 在 Init 之前调用也是安全的，只是不输出任何内容。
var l = zap.NewNop()

// Init 初始化全局 logger：始终输出到控制台，配置了 filename 时同时写入滚动日志文件。
func Init(cfg config.Log) error {
	level, err := zapcore.ParseLevel(cfg.Level)
	if err != nil {
		return err
	}

	encCfg := zap.NewProductionEncoderConfig()
	encCfg.TimeKey = "time"
	encCfg.EncodeTime = zapcore.ISO8601TimeEncoder

	var enc zapcore.Encoder
	if cfg.Format == "json" {
		enc = zapcore.NewJSONEncoder(encCfg)
	} else {
		if cfg.Filename == "" {
			encCfg.EncodeLevel = zapcore.CapitalColorLevelEncoder
		} else {
			encCfg.EncodeLevel = zapcore.CapitalLevelEncoder // 写文件时不带颜色控制符
		}
		enc = zapcore.NewConsoleEncoder(encCfg)
	}

	writers := []zapcore.WriteSyncer{zapcore.AddSync(os.Stdout)}
	if cfg.Filename != "" {
		writers = append(writers, zapcore.AddSync(&lumberjack.Logger{
			Filename:   cfg.Filename,
			MaxSize:    cfg.MaxSize,
			MaxBackups: cfg.MaxBackups,
			MaxAge:     cfg.MaxAge,
			Compress:   true,
		}))
	}

	core := zapcore.NewCore(enc, zapcore.NewMultiWriteSyncer(writers...), level)
	l = zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))
	zap.ReplaceGlobals(l)
	return nil
}

func L() *zap.Logger { return l }

func Sync() { _ = l.Sync() }

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
